package httpapi

import (
	"net/http"
	"testing"
	"time"

	"github.com/narumov-diyar/currency-tracker/internal/storage"
)

func candlesAt(closes ...string) []storage.Candle {
	start := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	candles := make([]storage.Candle, len(closes))
	for i, c := range closes {
		candles[i] = storage.Candle{
			CollectedAt: start.Add(time.Duration(i) * time.Minute),
			Open:        c, High: c, Low: c, Close: c,
		}
	}
	return candles
}

func TestHistoryReturnsAnalytics(t *testing.T) {
	candles := &stubCandles{candles: candlesAt("100", "110", "120", "130", "140")}
	handler, token := newHistoryEnv(t, candles)

	rec := getHistory(t, handler, "base=BTC&quote=USDT&period=1h", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}

	response := decodeHistory(t, rec.Body.Bytes())
	if response.Analytics.BaseChange == nil {
		t.Fatal("изменение базового актива должно считаться")
	}
	if got := *response.Analytics.BaseChange; got < 39.9 || got > 40.1 {
		t.Errorf("изменение = %v, ожидалось 40", got)
	}
	// Обе стороны здесь один и тот же ряд, поэтому разность нулевая.
	if response.Analytics.ChangeDiff == nil || *response.Analytics.ChangeDiff != 0 {
		t.Errorf("разность = %v, ожидался ноль", response.Analytics.ChangeDiff)
	}
}

// Показатели считаются по рядам сторон, а не по кривой отношения,
// поэтому в режиме кросс-курса они тоже на месте.
func TestAnalyticsPresentInCrossMode(t *testing.T) {
	candles := &stubCandles{candles: candlesAt("100", "110", "120", "130")}
	handler, token := newHistoryEnv(t, candles)

	rec := getHistory(t, handler, "base=BTC&quote=USDT&period=1h&mode=cross", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}

	response := decodeHistory(t, rec.Body.Bytes())
	if response.Base != nil || response.Quote != nil {
		t.Error("в режиме кросс-курса ряды сторон не отдаются")
	}
	if response.Analytics.BaseChange == nil {
		t.Error("показатели должны считаться и в режиме кросс-курса")
	}
}

// Пустой период не должен ломать расчёт: показатели просто отсутствуют.
func TestAnalyticsEmptyWhenNoData(t *testing.T) {
	handler, token := newHistoryEnv(t, &stubCandles{})

	rec := getHistory(t, handler, "base=BTC&quote=USDT&period=1h", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	response := decodeHistory(t, rec.Body.Bytes())
	if response.Analytics.BaseChange != nil || response.Analytics.Correlation != nil {
		t.Error("без данных показателей быть не должно")
	}
}
