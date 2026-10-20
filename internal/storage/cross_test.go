package storage

import (
	"context"
	"testing"
	"time"
)

func TestCrossSeriesCarriesForwardTheRareSide(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	if _, err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	btc, rub := assetID(t, pool, "BTC"), assetID(t, pool, "RUB")
	now := time.Now().UTC().Truncate(time.Minute)
	from, to := now.Add(-10*time.Minute), now.Add(time.Minute)

	// Биткоин обновляется поминутно.
	for i := 0; i < 5; i++ {
		insertCandle(t, pool, btc, 1, now.Add(time.Duration(-i)*time.Minute), "60000")
	}
	// Курс рубля один и старше всего окна: именно так выглядят выходные,
	// когда фиатный ряд не пополняется несколько суток подряд.
	insertCandle(t, pool, rub, 1440, now.Add(-72*time.Hour), "0.01")

	points, err := NewCandles(pool).CrossSeries(ctx, CrossSeriesParams{
		GridAssetID: btc, GridResolution: 1,
		CarriedAssetID: rub, CarriedResolution: 1440,
		GridIsBase: true,
		From:       from, To: to, Limit: 100,
	})
	if err != nil {
		t.Fatalf("CrossSeries: %v", err)
	}

	if len(points) != 5 {
		t.Fatalf("got %d points, want one per minute candle", len(points))
	}

	// 60000 / 0.01 = 6000000, то есть биткоин в рублях.
	for i, p := range points {
		if p.Rate != "6000000.00000000" {
			t.Fatalf("point %d: rate = %s, want the carried fiat value applied", i, p.Rate)
		}
	}

	// Точки идут по возрастанию времени, иначе график рисуется задом наперёд.
	for i := 1; i < len(points); i++ {
		if !points[i].CollectedAt.After(points[i-1].CollectedAt) {
			t.Fatalf("points are not ordered: %v then %v", points[i-1].CollectedAt, points[i].CollectedAt)
		}
	}
}

// Порядок деления задаётся не тем, чей ряд стал сеткой, а тем, какой актив
// базовый: иначе при фиате в числителе курс вышел бы перевёрнутым.
func TestCrossSeriesRespectsDivisionOrder(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	if _, err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	btc, usdt := assetID(t, pool, "BTC"), assetID(t, pool, "USDT")
	now := time.Now().UTC().Truncate(time.Minute)

	insertCandle(t, pool, btc, 1, now, "60000")
	insertCandle(t, pool, usdt, 1, now, "2")

	candles := NewCandles(pool)
	params := CrossSeriesParams{
		GridAssetID: btc, GridResolution: 1,
		CarriedAssetID: usdt, CarriedResolution: 1,
		GridIsBase: true,
		From:       now.Add(-time.Minute), To: now.Add(time.Minute), Limit: 10,
	}

	direct, err := candles.CrossSeries(ctx, params)
	if err != nil {
		t.Fatalf("CrossSeries: %v", err)
	}
	if len(direct) != 1 || direct[0].Rate != "30000.00000000" {
		t.Fatalf("rate = %v, want 30000", direct)
	}

	params.GridIsBase = false
	inverted, err := candles.CrossSeries(ctx, params)
	if err != nil {
		t.Fatalf("CrossSeries inverted: %v", err)
	}
	if len(inverted) != 1 || inverted[0].Rate != "0.00003333" {
		t.Fatalf("rate = %v, want the reciprocal", inverted)
	}
}

// Пока у редкой стороны нет ни одного значения, курса не существует:
// точки просто не появляются, вместо того чтобы считаться по нулю.
func TestCrossSeriesSkipsPointsWithoutCarriedValue(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	if _, err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	btc, rub := assetID(t, pool, "BTC"), assetID(t, pool, "RUB")
	now := time.Now().UTC().Truncate(time.Minute)

	insertCandle(t, pool, btc, 1, now.Add(-5*time.Minute), "60000")
	insertCandle(t, pool, btc, 1, now, "61000")
	// Курс рубля появляется только к концу окна.
	insertCandle(t, pool, rub, 1440, now.Add(-time.Minute), "0.0119047619")

	points, err := NewCandles(pool).CrossSeries(ctx, CrossSeriesParams{
		GridAssetID: btc, GridResolution: 1,
		CarriedAssetID: rub, CarriedResolution: 1440,
		GridIsBase: true,
		From:       now.Add(-10 * time.Minute), To: now.Add(time.Minute), Limit: 100,
	})
	if err != nil {
		t.Fatalf("CrossSeries: %v", err)
	}

	if len(points) != 1 {
		t.Fatalf("got %d points, want only the one after the fiat rate appeared", len(points))
	}
	if !points[0].CollectedAt.Equal(now) {
		t.Errorf("point at %v, want %v", points[0].CollectedAt, now)
	}
}

// Ограничение отбирает самые свежие точки: при обрезании должны теряться
// старые, а не последние.
func TestCrossSeriesKeepsNewestPointsWhenLimited(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	if _, err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	btc, usdt := assetID(t, pool, "BTC"), assetID(t, pool, "USDT")
	now := time.Now().UTC().Truncate(time.Minute)

	for i := 0; i < 10; i++ {
		insertCandle(t, pool, btc, 1, now.Add(time.Duration(-i)*time.Minute), "60000")
	}
	insertCandle(t, pool, usdt, 1, now.Add(-20*time.Minute), "1")

	points, err := NewCandles(pool).CrossSeries(ctx, CrossSeriesParams{
		GridAssetID: btc, GridResolution: 1,
		CarriedAssetID: usdt, CarriedResolution: 1,
		GridIsBase: true,
		From:       now.Add(-time.Hour), To: now.Add(time.Minute), Limit: 3,
	})
	if err != nil {
		t.Fatalf("CrossSeries: %v", err)
	}

	if len(points) != 3 {
		t.Fatalf("got %d points, want 3", len(points))
	}
	if !points[2].CollectedAt.Equal(now) {
		t.Errorf("last point at %v, want the newest candle at %v", points[2].CollectedAt, now)
	}
}
