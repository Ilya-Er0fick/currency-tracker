"""Циклы опроса источников."""

from __future__ import annotations

import asyncio
import time
from datetime import datetime, timezone
from decimal import Decimal
from typing import Callable, Protocol

from .candles import QUOTE_SYMBOL, RESOLUTIONS, Candle, flat_candle
from .db import Asset
from .errors import SourceError
from .httpclient import FetchJSON
from .log import Logger
from .schedule import Job, seconds_until_next
from .sources import bybit

# Сдвиг опроса от границы интервала: минутную свечу биржа закрывает
# не мгновенно.
CRYPTO_OFFSET = 2.0

DEFAULT_CRYPTO_PERIOD = 60

# Одновременных запросов к бирже: каталог опрашивается за секунды,
# а лимит биржи остаётся далеко.
BYBIT_CONCURRENCY = 10


class Store(Protocol):
    async def assets_by_source(self, source: str) -> list[Asset]: ...
    async def save_candles(self, candles: list[Candle]) -> int: ...


def _usable(assets: list[Asset], logger: Logger, job: str) -> list[Asset]:
    usable = []
    for asset in assets:
        if asset.resolution_seconds % 60 or asset.resolution not in RESOLUTIONS:
            logger.error("asset skipped: unsupported resolution", job=job, symbol=asset.symbol,
                         resolution_seconds=asset.resolution_seconds)
            continue
        usable.append(asset)
    return usable


class CryptoJob(Job):
    """Раз в минуту забирает закрытую минутную свечу по каждому активу биржи."""

    name = "crypto"
    failure_delay = DEFAULT_CRYPTO_PERIOD

    def __init__(self, store: Store, fetch: FetchJSON, logger: Logger, *,
                 clock: Callable[[], float] = time.time):
        self._store = store
        self._fetch = fetch
        self._logger = logger
        self._clock = clock

    async def cycle(self) -> float:
        assets = _usable(await self._store.assets_by_source(bybit.NAME), self._logger, self.name)
        period = min((a.resolution_seconds for a in assets), default=DEFAULT_CRYPTO_PERIOD)
        started = self._clock()

        # Цена котировочной валюты в самой себе — единица, на бирже такой
        # пары нет. Свеча пишется, чтобы пары с USDT имели обе стороны.
        closed_minute = datetime.fromtimestamp((started // 60 - 1) * 60, timezone.utc)
        candles = [flat_candle(a.id, a.resolution, closed_minute, Decimal(1))
                   for a in assets if a.symbol == QUOTE_SYMBOL]

        traded = [a for a in assets if a.symbol != QUOTE_SYMBOL]
        fetched = await self._fetch_all(traded)

        saved = await self._store.save_candles(candles + fetched)
        self._logger.info("crypto cycle finished", source=bybit.NAME, polled=len(traded),
                          fetched=len(fetched), failed=len(traded) - len(fetched), saved=saved,
                          duration_seconds=round(self._clock() - started, 3))
        return seconds_until_next(self._clock(), period, CRYPTO_OFFSET)

    async def _fetch_all(self, assets: list[Asset]) -> list[Candle]:
        semaphore = asyncio.Semaphore(BYBIT_CONCURRENCY)

        async def limited(asset: Asset) -> Candle | None:
            async with semaphore:
                return await self._fetch_one(asset)

        results = await asyncio.gather(*(limited(a) for a in assets))
        return [c for c in results if c is not None]

    async def _fetch_one(self, asset: Asset) -> Candle | None:
        try:
            return await bybit.fetch_candle(self._fetch, asset, self._clock)
        except SourceError as exc:
            self._logger.error("request failed", source=bybit.NAME, symbol=asset.symbol,
                               error_type=type(exc).__name__, error=str(exc))
            return None
        except Exception as exc:
            # Непредвиденная ошибка по одному активу не должна срывать сбор:
            # исключение из gather отменило бы весь цикл, и свечи остальных
            # активов за эту минуту потерялись бы.
            self._logger.exception("unexpected error", source=bybit.NAME, symbol=asset.symbol,
                                   error_type=type(exc).__name__, error=str(exc))
            return None
