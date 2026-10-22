package storage

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Notification struct {
	ID          int64     `json:"id"`
	AlertRuleID int64     `json:"alert_rule_id"`
	Target      string    `json:"target"`
	Direction   string    `json:"direction"`
	TargetValue string    `json:"target_value"`
	TriggeredAt time.Time `json:"triggered_at"`
}

type Notifications struct {
	pool *pgxpool.Pool
}

func NewNotifications(pool *pgxpool.Pool) *Notifications {
	return &Notifications{pool: pool}
}

// Unseen отдаёт непросмотренные уведомления вместе с описанием цели:
// сам по себе идентификатор правила пользователю ничего не говорит.
func (n *Notifications) Unseen(ctx context.Context, userID int64) ([]Notification, error) {
	rows, err := n.pool.Query(ctx, `
		SELECT n.id, n.alert_rule_id,
		       coalesce(a.symbol, pb.symbol || '/' || pq.symbol) AS target,
		       r.direction, r.target_value::text, n.triggered_at
		FROM notifications n
		JOIN alert_rules r ON r.id = n.alert_rule_id
		LEFT JOIN assets a ON a.id = r.asset_id
		LEFT JOIN user_pairs p ON p.id = r.pair_id
		LEFT JOIN assets pb ON pb.id = p.base_asset_id
		LEFT JOIN assets pq ON pq.id = p.quote_asset_id
		WHERE n.user_id = $1 AND NOT n.seen
		ORDER BY n.triggered_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]Notification, 0, 16)
	for rows.Next() {
		var item Notification
		if err := rows.Scan(&item.ID, &item.AlertRuleID, &item.Target,
			&item.Direction, &item.TargetValue, &item.TriggeredAt); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	return list, rows.Err()
}

// MarkSeen гасит значок непрочитанного. Сами уведомления остаются:
// пользователь должен видеть историю срабатываний.
func (n *Notifications) MarkSeen(ctx context.Context, userID int64) error {
	_, err := n.pool.Exec(ctx,
		`UPDATE notifications SET seen = true WHERE user_id = $1 AND NOT seen`, userID)
	return err
}
