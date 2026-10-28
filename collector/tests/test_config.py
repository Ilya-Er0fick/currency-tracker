import os

import pytest

from collector import config


def test_load_requires_database_url():
    with pytest.raises(config.ConfigError):
        config.load({})


def test_load_defaults():
    cfg = config.load({"DATABASE_URL": "postgres://x"})
    assert cfg.database_url == "postgres://x"
    assert cfg.http_timeout == 10.0
    assert cfg.retry_attempts == 4
    assert cfg.breaker_threshold == 3
    assert cfg.breaker_open_interval == 300.0
    assert cfg.fiat_retry_interval == 600.0
    assert cfg.db_pool_size == 5
    assert cfg.log_level == "INFO"


def test_load_overrides():
    cfg = config.load({"DATABASE_URL": "postgres://x", "COLLECTOR_HTTP_TIMEOUT": "3.5",
                       "COLLECTOR_DB_POOL_SIZE": "2", "COLLECTOR_LOG_LEVEL": "debug"})
    assert cfg.http_timeout == 3.5
    assert cfg.db_pool_size == 2
    assert cfg.log_level == "DEBUG"


@pytest.mark.parametrize("raw", ["0", "-1", "abc"])
def test_load_rejects_non_positive_numbers(raw):
    with pytest.raises(config.ConfigError):
        config.load({"DATABASE_URL": "postgres://x", "COLLECTOR_HTTP_TIMEOUT": raw})


def test_load_rejects_unknown_log_level():
    with pytest.raises(config.ConfigError, match="COLLECTOR_LOG_LEVEL"):
        config.load({"DATABASE_URL": "postgres://x", "COLLECTOR_LOG_LEVEL": "debg"})


@pytest.mark.parametrize("line,expected", [
    ("KEY=value", ("KEY", "value")),
    ("export KEY = value ", ("KEY", "value")),
    ('KEY="quoted value"', ("KEY", "quoted value")),
    ("KEY='x'", ("KEY", "x")),
    ("KEY=\"unbalanced'", ("KEY", "\"unbalanced'")),
    ("URL=postgres://a:b@h/db?sslmode=disable", ("URL", "postgres://a:b@h/db?sslmode=disable")),
    ("EMPTY=", ("EMPTY", "")),
    ("# comment", None),
    ("   # indented comment", None),
    ("", None),
    ("no separator", None),
    ("=value", None),
])
def test_parse_line(line, expected):
    assert config.parse_line(line) == expected


def test_env_file_does_not_override_environment(tmp_path, monkeypatch):
    env = tmp_path / ".env"
    env.write_text("COLLECTOR_T_SET=from-file\nCOLLECTOR_T_NEW=from-file\n")
    monkeypatch.setenv("COLLECTOR_T_SET", "from-env")
    monkeypatch.delenv("COLLECTOR_T_NEW", raising=False)

    config.load_env_file(env)

    assert os.environ["COLLECTOR_T_SET"] == "from-env"
    assert os.environ["COLLECTOR_T_NEW"] == "from-file"
    monkeypatch.delenv("COLLECTOR_T_NEW")


def test_missing_env_file_is_not_an_error(tmp_path):
    config.load_env_file(tmp_path / "absent.env")
