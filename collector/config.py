"""Настройки сборщика из переменных окружения и файла .env."""

from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path

# Файл с настройками для разработки, тот же, что читает сервер.
ENV_FILE = ".env"

# Корень проекта: .env сервера лежит там, и сборщик, запущенный из своей
# папки, должен найти его так же, как запущенный из корня.
PROJECT_ROOT = Path(__file__).resolve().parent.parent


LOG_LEVELS = ("DEBUG", "INFO", "WARNING", "ERROR")


class ConfigError(Exception):
    """Ошибка настройки: повторять бессмысленно, нужно исправить окружение."""


@dataclass(frozen=True)
class Config:
    database_url: str
    http_timeout: float = 10.0
    retry_attempts: int = 4
    breaker_threshold: int = 3
    breaker_open_interval: float = 300.0
    fiat_retry_interval: float = 600.0
    db_pool_size: int = 5
    log_level: str = "INFO"


def load(environ: dict[str, str] | None = None) -> Config:
    if environ is None:
        for path in (Path.cwd() / ENV_FILE, PROJECT_ROOT / ENV_FILE):
            load_env_file(path)
        environ = dict(os.environ)

    database_url = environ.get("DATABASE_URL", "")
    if not database_url:
        raise ConfigError("DATABASE_URL is not set")

    # Уровень проверяется здесь, а не при настройке журнала: опечатка должна
    # дать понятную ошибку конфигурации, а не трассировку из logging.
    log_level = environ.get("COLLECTOR_LOG_LEVEL", "INFO").upper()
    if log_level not in LOG_LEVELS:
        raise ConfigError(f"COLLECTOR_LOG_LEVEL: expected one of {', '.join(LOG_LEVELS)}, "
                          f"got {log_level!r}")

    return Config(
        database_url=database_url,
        http_timeout=_positive(environ, "COLLECTOR_HTTP_TIMEOUT", 10.0, float),
        retry_attempts=_positive(environ, "COLLECTOR_RETRY_ATTEMPTS", 4, int),
        breaker_threshold=_positive(environ, "COLLECTOR_BREAKER_THRESHOLD", 3, int),
        breaker_open_interval=_positive(environ, "COLLECTOR_BREAKER_OPEN_INTERVAL", 300.0, float),
        fiat_retry_interval=_positive(environ, "COLLECTOR_FIAT_RETRY_INTERVAL", 600.0, float),
        db_pool_size=_positive(environ, "COLLECTOR_DB_POOL_SIZE", 5, int),
        log_level=log_level,
    )


def _positive(environ: dict[str, str], key: str, default, kind):
    raw = environ.get(key, "")
    if raw == "":
        return default
    try:
        value = kind(raw)
    except ValueError:
        raise ConfigError(f"{key}: expected positive number, got {raw!r}") from None
    if value <= 0:
        raise ConfigError(f"{key}: expected positive number, got {raw!r}")
    return value


def load_env_file(path: Path) -> None:
    """Переносит настройки из файла в окружение процесса.

    Уже заданные переменные не перезаписываются: в контейнере настройки
    приходят снаружи, и файл разработчика не должен их подменять.
    Отсутствие файла ошибкой не считается.
    """
    try:
        text = path.read_text(encoding="utf-8")
    except FileNotFoundError:
        return

    for line in text.splitlines():
        parsed = parse_line(line)
        if parsed is None:
            continue
        key, value = parsed
        os.environ.setdefault(key, value)


def parse_line(line: str) -> tuple[str, str] | None:
    line = line.strip()
    if not line or line.startswith("#"):
        return None

    key, sep, value = line.partition("=")
    if not sep:
        return None

    key = key.strip().removeprefix("export ").strip()
    value = value.strip()

    # Кавычки вокруг значения встречаются в примерах конфигураций,
    # но частью значения не являются.
    if len(value) >= 2 and value[0] in "\"'" and value[-1] == value[0]:
        value = value[1:-1]

    if not key:
        return None
    return key, value
