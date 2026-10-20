package storage

import (
	"context"
	"time"
)

type CrossPoint struct {
	CollectedAt time.Time
	Rate        string
}

// CrossSeriesParams описывает, из какого ряда берётся сетка времени,
// а какой подтягивается к ней переносом последнего значения.
type CrossSeriesParams struct {
	GridAssetID       int
	GridResolution    int16
	CarriedAssetID    int
	CarriedResolution int16
	// GridIsBase сообщает, в каком порядке делить: курс считается
	// как цена базового актива, делённая на цену котируемого.
	GridIsBase bool
	From, To   time.Time
	Limit      int
}

// crossQuery строит кросс-курс целиком в базе.
//
// Сетка берётся у более частого ряда, а редкий подтягивается боковым
// соединением: для каждой точки сетки выбирается последнее известное
// значение на этот момент. Так пара из криптовалюты и фиата меняется
// поминутно, хотя курс валюты обновляется раз в сутки, и так же
// закрываются выходные, когда фиатный ряд не пополняется.
//
// Деление выполняется в numeric и отдаётся строкой: курс BTC к рублю
// не укладывается в double без потери значащих цифр.
const crossQuery = `
SELECT grid.collected_at,
       round(
           CASE WHEN $7 THEN grid.close_price / nullif(carried.close_price, 0)
                ELSE carried.close_price / nullif(grid.close_price, 0)
           END, 8)::text AS rate
FROM (
    SELECT collected_at, close_price
    FROM price_candles
    WHERE asset_id = $1 AND resolution = $2 AND collected_at >= $5 AND collected_at < $6
    ORDER BY collected_at DESC
    LIMIT $8
) grid
JOIN LATERAL (
    SELECT close_price
    FROM price_candles
    WHERE asset_id = $3 AND resolution = $4 AND collected_at <= grid.collected_at
    ORDER BY collected_at DESC
    LIMIT 1
) carried ON true
ORDER BY grid.collected_at`

func (c *Candles) CrossSeries(ctx context.Context, p CrossSeriesParams) ([]CrossPoint, error) {
	rows, err := c.pool.Query(ctx, crossQuery,
		p.GridAssetID, p.GridResolution,
		p.CarriedAssetID, p.CarriedResolution,
		p.From, p.To, p.GridIsBase, p.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	points := make([]CrossPoint, 0, p.Limit)
	for rows.Next() {
		var point CrossPoint
		var rate *string
		if err := rows.Scan(&point.CollectedAt, &rate); err != nil {
			return nil, err
		}
		// Нулевая цена котируемого актива делает курс неопределённым;
		// такая точка просто пропускается.
		if rate == nil {
			continue
		}
		point.Rate = *rate
		points = append(points, point)
	}
	return points, rows.Err()
}
