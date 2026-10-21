package storage

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrPairExists   = errors.New("pair is already in the watchlist")
	ErrPairNotFound = errors.New("pair not found")
)

type PairSide struct {
	Symbol string `json:"symbol"`
	Type   string `json:"type"`
}

type Pair struct {
	ID        int64      `json:"id"`
	Base      PairSide   `json:"base"`
	Quote     PairSide   `json:"quote"`
	Rate      *string    `json:"rate"`
	UpdatedAt *time.Time `json:"updated_at"`
	Stale     bool       `json:"stale"`
}

type Pairs struct {
	pool *pgxpool.Pool
}

func NewPairs(pool *pgxpool.Pool) *Pairs {
	return &Pairs{pool: pool}
}

// listQuery подбирает к каждой паре последнюю свечу обеих сторон.
//
// Боковое соединение берёт по одной строке на актив, попадая прямо в индекс
// (asset_id, resolution, collected_at DESC), поэтому стоимость не зависит
// от глубины истории. Разрешение вычисляется из справочника: у криптовалют
// это минутные свечи, у фиатных валют дневные.
//
// Деление выполняется в numeric, а наружу уходит строкой: курс BTC к USDT
// не укладывается в double без потери значащих цифр.
//
// Свежесть считается по наиболее отставшей стороне: пара не свежее
// худшего из двух своих рядов.
const listQuery = `
SELECT p.id,
       b.symbol, b.type, q.symbol, q.type,
       round(cb.close_price / nullif(cq.close_price, 0), 8)::text AS rate,
       least(cb.collected_at, cq.collected_at) AS updated_at,
       coalesce(
           now() - cb.collected_at > make_interval(secs => b.resolution_seconds * 2)
           OR now() - cq.collected_at > make_interval(secs => q.resolution_seconds * 2),
           true
       ) AS stale
FROM user_pairs p
JOIN assets b ON b.id = p.base_asset_id
JOIN assets q ON q.id = p.quote_asset_id
LEFT JOIN LATERAL (
    SELECT c.close_price, c.collected_at
    FROM price_candles c
    WHERE c.asset_id = b.id AND c.resolution = b.resolution_seconds / 60
    ORDER BY c.collected_at DESC
    LIMIT 1
) cb ON true
LEFT JOIN LATERAL (
    SELECT c.close_price, c.collected_at
    FROM price_candles c
    WHERE c.asset_id = q.id AND c.resolution = q.resolution_seconds / 60
    ORDER BY c.collected_at DESC
    LIMIT 1
) cq ON true
WHERE p.user_id = $1
ORDER BY p.created_at, p.id`

func (p *Pairs) List(ctx context.Context, userID int64) ([]Pair, error) {
	rows, err := p.pool.Query(ctx, listQuery, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	pairs := make([]Pair, 0, 16)
	for rows.Next() {
		var pair Pair
		if err := rows.Scan(&pair.ID, &pair.Base.Symbol, &pair.Base.Type,
			&pair.Quote.Symbol, &pair.Quote.Type,
			&pair.Rate, &pair.UpdatedAt, &pair.Stale); err != nil {
			return nil, err
		}
		pairs = append(pairs, pair)
	}
	return pairs, rows.Err()
}

func (p *Pairs) Create(ctx context.Context, userID int64, baseAssetID, quoteAssetID int) (int64, error) {
	var id int64
	err := p.pool.QueryRow(ctx,
		`INSERT INTO user_pairs (user_id, base_asset_id, quote_asset_id)
		 VALUES ($1, $2, $3) RETURNING id`,
		userID, baseAssetID, quoteAssetID).Scan(&id)

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return 0, ErrPairExists
	}
	if err != nil {
		return 0, err
	}
	return id, nil
}

// Delete ограничен владельцем: иначе по чужому идентификатору можно было бы
// удалить пару другого пользователя. История котировок не затрагивается —
// она общая и к пользовательским спискам не привязана.
func (p *Pairs) Delete(ctx context.Context, userID, pairID int64) error {
	tag, err := p.pool.Exec(ctx,
		`DELETE FROM user_pairs WHERE id = $1 AND user_id = $2`, pairID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrPairNotFound
	}
	return nil
}

// Owned проверяет, что пара принадлежит пользователю. Внешний ключ на
// user_pairs этого не гарантирует: он допускает ссылку на чужую пару,
// поэтому правило оповещения приходится проверять отдельно.
func (p *Pairs) Owned(ctx context.Context, userID, pairID int64) (bool, error) {
	var exists bool
	err := p.pool.QueryRow(ctx,
		`SELECT exists(SELECT 1 FROM user_pairs WHERE id = $1 AND user_id = $2)`,
		pairID, userID).Scan(&exists)
	return exists, err
}
