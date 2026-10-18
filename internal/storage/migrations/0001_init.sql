CREATE TABLE users (
    id            bigserial   PRIMARY KEY,
    email         text        NOT NULL UNIQUE,
    password_hash text        NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);

-- Разрешение и признак потокового режима заданы здесь, а не в коде:
-- добавление актива нового типа не должно требовать правки сервиса.
CREATE TABLE assets (
    id                 serial  PRIMARY KEY,
    symbol             text    NOT NULL UNIQUE,
    type               text    NOT NULL CHECK (type IN ('crypto', 'fiat')),
    source             text    NOT NULL CHECK (source IN ('bybit', 'er-api', 'cbr')),
    resolution_seconds integer NOT NULL CHECK (resolution_seconds > 0),
    supports_realtime  boolean NOT NULL
);

-- Одна таблица хранит и минутные свечи, и агрегаты свёртки, и вырожденные
-- свечи фиатных валют, у которых все четыре цены совпадают.
-- resolution задаётся в минутах: 1, 5, 60, 1440.
CREATE TABLE price_candles (
    id           bigserial       PRIMARY KEY,
    asset_id     integer         NOT NULL REFERENCES assets (id),
    resolution   smallint        NOT NULL CHECK (resolution IN (1, 5, 60, 1440)),
    collected_at timestamptz     NOT NULL,
    open_price   numeric(18, 8)  NOT NULL,
    high_price   numeric(18, 8)  NOT NULL,
    low_price    numeric(18, 8)  NOT NULL,
    close_price  numeric(18, 8)  NOT NULL,
    volume       numeric(24, 8),
    turnover     numeric(24, 8),
    UNIQUE (asset_id, resolution, collected_at)
);

-- Основной индекс выборки: период фиксирует актив и разрешение,
-- а диапазон времени читается как упорядоченный участок индекса.
CREATE INDEX price_candles_series_idx
    ON price_candles (asset_id, resolution, collected_at DESC);

CREATE TABLE user_pairs (
    id             bigserial   PRIMARY KEY,
    user_id        bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    base_asset_id  integer     NOT NULL REFERENCES assets (id),
    quote_asset_id integer     NOT NULL REFERENCES assets (id),
    created_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, base_asset_id, quote_asset_id),
    CHECK (base_asset_id <> quote_asset_id)
);

CREATE INDEX user_pairs_user_idx ON user_pairs (user_id);

CREATE TABLE alert_rules (
    id             bigserial      PRIMARY KEY,
    user_id        bigint         NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    target_type    text           NOT NULL CHECK (target_type IN ('asset', 'pair')),
    asset_id       integer        REFERENCES assets (id),
    pair_id        bigint         REFERENCES user_pairs (id) ON DELETE CASCADE,
    condition_type text           NOT NULL CHECK (condition_type IN ('threshold', 'percent')),
    direction      text           NOT NULL CHECK (direction IN ('above', 'below')),
    target_value   numeric(18, 8) NOT NULL CHECK (target_value > 0),
    window_minutes integer        CHECK (window_minutes > 0),
    status         text           NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'triggered')),
    created_at     timestamptz    NOT NULL DEFAULT now(),

    -- Цель правила — ровно одна: либо актив, либо пара.
    CHECK (
        (target_type = 'asset' AND asset_id IS NOT NULL AND pair_id IS NULL)
        OR (target_type = 'pair' AND pair_id IS NOT NULL AND asset_id IS NULL)
    ),
    -- Процентное правило бессмысленно без интервала наблюдения.
    CHECK (condition_type <> 'percent' OR window_minutes IS NOT NULL)
);

-- Путь проверки правил: при новой свече выбираются активные правила по активу.
CREATE INDEX alert_rules_check_idx ON alert_rules (asset_id, status);
CREATE INDEX alert_rules_pair_idx ON alert_rules (pair_id, status);
CREATE INDEX alert_rules_user_idx ON alert_rules (user_id);

CREATE TABLE notifications (
    id            bigserial   PRIMARY KEY,
    user_id       bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    alert_rule_id bigint      NOT NULL REFERENCES alert_rules (id) ON DELETE CASCADE,
    triggered_at  timestamptz NOT NULL DEFAULT now(),
    seen          boolean     NOT NULL DEFAULT false
);

CREATE INDEX notifications_unseen_idx ON notifications (user_id, seen);
