"""Точка входа: python -m collector."""

from __future__ import annotations

import asyncio
import signal
import sys

import asyncpg

from . import config, log
from .schedule import Job, run_periodic


async def run(cfg: config.Config, logger: log.Logger) -> None:
    # Событие остановки вместо отмены задач: начатый цикл дорабатывает,
    # а новые не начинаются.
    stop = asyncio.Event()
    loop = asyncio.get_running_loop()
    for sig in (signal.SIGINT, signal.SIGTERM):
        loop.add_signal_handler(sig, _request_stop, stop, logger, sig)

    pool = await asyncpg.create_pool(cfg.database_url, min_size=1, max_size=cfg.db_pool_size)
    try:
        jobs: list[Job] = []
        logger.info("collector started", jobs=[job.name for job in jobs])
        await asyncio.gather(stop.wait(), *(run_periodic(job, stop, logger) for job in jobs))
    finally:
        await pool.close()
    logger.info("collector stopped")


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
