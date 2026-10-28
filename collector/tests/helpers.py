from __future__ import annotations

import logging

from collector.db import Asset
from collector.log import Logger


def quiet_logger() -> Logger:
    logger = logging.getLogger("collector.tests")
    logger.addHandler(logging.NullHandler())
    logger.propagate = False
    return Logger(logger)


class RecordingLogger:
    """Запоминает записи журнала, чтобы проверять, что и с какими полями ушло."""

    def __init__(self):
        self.records = []

    def _record(self, level, msg, fields):
        self.records.append((level, msg, fields))

    def debug(self, msg, **fields):
        self._record("debug", msg, fields)

    def info(self, msg, **fields):
        self._record("info", msg, fields)

    def warning(self, msg, **fields):
        self._record("warning", msg, fields)

    def error(self, msg, **fields):
        self._record("error", msg, fields)

    def exception(self, msg, **fields):
        self._record("exception", msg, fields)


def crypto(asset_id: int, symbol: str) -> Asset:
    return Asset(asset_id, symbol, "crypto", "bybit", 60, True)


def fiat(asset_id: int, symbol: str) -> Asset:
    return Asset(asset_id, symbol, "fiat", "er-api", 86400, False)


class FakeStore:
    def __init__(self, crypto_assets=(), fiat_assets=()):
        self.crypto = list(crypto_assets)
        self.fiat = list(fiat_assets)
        self.saved = []

    async def assets_by_source(self, source):
        return [a for a in self.crypto if a.source == source]

    async def assets_by_type(self, asset_type):
        return self.fiat if asset_type == "fiat" else self.crypto

    async def save_candles(self, candles):
        self.saved.extend(candles)
        return len(candles)


class FakeClock:
    def __init__(self, now: float = 0.0):
        self.now = now

    def __call__(self) -> float:
        return self.now


class FakeSource:
    """Отвечает по URL и символу; ответ — значение или исключение."""

    def __init__(self, routes):
        self.routes = routes
        self.calls = []

    async def __call__(self, url, params=None):
        key = url if params is None else f"{url}?{params['symbol']}"
        self.calls.append(key)
        answer = self.routes[key]
        if isinstance(answer, BaseException):
            raise answer
        return answer
