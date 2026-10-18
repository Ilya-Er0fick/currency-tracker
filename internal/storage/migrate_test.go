package storage

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Тест работает с настоящей базой: схему и ограничения иначе не проверить.
// Без TEST_DATABASE_URL пропускается, поэтому сборка без базы не падает.
//
// Каждый тест получает собственную схему и удаляет её за собой: так прогон
// не зависит от предыдущего и не трогает данные разработки в public.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	schema := "test_" + strings.ToLower(strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()))

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+schema)
		return err
	}

	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer admin.Close()

	if _, err := admin.Exec(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE; CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect to schema: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
		cleanup, err := pgxpool.New(ctx, dsn)
		if err != nil {
			return
		}
		defer cleanup.Close()
		_, _ = cleanup.Exec(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
	})

	return pool
}

func TestMigrateAppliesAndIsRepeatable(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)

	applied, err := Migrate(ctx, pool)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if len(applied) == 0 {
		t.Fatal("expected migrations to be applied on a clean schema")
	}

	again, err := Migrate(ctx, pool)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if len(again) != 0 {
		t.Errorf("second run applied %v, want nothing", again)
	}
}

func TestSeedFillsAssetCatalog(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)

	if _, err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var crypto, fiat int
	err := pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE type = 'crypto'),
		       count(*) FILTER (WHERE type = 'fiat')
		FROM assets`).Scan(&crypto, &fiat)
	if err != nil {
		t.Fatalf("count assets: %v", err)
	}

	if crypto != 30 {
		t.Errorf("crypto assets = %d, want 30", crypto)
	}
	if fiat != 10 {
		t.Errorf("fiat assets = %d, want 10", fiat)
	}

	var realtime bool
	if err := pool.QueryRow(ctx, `SELECT supports_realtime FROM assets WHERE symbol = 'RUB'`).Scan(&realtime); err != nil {
		t.Fatalf("read RUB: %v", err)
	}
	if realtime {
		t.Error("fiat assets must not support the realtime mode")
	}
}

func TestSchemaRejectsInvalidRows(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)

	if _, err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var userID int64
	err := pool.QueryRow(ctx, `
		INSERT INTO users (email, password_hash) VALUES ('a@example.com', 'hash') RETURNING id`).Scan(&userID)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}

	var btc, usdt int
	if err := pool.QueryRow(ctx, `SELECT id FROM assets WHERE symbol = 'BTC'`).Scan(&btc); err != nil {
		t.Fatalf("read BTC: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM assets WHERE symbol = 'USDT'`).Scan(&usdt); err != nil {
		t.Fatalf("read USDT: %v", err)
	}

	tests := []struct {
		name string
		sql  string
		args []any
	}{
		{
			name: "pair of an asset with itself",
			sql:  `INSERT INTO user_pairs (user_id, base_asset_id, quote_asset_id) VALUES ($1, $2, $2)`,
			args: []any{userID, btc},
		},
		{
			name: "duplicate pair for the same user",
			sql:  `INSERT INTO user_pairs (user_id, base_asset_id, quote_asset_id) VALUES ($1, $2, $3), ($1, $2, $3)`,
			args: []any{userID, btc, usdt},
		},
		{
			name: "percent rule without a window",
			sql: `INSERT INTO alert_rules (user_id, target_type, asset_id, condition_type, direction, target_value)
			      VALUES ($1, 'asset', $2, 'percent', 'above', 5)`,
			args: []any{userID, btc},
		},
		{
			name: "asset rule pointing at a pair",
			sql: `INSERT INTO alert_rules (user_id, target_type, pair_id, condition_type, direction, target_value)
			      VALUES ($1, 'asset', 1, 'threshold', 'above', 5)`,
			args: []any{userID},
		},
		{
			name: "candle with an unknown resolution",
			sql: `INSERT INTO price_candles (asset_id, resolution, collected_at, open_price, high_price, low_price, close_price)
			      VALUES ($1, 7, now(), 1, 1, 1, 1)`,
			args: []any{btc},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, tt.sql, tt.args...); err == nil {
				t.Error("expected the database to reject the row")
			}
		})
	}
}
