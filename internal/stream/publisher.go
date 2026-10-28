package stream

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// WatchedChannel — канал, в котором сервер перечисляет активы с хотя бы одним
// зрителем. Сборщик держит подписку на бирже только на них: подписка на весь
// каталог даёт 18 ГБ входящего трафика в сутки против 3–6 ГБ по требованию.
const WatchedChannel = "watched"

// Сборщик считает список устаревшим после 30 секунд молчания и закрывает
// соединение с биржей, поэтому подтверждение уходит заметно чаще.
const watchedHeartbeat = 10 * time.Second

// Список меняется в обработчиках подключений, и будить публикацию прямо
// оттуда значило бы связывать их с базой. Опрос раз в секунду даёт
// достаточную отзывчивость и оставляет Hub независимым.
const watchedPollInterval = time.Second

// Notify отправляет сообщение сборщику. Отдельный тип, а не пул соединений,
// чтобы проверять публикацию без базы.
type Notify func(ctx context.Context, payload string) error

func PostgresNotify(pool *pgxpool.Pool) Notify {
	return func(ctx context.Context, payload string) error {
		_, err := pool.Exec(ctx, "SELECT pg_notify($1, $2)", WatchedChannel, payload)
		return err
	}
}

// Publisher сообщает сборщику, какие активы сейчас смотрят.
//
// Отправляется полное состояние, а не изменения: сборщик может запуститься
// позже сервера или пропустить сообщение при переподключении, а полный
// список идемпотентен и восстанавливается сам.
type Publisher struct {
	hub       *Hub
	notify    Notify
	logger    *slog.Logger
	poll      time.Duration
	heartbeat time.Duration
}

func NewPublisher(hub *Hub, notify Notify, logger *slog.Logger) *Publisher {
	return &Publisher{
		hub:       hub,
		notify:    notify,
		logger:    logger,
		poll:      watchedPollInterval,
		heartbeat: watchedHeartbeat,
	}
}

func (p *Publisher) Run(ctx context.Context) {
	ticker := time.NewTicker(p.poll)
	defer ticker.Stop()

	var published string
	var publishedAt time.Time

	for {
		payload, err := p.payload()
		if err != nil {
			p.logger.Error("watched list not serialized", "error", err)
		} else if payload != published || time.Since(publishedAt) >= p.heartbeat {
			if err := p.notify(ctx, payload); err != nil {
				// Отказ не запоминается: следующий проход повторит отправку,
				// а сборщик до тех пор доживёт на своём сроке ожидания.
				if ctx.Err() == nil {
					p.logger.Error("watched list not published", "error", err)
				}
			} else {
				if payload != published {
					p.logger.Info("watched list changed", "assets", payload)
				}
				published, publishedAt = payload, time.Now()
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// payload отдаёт отсортированный список: порядок в Hub произвольный, и без
// сортировки одно и то же множество выглядело бы изменившимся.
func (p *Publisher) payload() (string, error) {
	assets := p.hub.Watched()
	slices.Sort(assets)

	// Пустой список должен стать [], а не null: сборщик принимает только
	// массив. Watched возвращает непустой срез, но полагаться на это
	// в формате обмена не стоит.
	if assets == nil {
		assets = []int{}
	}

	raw, err := json.Marshal(assets)
	return string(raw), err
}
