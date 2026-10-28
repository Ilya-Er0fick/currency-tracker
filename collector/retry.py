"""Повтор запроса при временных отказах с возрастающей паузой."""

from __future__ import annotations

import asyncio
from typing import Awaitable, Callable, TypeVar

from .errors import TransientError
from .log import Logger
from .schedule import wait_stop

T = TypeVar("T")


async def with_retry(
    call: Callable[[], Awaitable[T]],
    *,
    attempts: int,
    deadline: float,
    stop: asyncio.Event,
    logger: Logger,
    what: str,
    base_delay: float = 1.0,
) -> T:
    """Выполняет call, повторяя его только при временном отказе.

    Паузы растут вдвое: 1 с, 2 с, 4 с. Повтор не начинается, если пауза
    вывела бы за deadline — общее время цикла ограничено, — и после
    сигнала остановки: цикл дорабатывает с тем, что успел получить.
    Остальные ошибки пробрасываются сразу.
    """
    loop = asyncio.get_running_loop()
    for attempt in range(1, attempts + 1):
        try:
            return await call()
        except TransientError as exc:
            delay = base_delay * 2 ** (attempt - 1)
            if attempt == attempts or loop.time() + delay > deadline or stop.is_set():
                raise
            logger.warning("retrying request", target=what, attempt=attempt,
                           delay_seconds=delay, error=str(exc))
            if await wait_stop(stop, delay):
                raise
    raise AssertionError("unreachable")
