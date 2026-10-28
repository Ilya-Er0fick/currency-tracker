"""Курс рубля от ЦБ РФ — резервный источник фиатных курсов."""

from __future__ import annotations

from datetime import datetime, timezone
from decimal import Decimal

from ..candles import parse_decimal, unit_price
from ..errors import InvalidResponseError
from ..httpclient import FetchJSON

NAME = "cbr"
URL = "https://www.cbr-xml-daily.ru/daily_json.js"

# Единственная валюта, которую даёт резервный источник.
SYMBOL = "RUB"


async def fetch_rub(fetch: FetchJSON) -> tuple[datetime, Decimal]:
    return parse_daily(await fetch(URL, None))


def parse_daily(payload) -> tuple[datetime, Decimal]:
    """Возвращает день курса и цену одного рубля в долларах.

    ЦБ публикует, сколько рублей стоит Nominal долларов. Направление
    приводится к принятому в базе — «сколько USDT за один рубль».
    """
    try:
        usd = payload["Valute"]["USD"]
        rub_per_usd = parse_decimal(usd["Value"]) / parse_decimal(usd["Nominal"])
        day = datetime.fromisoformat(payload["Date"]).date()
    except (KeyError, TypeError, ValueError, ArithmeticError):
        raise InvalidResponseError("cbr: unexpected shape") from None

    # Дата ЦБ — день, на который установлен курс; свеча открывается в его полночь.
    collected_at = datetime(day.year, day.month, day.day, tzinfo=timezone.utc)
    return collected_at, unit_price(rub_per_usd)
