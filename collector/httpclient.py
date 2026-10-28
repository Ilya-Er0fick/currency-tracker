"""HTTP-запросы к источникам с разбором вида отказа."""

from __future__ import annotations

import asyncio
import json
from typing import Any, Awaitable, Callable

import aiohttp

from .errors import InvalidResponseError, RequestRejectedError, TransientError

FetchJSON = Callable[[str, dict[str, str] | None], Awaitable[Any]]


def json_fetcher(session: aiohttp.ClientSession) -> FetchJSON:
    """Возвращает функцию GET-запроса за JSON.

    Время ожидания задаётся у сессии и одинаково для всех запросов.
    """

    async def fetch(url: str, params: dict[str, str] | None = None) -> Any:
        try:
            async with session.get(url, params=params) as response:
                status = response.status
                body = await response.text()
        except asyncio.TimeoutError:
            raise TransientError(f"timeout: {url}") from None
        except aiohttp.ClientConnectionError as exc:
            raise TransientError(f"connection error: {url}: {exc}") from None
        except aiohttp.ClientPayloadError as exc:
            raise TransientError(f"broken response: {url}: {exc}") from None

        if status == 429 or status >= 500:
            raise TransientError(f"HTTP {status}: {url}")
        if status >= 400:
            raise RequestRejectedError(f"HTTP {status}: {url}")

        # Тип содержимого не проверяется: ЦБ отдаёт JSON как application/javascript.
        try:
            return json.loads(body)
        except ValueError:
            raise InvalidResponseError(f"malformed JSON: {url}") from None

    return fetch
