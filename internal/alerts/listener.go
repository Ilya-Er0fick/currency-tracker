package alerts

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Channel — канал уведомлений PostgreSQL, в который сборщик сообщает
// о сохранённой свече. Канал выбран вместо отдельной очереди сознательно:
// сборщик и сервер и так оба подключены к базе, и лишнего посредника
// заводить не требуется.
const Channel = "candles"

// candleEvent — полезная нагрузка уведомления. Для проверки правил нужен
// только актив: все значения сервер возьмёт из базы сам.
type candleEvent struct {
	AssetID int `json:"asset_id"`
}

type Listener struct {
	pool    *pgxpool.Pool
	service *Service
	logger  *slog.Logger
}

func NewListener(pool *pgxpool.Pool, service *Service, logger *slog.Logger) *Listener {
	return &Listener{pool: pool, service: service, logger: logger}
}

// Run слушает канал до отмены контекста.
//
// Соединение берётся из пула и удерживается: LISTEN действует в пределах
// одного соединения, и вернуть его в пул означало бы потерять подписку.
// При обрыве подписка восстанавливается с паузой, иначе при недоступной
// базе цикл крутился бы вхолостую на полной скорости.
func (l *Listener) Run(ctx context.Context) {
	const retryDelay = 5 * time.Second

	for {
		if err := l.listen(ctx); err != nil && !errors.Is(err, context.Canceled) {
			l.logger.Error("alert listener stopped", "error", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(retryDelay):
		}
	}
}

func (l *Listener) listen(ctx context.Context) error {
	conn, err := l.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "LISTEN "+Channel); err != nil {
		return err
	}
	l.logger.Info("alert listener started", "channel", Channel)

	for {
		notification, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}

		var event candleEvent
		if err := json.Unmarshal([]byte(notification.Payload), &event); err != nil {
			l.logger.Error("malformed candle event", "payload", notification.Payload)
			continue
		}

		// Ошибка проверки одного актива не должна снимать подписку:
		// следующие котировки обрабатывать всё равно нужно.
		if _, err := l.service.CheckAsset(ctx, event.AssetID); err != nil {
			l.logger.Error("check alerts", "asset", event.AssetID, "error", err)
		}
	}
}
