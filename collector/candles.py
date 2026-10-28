"""Свеча и общие правила записи цен."""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime
from decimal import Decimal, InvalidOperation

from .errors import InvalidResponseError

# Валюта, в которой выражены все цены. Цена её самой — единица по определению.
QUOTE_SYMBOL = "USDT"

# Разрешения, которые допускает price_candles, в минутах.
RESOLUTIONS = (1, 5, 60, 1440)


@dataclass(frozen=True)
class Candle:
    asset_id: int
    resolution: int
    collected_at: datetime
    open: Decimal
    high: Decimal
    low: Decimal
    close: Decimal
    volume: Decimal | None = None
    turnover: Decimal | None = None


def flat_candle(asset_id: int, resolution: int, collected_at: datetime, price: Decimal) -> Candle:
    """Вырожденная свеча: все четыре цены совпадают, объёмов нет."""
    return Candle(asset_id, resolution, collected_at, price, price, price, price)


def parse_decimal(raw) -> Decimal:
    try:
        value = Decimal(str(raw))
    except (InvalidOperation, ValueError):
        raise InvalidResponseError(f"not a number: {raw!r}") from None
    if not value.is_finite() or value < 0:
        raise InvalidResponseError(f"not a valid price: {raw!r}")
    return value
