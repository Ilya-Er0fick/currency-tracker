import asyncio

import pytest

from collector.breaker import CircuitBreaker, State
from collector.errors import RequestRejectedError, TransientError
from collector.retry import with_retry

from .helpers import FakeClock, quiet_logger


def run(coro):
    return asyncio.run(coro)


def make_breaker(clock, threshold=3, interval=300):
    return CircuitBreaker("test", threshold, interval, quiet_logger(), clock=clock)


def test_breaker_opens_after_consecutive_failures():
    clock = FakeClock()
    breaker = make_breaker(clock)

    breaker.record_failure()
    breaker.record_failure()
    assert breaker.state is State.CLOSED
    breaker.record_failure()
    assert breaker.state is State.OPEN
    assert not breaker.allow()


def test_breaker_success_resets_counter():
    breaker = make_breaker(FakeClock())
    breaker.record_failure()
    breaker.record_failure()
    breaker.record_success()
    breaker.record_failure()
    breaker.record_failure()
    assert breaker.state is State.CLOSED


def test_breaker_half_open_probe():
    clock = FakeClock()
    breaker = make_breaker(clock, threshold=1, interval=300)
    breaker.record_failure()

    clock.now = 299
    assert breaker.state is State.OPEN
    clock.now = 300
    assert breaker.state is State.HALF_OPEN
    assert breaker.allow()

    # Неудачная проба возвращает в Open на новый интервал.
    breaker.record_failure()
    assert breaker.state is State.OPEN
    clock.now = 599
    assert breaker.state is State.OPEN
    clock.now = 600
    assert breaker.state is State.HALF_OPEN

    breaker.record_success()
    assert breaker.state is State.CLOSED


class Flaky:
    def __init__(self, errors, result="ok"):
        self.errors = list(errors)
        self.result = result
        self.calls = 0

    async def __call__(self):
        self.calls += 1
        if self.errors:
            raise self.errors.pop(0)
        return self.result


def retry(call, attempts=4, budget=100.0, stop=None, base_delay=0.001):
    async def go():
        loop = asyncio.get_running_loop()
        return await with_retry(call, attempts=attempts, deadline=loop.time() + budget,
                                stop=stop or asyncio.Event(), logger=quiet_logger(),
                                what="test", base_delay=base_delay)
    return run(go())


def test_retry_recovers_from_transient_errors():
    call = Flaky([TransientError("429"), TransientError("503")])
    assert retry(call) == "ok"
    assert call.calls == 3


def test_retry_gives_up_after_attempts():
    call = Flaky([TransientError("timeout")] * 10)
    with pytest.raises(TransientError):
        retry(call, attempts=4)
    assert call.calls == 4


def test_retry_does_not_repeat_permanent_errors():
    call = Flaky([RequestRejectedError("404")])
    with pytest.raises(RequestRejectedError):
        retry(call)
    assert call.calls == 1


def test_retry_respects_cycle_budget():
    # Пауза 1 с не укладывается в оставшиеся 0,5 с: повтора нет.
    call = Flaky([TransientError("timeout")] * 3)
    with pytest.raises(TransientError):
        retry(call, budget=0.5, base_delay=1.0)
    assert call.calls == 1


def test_retry_stops_on_shutdown():
    async def go():
        stop = asyncio.Event()
        stop.set()
        call = Flaky([TransientError("timeout")] * 3)
        loop = asyncio.get_running_loop()
        with pytest.raises(TransientError):
            await with_retry(call, attempts=4, deadline=loop.time() + 100, stop=stop,
                             logger=quiet_logger(), what="test", base_delay=0.001)
        return call.calls
    assert run(go()) == 1


def test_retry_delays_double():
    delays = []

    async def go():
        loop = asyncio.get_running_loop()
        last = loop.time()

        async def call():
            nonlocal last
            now = loop.time()
            delays.append(now - last)
            last = now
            raise TransientError("503")

        with pytest.raises(TransientError):
            await with_retry(call, attempts=4, deadline=loop.time() + 100,
                             stop=asyncio.Event(), logger=quiet_logger(), what="test",
                             base_delay=0.02)

    run(go())
    gaps = delays[1:]
    assert len(gaps) == 3
    for gap, expected in zip(gaps, (0.02, 0.04, 0.08), strict=True):
        assert expected <= gap < expected + 0.05
