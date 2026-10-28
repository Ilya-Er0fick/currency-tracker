import asyncio

import pytest

from collector.schedule import run_periodic, seconds_until_next, wait_stop

from .helpers import quiet_logger


@pytest.mark.parametrize("now,expected", [
    (120.0, 2.0),    # до 2-й секунды минуты
    (121.5, 0.5),
    (122.0, 60.0),   # ровно на границе — следующая через период
    (150.0, 32.0),
])
def test_seconds_until_next(now, expected):
    assert seconds_until_next(now, 60, 2) == expected


def test_seconds_until_next_daily_offset():
    # 2026-10-27 21:33:05 UTC → следующий запуск 2026-10-28 01:00 UTC.
    assert seconds_until_next(1793136785.0, 86400, 3600) == 12415.0


def test_wait_stop_times_out():
    async def go():
        return await wait_stop(asyncio.Event(), 0.01)
    assert asyncio.run(go()) is False


def test_wait_stop_returns_on_stop():
    async def go():
        stop = asyncio.Event()
        asyncio.get_running_loop().call_later(0.01, stop.set)
        return await asyncio.wait_for(wait_stop(stop, 60), 1)
    assert asyncio.run(go()) is True


def test_wait_stop_negative_delay():
    async def go():
        return await wait_stop(asyncio.Event(), -5)
    assert asyncio.run(go()) is False


def test_run_periodic_finishes_current_cycle_on_stop():
    events = []

    class SlowJob:
        name = "slow"
        failure_delay = 1

        def __init__(self, stop):
            self.stop = stop

        async def cycle(self):
            events.append("start")
            self.stop.set()  # сигнал пришёл посреди цикла
            await asyncio.sleep(0.01)
            events.append("end")
            return 3600

    async def go():
        stop = asyncio.Event()
        await asyncio.wait_for(run_periodic(SlowJob(stop), stop, quiet_logger()), 1)

    asyncio.run(go())
    assert events == ["start", "end"]


def test_run_periodic_survives_cycle_errors():
    calls = []

    class BrokenJob:
        name = "broken"
        failure_delay = 0

        def __init__(self, stop):
            self.stop = stop

        async def cycle(self):
            calls.append(1)
            if len(calls) == 2:
                self.stop.set()
            raise ConnectionError("database is down")

    async def go():
        stop = asyncio.Event()
        await asyncio.wait_for(run_periodic(BrokenJob(stop), stop, quiet_logger()), 1)

    asyncio.run(go())
    assert len(calls) == 2
