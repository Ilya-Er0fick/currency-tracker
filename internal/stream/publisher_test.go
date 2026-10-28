package stream

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

type recorder struct {
	mu       sync.Mutex
	payloads []string
	err      error
}

func (r *recorder) notify(_ context.Context, payload string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.payloads = append(r.payloads, payload)
	return nil
}

func (r *recorder) seen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.payloads...)
}

// Интервалы задаются полями, иначе проверка повтора по сроку заняла бы
// десять секунд реального времени.
func newTestPublisher(hub *Hub, notify Notify, poll, heartbeat time.Duration) *Publisher {
	p := NewPublisher(hub, notify, slog.New(slog.NewTextHandler(io.Discard, nil)))
	p.poll, p.heartbeat = poll, heartbeat
	return p
}

// waitFor ждёт условия вместо фиксированной паузы: публикация идёт
// в отдельной горутине, и жёсткое ожидание сделало бы тест то медленным,
// то шатким.
func waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if done() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("не дождались: %s", what)
}

func TestPublisherSendsSortedList(t *testing.T) {
	hub := NewHub(10)
	rec := &recorder{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go newTestPublisher(hub, rec.notify, time.Millisecond, time.Hour).Run(ctx)

	// Порядок в Hub произвольный, поэтому подписка идёт в обратном порядке.
	hub.Subscribe(7)
	hub.Subscribe(2)

	waitFor(t, "список с обоими активами", func() bool {
		seen := rec.seen()
		return len(seen) > 0 && seen[len(seen)-1] == "[2,7]"
	})
}

// Пустой список должен быть массивом, а не null: сборщик принимает
// только массив и на null ответит разбором с ошибкой.
func TestPublisherSendsEmptyArrayWhenNobodyWatches(t *testing.T) {
	rec := &recorder{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go newTestPublisher(NewHub(10), rec.notify, time.Millisecond, time.Hour).Run(ctx)

	waitFor(t, "пустой список", func() bool {
		seen := rec.seen()
		return len(seen) > 0 && seen[0] == "[]"
	})
}

// Без изменений список всё равно повторяется: сборщик считает его
// устаревшим после 30 секунд молчания и закрывает соединение с биржей.
func TestPublisherRepeatsListOnHeartbeat(t *testing.T) {
	rec := &recorder{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go newTestPublisher(NewHub(10), rec.notify, time.Millisecond, 5*time.Millisecond).Run(ctx)

	waitFor(t, "повтор того же списка", func() bool {
		seen := rec.seen()
		return len(seen) >= 3 && seen[0] == "[]" && seen[1] == "[]" && seen[2] == "[]"
	})
}

// Пока список не меняется, отправка идёт по сроку, а не каждый проход:
// иначе при опросе раз в секунду канал получал бы сообщение ежесекундно.
func TestPublisherStaysQuietBetweenHeartbeats(t *testing.T) {
	rec := &recorder{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go newTestPublisher(NewHub(10), rec.notify, time.Millisecond, time.Hour).Run(ctx)

	waitFor(t, "первую отправку", func() bool { return len(rec.seen()) > 0 })
	time.Sleep(50 * time.Millisecond)

	if got := len(rec.seen()); got != 1 {
		t.Errorf("отправок %d, за срок без изменений ожидалась одна", got)
	}
}

func TestPublisherReactsToUnsubscribe(t *testing.T) {
	hub := NewHub(10)
	rec := &recorder{}
	sub := hub.Subscribe(3)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go newTestPublisher(hub, rec.notify, time.Millisecond, time.Hour).Run(ctx)

	waitFor(t, "список со зрителем", func() bool {
		seen := rec.seen()
		return len(seen) > 0 && seen[0] == "[3]"
	})

	hub.Unsubscribe(sub)
	waitFor(t, "пустой список после ухода зрителя", func() bool {
		seen := rec.seen()
		return len(seen) > 1 && seen[len(seen)-1] == "[]"
	})
}

// Отказ отправки не запоминается: следующий проход повторит её, иначе
// после разрыва связи с базой сборщик навсегда остался бы без списка.
func TestPublisherRetriesAfterFailure(t *testing.T) {
	rec := &recorder{err: errFailed}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go newTestPublisher(NewHub(10), rec.notify, time.Millisecond, time.Hour).Run(ctx)

	time.Sleep(20 * time.Millisecond)
	if len(rec.seen()) != 0 {
		t.Fatal("отправка не должна была пройти")
	}

	rec.mu.Lock()
	rec.err = nil
	rec.mu.Unlock()

	waitFor(t, "отправку после восстановления", func() bool { return len(rec.seen()) > 0 })
}

var errFailed = errPublish("база недоступна")

type errPublish string

func (e errPublish) Error() string { return string(e) }
