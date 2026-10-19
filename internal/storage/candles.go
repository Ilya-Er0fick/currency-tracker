package storage

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Candle отдаётся наружу в том же виде, в каком хранится.
// Цены — строки: деление и сравнение значений не укладываются в double
// без потери значащих цифр, а приводить их к числу незачем, клиент только рисует.
type Candle struct {
	CollectedAt time.Time
	Open        string
	High        string
	Low         string
	Close       string
	Volume      *string
	Turnover    *string
}

type Candles struct {
	pool *pgxpool.Pool
}

func NewCandles(pool *pgxpool.Pool) *Candles {
	return &Candles{pool: pool}
}

// Series возвращает свечи актива за окно времени по возрастанию времени.
//
// Условие полностью ложится на индекс (asset_id, resolution, collected_at DESC),
// поэтому стоимость запроса определяется числом возвращаемых строк, а не
// глубиной истории: запрос за неделю стоит столько же, сколько за час.
//
// Отбор идёт от конца окна, иначе при обрезании по limit терялись бы
// самые свежие свечи вместо самых старых.
func (c *Candles) Series(ctx context.Context, assetID int, resolution int16, from, to time.Time, limit int) ([]Candle, error) {
	rows, err := c.pool.Query(ctx, `
		SELECT collected_at, open_price::text, high_price::text, low_price::text,
		       close_price::text, volume::text, turnover::text
		FROM (
		    SELECT collected_at, open_price, high_price, low_price, close_price, volume, turnover
		    FROM price_candles
		    WHERE asset_id = $1 AND resolution = $2 AND collected_at >= $3 AND collected_at < $4
		    ORDER BY collected_at DESC
		    LIMIT $5
		) recent
		ORDER BY collected_at`,
		assetID, resolution, from, to, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	candles := make([]Candle, 0, limit)
	for rows.Next() {
		var candle Candle
		if err := rows.Scan(&candle.CollectedAt, &candle.Open, &candle.High,
			&candle.Low, &candle.Close, &candle.Volume, &candle.Turnover); err != nil {
			return nil, err
		}
		candles = append(candles, candle)
	}
	return candles, rows.Err()
}
