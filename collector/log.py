"""Журнал в STDOUT в формате JSON, как у сервера."""

from __future__ import annotations

import json
import logging
import sys
from datetime import datetime, timezone


class JSONFormatter(logging.Formatter):
    def format(self, record: logging.LogRecord) -> str:
        entry = {
            "time": datetime.fromtimestamp(record.created, timezone.utc).isoformat(),
            "level": record.levelname,
            "msg": record.getMessage(),
        }
        entry.update(getattr(record, "fields", {}))
        if record.exc_info:
            entry["exception"] = self.formatException(record.exc_info)
        return json.dumps(entry, ensure_ascii=False, default=str)


class Logger:
    """Обёртка, принимающая поля записи именованными аргументами."""

    def __init__(self, logger: logging.Logger):
        self._logger = logger

    def debug(self, msg: str, **fields) -> None:
        self._logger.debug(msg, extra={"fields": fields})

    def info(self, msg: str, **fields) -> None:
        self._logger.info(msg, extra={"fields": fields})

    def warning(self, msg: str, **fields) -> None:
        self._logger.warning(msg, extra={"fields": fields})

    def error(self, msg: str, **fields) -> None:
        self._logger.error(msg, extra={"fields": fields})

    def exception(self, msg: str, **fields) -> None:
        self._logger.exception(msg, extra={"fields": fields})


def setup(level: str) -> Logger:
    handler = logging.StreamHandler(sys.stdout)
    handler.setFormatter(JSONFormatter())

    logger = logging.getLogger("collector")
    logger.handlers[:] = [handler]
    logger.setLevel(level)
    logger.propagate = False
    return Logger(logger)
