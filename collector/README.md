# Сборщик данных

Отдельный процесс на Python: опрашивает внешние источники и пишет свечи
в `price_candles`. Схему базы создаёт сервер, сборщик только пишет в неё.

| Задание | Источник | Активы | Период | resolution |
|---|---|---|---|---|
| crypto | Bybit `/v5/market/kline`, вторая (закрытая) свеча | `assets.source = 'bybit'` | `resolution_seconds` (60 с) | 1 |
| fiat | `open.er-api.com` | `assets.type = 'fiat'` | `resolution_seconds` (сутки), в 01:00 UTC | 1440 |

Цена всегда означает «сколько USDT стоит одна единица актива». Курсы фиатных
источников («рублей за доллар») переворачиваются: хранится `1 / курс`.
USDT на бирже не торгуется сам к себе, его минутная свеча пишется с ценой 1.

## Запуск

Из корня проекта, `.env` берётся оттуда же (переменные окружения важнее файла):

```
python3 -m venv .venv
.venv/bin/pip install -r collector/requirements-dev.txt
.venv/bin/python -m collector
.venv/bin/python -m pytest collector
.venv/bin/ruff check collector
```

Тесты не требуют базы и сети: источники и хранилище в них подменяются.

Настройки — в `.env.example`, раздел «Сборщик данных».

Неуспешный суточный опрос фиата повторяется через `COLLECTOR_FIAT_RETRY_INTERVAL`.

Журнал пишется в STDOUT в формате JSON. По SIGINT/SIGTERM начатый цикл
дорабатывает, затем закрываются соединения.
