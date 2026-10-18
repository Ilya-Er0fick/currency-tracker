// Утилита заполняет базу синтетическими котировками, чтобы серверную
// и клиентскую части можно было разрабатывать до готовности сборщика.
package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/narumov-diyar/currency-tracker/internal/devdata"
)

func main() {
	days := flag.Int("days", 2, "глубина истории в сутках")
	seed := flag.Int64("seed", 1, "зерно генератора, одинаковое зерно даёт одинаковые ряды")
	flag.Parse()

	if err := run(*days, *seed); err != nil {
		fmt.Fprintln(os.Stderr, "ошибка:", err)
		os.Exit(1)
	}
}

func run(days int, seed int64) error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL is not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()

	assets, err := loadAssets(ctx, pool)
	if err != nil {
		return err
	}
	if len(assets) == 0 {
		return fmt.Errorf("asset catalog is empty, run the api first to apply migrations")
	}

	rnd := rand.New(rand.NewSource(seed))
	span := time.Duration(days) * 24 * time.Hour
	now := time.Now().UTC()

	var total int64
	for _, asset := range assets {
		candles := devdata.Generate(asset, span, now, rnd)
		inserted, err := insert(ctx, pool, candles)
		if err != nil {
			return fmt.Errorf("%s: %w", asset.Symbol, err)
		}
		total += inserted
	}

	fmt.Printf("активов: %d, свечей записано: %d, глубина: %d сут.\n", len(assets), total, days)
	return nil
}

func loadAssets(ctx context.Context, pool *pgxpool.Pool) ([]devdata.Asset, error) {
	rows, err := pool.Query(ctx, `SELECT id, symbol, resolution_seconds FROM assets ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var assets []devdata.Asset
	for rows.Next() {
		var a devdata.Asset
		if err := rows.Scan(&a.ID, &a.Symbol, &a.ResolutionSeconds); err != nil {
			return nil, err
		}
		assets = append(assets, a)
	}
	return assets, rows.Err()
}

// Повторный запуск не плодит дубли: уникальный ключ по активу, разрешению
// и времени отсекает уже записанные свечи.
func insert(ctx context.Context, pool *pgxpool.Pool, candles []devdata.Candle) (int64, error) {
	batch := &pgx.Batch{}
	for _, c := range candles {
		batch.Queue(`
			INSERT INTO price_candles
				(asset_id, resolution, collected_at, open_price, high_price, low_price, close_price, volume, turnover)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (asset_id, resolution, collected_at) DO NOTHING`,
			c.AssetID, c.Resolution, c.CollectedAt, c.Open, c.High, c.Low, c.Close, nullable(c.Volume), nullable(c.Turnover))
	}

	results := pool.SendBatch(ctx, batch)
	defer results.Close()

	var inserted int64
	for range candles {
		tag, err := results.Exec()
		if err != nil {
			return inserted, err
		}
		inserted += tag.RowsAffected()
	}
	return inserted, nil
}

func nullable(v float64) any {
	if v == 0 {
		return nil
	}
	return v
}
