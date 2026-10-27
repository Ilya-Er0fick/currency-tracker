"""Циклы опроса источников."""

from __future__ import annotations

import asyncio
import time
from collections import Counter
from datetime import datetime, timezone
from decimal import Decimal
from typing import Callable, Protocol

from .candles import QUOTE_SYMBOL, RESOLUTIONS, Candle, flat_candle
from .db import Asset
from .errors import SourceError
from .httpclient import FetchJSON
from .log import Logger
from .schedule import Job, seconds_until_next
from .sources import bybit, erapi

# Сдвиг опроса от границы интервала. Минутную свечу биржа закрывает
# не мгновенно, а курсы er-api обновляются в первые полчаса суток UTC.
CRYPTO_OFFSET = 2.0
FIAT_OFFSET = 3600.0

DEFAULT_CRYPTO_PERIOD = 60
DEFAULT_FIAT_PERIOD = 86400

# Одновременных запросов к бирже: каталог опрашивается за секунды,
# а лимит биржи остаётся далеко.
BYBIT_CONCURRENCY = 10


class Store(Protocol):
    async def assets_by_source(self, source: str) -> list[Asset]: ...
    async def assets_by_type(self, asset_type: str) -> list[Asset]: ...
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


def _quote_minute(fetched: list[Candle], started: float) -> datetime:
    """Минута, в которую ложится свеча USDT.

    Берётся у свечей биржи: локальные часы могут расходиться с биржевыми,
    а цикл — запаздывать, и тогда USDT оказался бы в другой минуте, чем
    остальные активы, а в сравнении пар появились бы разрывы. Если свечи
    разошлись по минутам, побеждает самая частая. Локальные часы остаются
    на случай, когда биржа не отдала ничего.
    """
    if fetched:
        return Counter(c.collected_at for c in fetched).most_common(1)[0][0]
    return datetime.fromtimestamp((started // 60 - 1) * 60, timezone.utc)


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

        traded = [a for a in assets if a.symbol != QUOTE_SYMBOL]
        fetched = await self._fetch_all(traded)

        # Цена котировочной валюты в самой себе — единица, на бирже такой
        # пары нет. Свеча пишется, чтобы пары с USDT имели обе стороны.
        quote_minute = _quote_minute(fetched, started)
        quote = [flat_candle(a.id, a.resolution, quote_minute, Decimal(1))
                 for a in assets if a.symbol == QUOTE_SYMBOL]

        saved = await self._store.save_candles(fetched + quote)
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


class FiatJob(Job):
    """Раз в сутки забирает курсы фиатных валют."""

    name = "fiat"

    def __init__(self, store: Store, fetch: FetchJSON, logger: Logger, *,
                 retry_interval: float, clock: Callable[[], float] = time.time):
        self._store = store
        self._fetch = fetch
        self._logger = logger
        self._retry_interval = retry_interval
        self._clock = clock
        self.failure_delay = retry_interval

    async def cycle(self) -> float:
        assets = _usable(await self._store.assets_by_type("fiat"), self._logger, self.name)
        period = min((a.resolution_seconds for a in assets), default=DEFAULT_FIAT_PERIOD)
        until_next = seconds_until_next(self._clock(), period, FIAT_OFFSET)

        if await self._collect(assets):
            return until_next
        # Сутки до следующей попытки — слишком долго: неуспешный цикл
        # повторяется чаще.
        return min(self._retry_interval, until_next)

    async def _collect(self, assets: list[Asset]) -> bool:
        if not assets:
            return True

        try:
            day, prices = await erapi.fetch_prices(self._fetch, [a.symbol for a in assets])
        except SourceError as exc:
            self._logger.error("request failed", source=erapi.NAME,
                               error_type=type(exc).__name__, error=str(exc))
            return False

        await self._save_prices(assets, day, prices)
        return True

    async def _save_prices(self, assets: list[Asset], day: datetime,
                           prices: dict[str, Decimal]) -> None:
        candles = [flat_candle(a.id, a.resolution, day, prices[a.symbol])
                   for a in assets if a.symbol in prices]
        missing = sorted(a.symbol for a in assets if a.symbol not in prices)
        if missing:
            self._logger.warning("currencies missing in response", source=erapi.NAME,
                                 symbols=missing)
        saved = await self._store.save_candles(candles)
        self._logger.info("fiat cycle finished", source=erapi.NAME, day=day.date().isoformat(),
                          fetched=len(candles), saved=saved)
