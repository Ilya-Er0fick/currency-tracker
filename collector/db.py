"""Работа с PostgreSQL: справочник активов и запись свечей."""

from __future__ import annotations

from dataclasses import dataclass

import asyncpg

from .candles import Candle


@dataclass(frozen=True)
class Asset:
    id: int
    symbol: str
    type: str
    source: str
    resolution_seconds: int
    supports_realtime: bool

    @property
    def resolution(self) -> int:
        """Разрешение свечи в минутах, как в столбце price_candles.resolution."""
        return self.resolution_seconds // 60


_ASSET_COLUMNS = "id, symbol, type, source, resolution_seconds, supports_realtime"

# Строки передаются массивами и разворачиваются через unnest: один запрос
# на весь цикл вместо запроса на свечу. Повторная вставка той же свечи
# ничего не делает, поэтому перезапуск сборщика не создаёт дублей,
# а RETURNING даёт число действительно добавленных строк.
_INSERT_CANDLES = """
INSERT INTO price_candles (asset_id, resolution, collected_at,
                           open_price, high_price, low_price, close_price,
                           volume, turnover)
SELECT * FROM unnest($1::integer[], $2::smallint[], $3::timestamptz[],
                     $4::numeric[], $5::numeric[], $6::numeric[], $7::numeric[],
                     $8::numeric[], $9::numeric[])
ON CONFLICT (asset_id, resolution, collected_at) DO NOTHING
RETURNING id
"""


class Store:
    def __init__(self, pool: asyncpg.Pool):
        self._pool = pool

    async def assets_by_source(self, source: str) -> list[Asset]:
        rows = await self._pool.fetch(
            f"SELECT {_ASSET_COLUMNS} FROM assets WHERE source = $1 ORDER BY symbol", source)
        return [Asset(**dict(row)) for row in rows]

    async def save_candles(self, candles: list[Candle]) -> int:
        if not candles:
            return 0
        columns = list(zip(*(
            (c.asset_id, c.resolution, c.collected_at, c.open, c.high, c.low, c.close,
             c.volume, c.turnover)
            for c in candles
        )))
        rows = await self._pool.fetch(_INSERT_CANDLES, *map(list, columns))
        return len(rows)
