package storage

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrAlertNotFound = errors.New("alert rule not found")

// Цель правила — ряд: либо курс актива в USDT, либо кросс-курс пары.
const (
	TargetAsset = "asset"
	TargetPair  = "pair"
)

// Условие либо о достижении уровня, либо об изменении на процент за интервал.
const (
	ConditionThreshold = "threshold"
	ConditionPercent   = "percent"
)

const (
	StatusActive    = "active"
	StatusTriggered = "triggered"
)

type AlertRule struct {
	ID            int64     `json:"id"`
	TargetType    string    `json:"target_type"`
	Symbol        *string   `json:"symbol,omitempty"`
	PairID        *int64    `json:"pair_id,omitempty"`
	Pair          *string   `json:"pair,omitempty"`
	ConditionType string    `json:"condition_type"`
	Direction     string    `json:"direction"`
	TargetValue   string    `json:"target_value"`
	WindowMinutes *int      `json:"window_minutes,omitempty"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
}

type NewAlertRule struct {
	UserID        int64
	TargetType    string
	AssetID       *int
	PairID        *int64
	ConditionType string
	Direction     string
	TargetValue   string
	WindowMinutes *int
}

type AlertUpdate struct {
	TargetValue   *string
	Direction     *string
	WindowMinutes *int
}

type Alerts struct {
	pool *pgxpool.Pool
}

func NewAlerts(pool *pgxpool.Pool) *Alerts {
	return &Alerts{pool: pool}
}

// selectRules собирает правило вместе с человекочитаемой целью: для правила
// на актив это его символ, для правила на пару — обе её стороны.
const selectRules = `
SELECT r.id, r.target_type, a.symbol, r.pair_id,
       CASE WHEN r.pair_id IS NOT NULL THEN pb.symbol || '/' || pq.symbol END AS pair,
       r.condition_type, r.direction, r.target_value::text, r.window_minutes,
       r.status, r.created_at
FROM alert_rules r
LEFT JOIN assets a ON a.id = r.asset_id
LEFT JOIN user_pairs p ON p.id = r.pair_id
LEFT JOIN assets pb ON pb.id = p.base_asset_id
LEFT JOIN assets pq ON pq.id = p.quote_asset_id`

func (a *Alerts) List(ctx context.Context, userID int64) ([]AlertRule, error) {
	rows, err := a.pool.Query(ctx, selectRules+`
		WHERE r.user_id = $1
		ORDER BY r.created_at DESC, r.id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rules := make([]AlertRule, 0, 16)
	for rows.Next() {
		rule, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func (a *Alerts) Create(ctx context.Context, rule NewAlertRule) (AlertRule, error) {
	var id int64
	err := a.pool.QueryRow(ctx, `
		INSERT INTO alert_rules
			(user_id, target_type, asset_id, pair_id, condition_type, direction, target_value, window_minutes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id`,
		rule.UserID, rule.TargetType, rule.AssetID, rule.PairID,
		rule.ConditionType, rule.Direction, rule.TargetValue, rule.WindowMinutes).Scan(&id)
	if err != nil {
		return AlertRule{}, err
	}
	return a.byID(ctx, rule.UserID, id)
}

// Update возвращает правило в активное состояние: изменённое условие ещё
// не срабатывало, и прежняя отметка о срабатывании к нему не относится.
func (a *Alerts) Update(ctx context.Context, userID, ruleID int64, update AlertUpdate) (AlertRule, error) {
	tag, err := a.pool.Exec(ctx, `
		UPDATE alert_rules
		SET target_value   = coalesce($3, target_value),
		    direction      = coalesce($4, direction),
		    window_minutes = coalesce($5, window_minutes),
		    status         = $6
		WHERE id = $1 AND user_id = $2`,
		ruleID, userID, update.TargetValue, update.Direction, update.WindowMinutes, StatusActive)
	if err != nil {
		return AlertRule{}, err
	}
	if tag.RowsAffected() == 0 {
		return AlertRule{}, ErrAlertNotFound
	}
	return a.byID(ctx, userID, ruleID)
}

// Delete ограничен владельцем: иначе по чужому идентификатору можно было бы
// удалить правило другого пользователя.
func (a *Alerts) Delete(ctx context.Context, userID, ruleID int64) error {
	tag, err := a.pool.Exec(ctx,
		`DELETE FROM alert_rules WHERE id = $1 AND user_id = $2`, ruleID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAlertNotFound
	}
	return nil
}

func (a *Alerts) byID(ctx context.Context, userID, ruleID int64) (AlertRule, error) {
	rows, err := a.pool.Query(ctx, selectRules+` WHERE r.id = $1 AND r.user_id = $2`, ruleID, userID)
	if err != nil {
		return AlertRule{}, err
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return AlertRule{}, err
		}
		return AlertRule{}, ErrAlertNotFound
	}
	return scanRule(rows)
}

func scanRule(rows pgx.Rows) (AlertRule, error) {
	var rule AlertRule
	err := rows.Scan(&rule.ID, &rule.TargetType, &rule.Symbol, &rule.PairID, &rule.Pair,
		&rule.ConditionType, &rule.Direction, &rule.TargetValue, &rule.WindowMinutes,
		&rule.Status, &rule.CreatedAt)
	return rule, err
}
