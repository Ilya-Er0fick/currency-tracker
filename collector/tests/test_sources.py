from datetime import datetime, timezone
from decimal import Decimal

import pytest

from collector.candles import parse_decimal
from collector.errors import InvalidResponseError, RequestRejectedError, TransientError
from collector.sources import bybit

from .helpers import crypto

BTC = crypto(2, "BTC")

KLINE = {
    "retCode": 0, "retMsg": "OK",
    "result": {"category": "spot", "symbol": "BTCUSDT", "list": [
        ["1793136780000", "81149.2", "81303.9", "81139.2", "81209", "21.663285", "1760186.48668064"],
        ["1793136720000", "81155.1", "81224.2", "81135.7", "81149.2", "25.998076", "2110325.3958895"],
    ]},
}


def test_kline_takes_second_candle():
    candle = bybit.parse_kline(KLINE, BTC, now_ms=1793136835945)

    assert candle.asset_id == 2
    assert candle.resolution == 1
    assert candle.collected_at == datetime(2026, 10, 27, 21, 32, tzinfo=timezone.utc)
    assert candle.open == Decimal("81155.1")
    assert candle.high == Decimal("81224.2")
    assert candle.low == Decimal("81135.7")
    assert candle.close == Decimal("81149.2")
    assert candle.volume == Decimal("25.998076")
    assert candle.turnover == Decimal("2110325.3958895")


def test_kline_params():
    assert bybit.kline_params("BTC") == {
        "category": "spot", "symbol": "BTCUSDT", "interval": "1", "limit": "2"}


def test_kline_rejects_unclosed_candle():
    with pytest.raises(InvalidResponseError):
        bybit.parse_kline(KLINE, BTC, now_ms=1793136720000 + 59_999)


def test_kline_accepts_candle_closed_exactly_now():
    assert bybit.parse_kline(KLINE, BTC, now_ms=1793136720000 + 60_000).asset_id == 2


@pytest.mark.parametrize("code,error", [
    (10006, TransientError),
    (10016, TransientError),
    (10001, RequestRejectedError),
])
def test_kline_error_codes(code, error):
    with pytest.raises(error):
        bybit.parse_kline({"retCode": code, "retMsg": "x", "result": {}}, BTC, now_ms=0)


@pytest.mark.parametrize("payload", [
    [],
    None,
    {"retCode": 0},
    {"retCode": 0, "result": {"list": []}},
    {"retCode": 0, "result": {"list": [KLINE["result"]["list"][0]]}},
    {"retCode": 0, "result": {"list": [["1"], ["2", "1", "1"]]}},
    {"retCode": 0, "result": {"list": [KLINE["result"]["list"][0],
                                       ["0", "x", "1", "1", "1", "1", "1"]]}},
    {"retCode": 0, "result": {"list": [KLINE["result"]["list"][0],
                                       ["0", "-1", "1", "1", "1", "1", "1"]]}},
])
def test_kline_rejects_malformed(payload):
    with pytest.raises(InvalidResponseError):
        bybit.parse_kline(payload, BTC, now_ms=10**13)


@pytest.mark.parametrize("raw,expected", [
    ("81149.2", Decimal("81149.2")),
    (1, Decimal("1")),
    ("0", Decimal("0")),
    ("1e-8", Decimal("1e-8")),
])
def test_parse_decimal(raw, expected):
    assert parse_decimal(raw) == expected


@pytest.mark.parametrize("raw", ["-1", "abc", "", None, "NaN", "Infinity", [1]])
def test_parse_decimal_rejects(raw):
    with pytest.raises(InvalidResponseError):
        parse_decimal(raw)
