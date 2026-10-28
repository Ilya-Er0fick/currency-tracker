import asyncio
from datetime import datetime, timezone
from decimal import Decimal

from collector.db import Asset
from collector.errors import RequestRejectedError
from collector.jobs import CryptoJob, _usable
from collector.sources import bybit

from .helpers import FakeClock, FakeSource, FakeStore, RecordingLogger, crypto, quiet_logger

# 2026-10-27 21:33:05 UTC
NOW = 1793136785.0
# Начало только что закрывшейся минуты, 21:32.
CLOSED_MS = (int(NOW) // 60 - 1) * 60_000


def kline(start_ms):
    return {"retCode": 0, "result": {"list": [
        [str(start_ms + 60_000), "2", "2", "2", "2", "1", "1"],
        [str(start_ms), "1", "3", "0.5", "2", "10", "20"],
    ]}}


def route(symbol):
    return f"{bybit.KLINE_URL}?{symbol}USDT"


def crypto_job(store, source, clock, logger=None):
    return CryptoJob(store, source, logger or quiet_logger(), clock=clock)


def test_crypto_cycle_saves_closed_candles_and_unit_usdt():
    clock = FakeClock(NOW)
    store = FakeStore([crypto(1, "USDT"), crypto(2, "BTC"), crypto(3, "ETH")])
    source = FakeSource({route("BTC"): kline(CLOSED_MS), route("ETH"): kline(CLOSED_MS)})

    delay = asyncio.run(crypto_job(store, source, clock).cycle())

    assert delay == 57  # до 21:34:02
    assert sorted(c.asset_id for c in store.saved) == [1, 2, 3]
    # На бирже нет пары USDTUSDT: запрос не делается, цена — единица.
    assert route("USDT") not in source.calls
    usdt = next(c for c in store.saved if c.asset_id == 1)
    assert usdt.open == usdt.high == usdt.low == usdt.close == Decimal(1)
    assert usdt.volume is None and usdt.turnover is None
    assert usdt.collected_at == datetime(2026, 10, 27, 21, 32, tzinfo=timezone.utc)
    btc = next(c for c in store.saved if c.asset_id == 2)
    assert btc.resolution == 1 and btc.high == Decimal("3") and btc.turnover == Decimal("20")


def test_usdt_minute_follows_exchange_candles():
    # Локальные часы ушли на две минуты вперёд, но свечи биржи — за 21:32,
    # и USDT должен лечь туда же, а не в 21:34.
    clock = FakeClock(NOW + 120)
    store = FakeStore([crypto(1, "USDT"), crypto(2, "BTC"), crypto(3, "ETH"), crypto(4, "SOL")])
    source = FakeSource({route("BTC"): kline(CLOSED_MS), route("ETH"): kline(CLOSED_MS),
                         route("SOL"): kline(CLOSED_MS + 60_000)})

    asyncio.run(crypto_job(store, source, clock).cycle())

    usdt = next(c for c in store.saved if c.asset_id == 1)
    assert usdt.collected_at == datetime(2026, 10, 27, 21, 32, tzinfo=timezone.utc)


def test_usdt_minute_falls_back_to_local_clock():
    clock = FakeClock(NOW)
    store = FakeStore([crypto(1, "USDT"), crypto(2, "BTC")])
    source = FakeSource({route("BTC"): RequestRejectedError("Not supported symbols")})

    asyncio.run(crypto_job(store, source, clock).cycle())

    assert [c.asset_id for c in store.saved] == [1]
    assert store.saved[0].collected_at == datetime(2026, 10, 27, 21, 32, tzinfo=timezone.utc)


def test_single_symbol_rejection_keeps_others():
    clock = FakeClock(NOW)
    store = FakeStore([crypto(2, "BTC"), crypto(3, "TON")])
    source = FakeSource({route("BTC"): kline(CLOSED_MS),
                         route("TON"): RequestRejectedError("Not supported symbols")})

    asyncio.run(crypto_job(store, source, clock).cycle())

    assert [c.asset_id for c in store.saved] == [2]


def test_malformed_response_keeps_others():
    clock = FakeClock(NOW)
    store = FakeStore([crypto(2, "BTC"), crypto(3, "ETH")])
    source = FakeSource({route("BTC"): kline(CLOSED_MS), route("ETH"): {"retCode": 0}})

    asyncio.run(crypto_job(store, source, clock).cycle())

    assert [c.asset_id for c in store.saved] == [2]


def test_unexpected_error_for_one_asset_keeps_others():
    clock = FakeClock(NOW)
    logger = RecordingLogger()
    store = FakeStore([crypto(2, "BTC"), crypto(3, "ETH"), crypto(4, "SOL")])
    source = FakeSource({route("BTC"): kline(CLOSED_MS), route("ETH"): RuntimeError("boom"),
                         route("SOL"): kline(CLOSED_MS)})

    asyncio.run(crypto_job(store, source, clock, logger).cycle())

    assert sorted(c.asset_id for c in store.saved) == [2, 4]
    failures = [f for level, msg, f in logger.records if level == "exception"]
    assert failures == [{"source": "bybit", "symbol": "ETH",
                         "error_type": "RuntimeError", "error": "boom"}]


def test_cycle_log_reports_counts():
    clock = FakeClock(NOW)
    logger = RecordingLogger()
    store = FakeStore([crypto(1, "USDT"), crypto(2, "BTC"), crypto(3, "TON")])
    source = FakeSource({route("BTC"): kline(CLOSED_MS),
                         route("TON"): RequestRejectedError("Not supported symbols")})

    asyncio.run(crypto_job(store, source, clock, logger).cycle())

    finished = next(f for _, msg, f in logger.records if msg == "crypto cycle finished")
    assert (finished["polled"], finished["fetched"], finished["failed"], finished["saved"]) == \
        (2, 1, 1, 2)


def test_usable_skips_unsupported_resolution():
    good = crypto(2, "BTC")
    odd = Asset(5, "ODD", "crypto", "bybit", 90, True)        # не кратно минуте
    weekly = Asset(6, "WEEK", "crypto", "bybit", 604800, True)  # нет в price_candles
    logger = RecordingLogger()

    assert _usable([good, odd, weekly], logger, "crypto") == [good]
    skipped = [f["symbol"] for level, _, f in logger.records if level == "error"]
    assert skipped == ["ODD", "WEEK"]
