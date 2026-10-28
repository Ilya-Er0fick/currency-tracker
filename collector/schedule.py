"""Периодический запуск заданий и их остановка."""

from __future__ import annotations

import asyncio

from .log import Logger


async def wait_stop(stop: asyncio.Event, delay: float) -> bool:
    """Ждёт delay секунд; возвращает True, если за это время пришла остановка."""
    try:
        await asyncio.wait_for(stop.wait(), timeout=max(delay, 0))
        return True
    except asyncio.TimeoutError:
        return False


def seconds_until_next(now: float, period: float, offset: float) -> float:
    """Сколько ждать до ближайшей границы периода со сдвигом offset."""
    return period - (now - offset) % period


class Job:
    name: str
    # Пауза после цикла, упавшего не по вине источника, например без базы.
    failure_delay: float

    async def cycle(self) -> float:
        """Выполняет один цикл опроса и возвращает паузу до следующего."""
        raise NotImplementedError


async def run_periodic(job: Job, stop: asyncio.Event, logger: Logger) -> None:
    """Повторяет циклы задания до сигнала остановки.

    Остановка проверяется только между циклами и в паузах ожидания:
    начатый цикл дорабатывает и сохраняет то, что успел получить.
    """
    logger.info("job started", job=job.name)
    while not stop.is_set():
        try:
            delay = await job.cycle()
        except Exception as exc:
            logger.exception("cycle failed", job=job.name, error=str(exc))
            delay = job.failure_delay
        logger.debug("next cycle scheduled", job=job.name, delay_seconds=round(delay, 3))
        if await wait_stop(stop, delay):
            break
    logger.info("job stopped", job=job.name)
