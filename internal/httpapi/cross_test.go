package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/narumov-diyar/currency-tracker/internal/storage"
)

func decodeHistory(t *testing.T, body []byte) historyResponse {
	t.Helper()

	var response historyResponse
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return response
}

func TestCompareModeIsDefault(t *testing.T) {
	handler, token := newHistoryEnv(t, &stubCandles{})

	response := decodeHistory(t, getHistory(t, handler, "base=BTC&quote=USDT&period=1h", token).Body.Bytes())
	if response.Mode != modeCompare {
		t.Errorf("mode = %q, want %q", response.Mode, modeCompare)
	}
	if response.Base == nil || response.Quote == nil {
		t.Error("compare mode must return both series")
	}
	if response.Cross != nil {
		t.Error("compare mode must not return a cross series")
	}
}

func TestCrossModeReturnsSingleCurve(t *testing.T) {
	at := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	candles := &stubCandles{crossPoints: []storage.CrossPoint{
		{CollectedAt: at, Rate: "5234000.12345678"},
	}}
	handler, token := newHistoryEnv(t, candles)

	rec := getHistory(t, handler, "base=BTC&quote=RUB&period=1w&mode=cross", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}

	response := decodeHistory(t, rec.Body.Bytes())
	if response.Base != nil || response.Quote != nil {
		t.Error("cross mode must not return separate series")
	}
	if response.Cross == nil {
		t.Fatal("cross mode must return a curve")
	}
	if response.Cross.Pair != "BTC/RUB" {
		t.Errorf("pair = %q, want BTC/RUB", response.Cross.Pair)
	}
	if len(response.Cross.Points) != 1 {
		t.Fatalf("got %d points, want 1", len(response.Cross.Points))
	}

	point := response.Cross.Points[0]
	if ts, ok := point[0].(float64); !ok || int64(ts) != at.UnixMilli() {
		t.Errorf("timestamp = %v, want %d", point[0], at.UnixMilli())
	}
	// Курс передаётся строкой: отношение крупных цен теряет точность в double.
	if rate, ok := point[1].(string); !ok || rate != "5234000.12345678" {
		t.Errorf("rate = %v, want the exact decimal as a string", point[1])
	}
}

// Сетка времени берётся у того актива, который обновляется чаще, иначе пара
// из криптовалюты и фиата меняла бы значение раз в сутки вместо раз в минуту.
func TestCrossGridFollowsTheFasterSide(t *testing.T) {
	tests := []struct {
		name           string
		query          string
		wantGridAsset  int
		wantResolution int16
		wantGridIsBase bool
	}{
		{
			name:           "фиат в знаменателе",
			query:          "base=BTC&quote=RUB&period=1w&mode=cross",
			wantGridAsset:  1, // BTC
			wantResolution: 60,
			wantGridIsBase: true,
		},
		{
			name:           "фиат в числителе",
			query:          "base=RUB&quote=BTC&period=1w&mode=cross",
			wantGridAsset:  1, // снова BTC, он частотнее
			wantResolution: 60,
			wantGridIsBase: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candles := &stubCandles{}
			handler, token := newHistoryEnv(t, candles)

			if rec := getHistory(t, handler, tt.query, token); rec.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", rec.Code, rec.Body)
			}
			if len(candles.crossParams) != 1 {
				t.Fatalf("made %d queries, want 1", len(candles.crossParams))
			}

			params := candles.crossParams[0]
			if params.GridAssetID != tt.wantGridAsset {
				t.Errorf("grid asset = %d, want %d", params.GridAssetID, tt.wantGridAsset)
			}
			if params.GridResolution != tt.wantResolution {
				t.Errorf("grid resolution = %d, want %d", params.GridResolution, tt.wantResolution)
			}
			// Порядок деления не зависит от того, чья сетка: курс всегда
			// считается как цена базового актива к цене котируемого.
			if params.GridIsBase != tt.wantGridIsBase {
				t.Errorf("GridIsBase = %v, want %v", params.GridIsBase, tt.wantGridIsBase)
			}
			if params.CarriedResolution != 1440 {
				t.Errorf("carried resolution = %d, want 1440", params.CarriedResolution)
			}
		})
	}
}

// Кривой нет, только когда период короче обновления ОБЕИХ сторон: сетку
// строить не из чего. Это пустой ответ, а не ошибка.
func TestCrossModeEmptyWhenPeriodTooShort(t *testing.T) {
	candles := &stubCandles{}
	handler, token := newHistoryEnv(t, candles)

	rec := getHistory(t, handler, "base=RUB&quote=KZT&period=1h&mode=cross", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	response := decodeHistory(t, rec.Body.Bytes())
	if response.Cross == nil {
		t.Fatal("cross must be present even when empty")
	}
	if len(response.Cross.Points) != 0 {
		t.Errorf("got %d points, want none", len(response.Cross.Points))
	}
	if len(candles.crossParams) != 0 {
		t.Error("an unsupported period must not query the database")
	}
}

func TestHistoryRejectsUnknownMode(t *testing.T) {
	handler, token := newHistoryEnv(t, &stubCandles{})

	rec := getHistory(t, handler, "base=BTC&quote=USDT&period=1h&mode=candles", token)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// Пара из криптовалюты и фиата строится даже на коротком периоде: сетку даёт
// криптовалюта, а курс валюты переносится с последнего обновления. Ради
// этого случая перенос и сделан, и раньше он здесь ошибочно отключался.
func TestCrossModeWorksWhenOnlyCarriedSideIsSlow(t *testing.T) {
	candles := &stubCandles{}
	handler, token := newHistoryEnv(t, candles)

	rec := getHistory(t, handler, "base=BTC&quote=RUB&period=1h&mode=cross", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if len(candles.crossParams) != 1 {
		t.Fatalf("made %d queries, want the series to be requested", len(candles.crossParams))
	}

	params := candles.crossParams[0]
	if params.GridResolution != 1 {
		t.Errorf("grid resolution = %d, want the crypto side", params.GridResolution)
	}
	if params.CarriedResolution != 1440 {
		t.Errorf("carried resolution = %d, want the fiat side", params.CarriedResolution)
	}
}
