"""Точка входа: python -m collector."""

from __future__ import annotations

import asyncio
import signal
import sys

import aiohttp
import asyncpg

from . import config, log
from .breaker import CircuitBreaker
from .db import Store
from .httpclient import json_fetcher
from .jobs import CryptoJob, FiatJob
from .schedule import run_periodic
from .sources import bybit, erapi

USER_AGENT = "currency-tracker-collector/1.0"


async def run(cfg: config.Config, logger: log.Logger) -> None:
    # Событие остановки вместо отмены задач: начатый цикл дорабатывает,
    # а новые не начинаются.
    stop = asyncio.Event()
    install_stop_handlers(asyncio.get_running_loop(), stop, logger)

    pool = await asyncpg.create_pool(cfg.database_url, min_size=1, max_size=cfg.db_pool_size)
    try:
        store = Store(pool)
        # Без явного значения действует умолчание aiohttp — пять минут на запрос:
        # подвисший источник остановил бы минутный цикл надолго.
        timeout = aiohttp.ClientTimeout(total=cfg.http_timeout)
        async with aiohttp.ClientSession(timeout=timeout,
                                         headers={"User-Agent": USER_AGENT}) as session:
            fetch = json_fetcher(session)

            # Предохранитель у каждого источника свой: отказ биржи
            # не должен останавливать опрос курсов валют.
            def breaker(name: str) -> CircuitBreaker:
                return CircuitBreaker(name, cfg.breaker_threshold, cfg.breaker_open_interval,
                                      logger)

            jobs = [
                CryptoJob(store, fetch, breaker(bybit.NAME), stop, logger,
                          attempts=cfg.retry_attempts),
                FiatJob(store, fetch, breaker(erapi.NAME), stop, logger,
                        attempts=cfg.retry_attempts, retry_interval=cfg.fiat_retry_interval),
            ]
            logger.info("collector started", jobs=[job.name for job in jobs])
            await asyncio.gather(stop.wait(), *(run_periodic(job, stop, logger) for job in jobs))
    finally:
        await pool.close()
    logger.info("collector stopped")


def install_stop_handlers(loop: asyncio.AbstractEventLoop, stop: asyncio.Event,
                          logger: log.Logger) -> None:
    for sig in (signal.SIGINT, signal.SIGTERM):
        try:
            loop.add_signal_handler(sig, _request_stop, stop, logger, sig)
        except NotImplementedError:
            # На Windows цикл событий обработчиков сигналов не поддерживает.
            # Обычный обработчик выполняется вне цикла, поэтому остановка
            # передаётся в него потокобезопасно. sig фиксируется аргументом
            # по умолчанию: иначе все обработчики видели бы последний сигнал.
            signal.signal(sig, lambda _signum, _frame, sig=sig: loop.call_soon_threadsafe(
                _request_stop, stop, logger, sig))


def _request_stop(stop: asyncio.Event, logger: log.Logger, sig: signal.Signals) -> None:
    if not stop.is_set():
        logger.info("collector shutting down", signal=sig.name)
    stop.set()


def main() -> int:
    try:
        cfg = config.load()
    except config.ConfigError as exc:
        log.setup("INFO").error("collector stopped", error=str(exc))
        return 1

    logger = log.setup(cfg.log_level)
    try:
        asyncio.run(run(cfg, logger))
    except Exception as exc:
        logger.exception("collector stopped", error=str(exc))
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
