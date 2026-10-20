package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/narumov-diyar/currency-tracker/internal/auth"
	"github.com/narumov-diyar/currency-tracker/internal/storage"
)

type recordedQuery struct {
	assetID    int
	resolution int16
	from, to   time.Time
	limit      int
}

type stubCandles struct {
	queries []recordedQuery
	candles []storage.Candle
}

func (s *stubCandles) Series(_ context.Context, assetID int, resolution int16,
	from, to time.Time, limit int) ([]storage.Candle, error) {

	s.queries = append(s.queries, recordedQuery{
		assetID: assetID, resolution: resolution, from: from, to: to, limit: limit,
	})
	return s.candles, nil
}

func newHistoryEnv(t *testing.T, candles *stubCandles) (http.Handler, string) {
	t.Helper()

	tokens := auth.NewTokenIssuer(strings.Repeat("k", 32), time.Hour)
	handler := NewRouter(Deps{
		DB:      stubPinger{},
		Users:   newStubUsers(),
		Assets:  newStubAssets(),
		Pairs:   newStubPairs(),
		Candles: candles,
		Tokens:  tokens,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	token, _, err := tokens.Issue(1)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	return handler, token
}

func getHistory(t *testing.T, handler http.Handler, query, token string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/api/history?"+query, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestHistoryRequiresToken(t *testing.T) {
	handler, _ := newHistoryEnv(t, &stubCandles{})

	rec := getHistory(t, handler, "base=BTC&quote=USDT&period=1h", "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// Детализация и глубина окна должны соответствовать периоду, иначе ответ
// либо теряет подробности, либо раздувается до десятков тысяч точек.
func TestHistoryPicksResolutionPerPeriod(t *testing.T) {
	tests := []struct {
		period     string
		resolution int16
		limit      int
		window     time.Duration
	}{
		{period: "1h", resolution: 1, limit: 60, window: time.Hour},
		{period: "4h", resolution: 1, limit: 240, window: 4 * time.Hour},
		{period: "1d", resolution: 5, limit: 288, window: 24 * time.Hour},
		{period: "1w", resolution: 60, limit: 168, window: 7 * 24 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.period, func(t *testing.T) {
			candles := &stubCandles{}
			handler, token := newHistoryEnv(t, candles)

			rec := getHistory(t, handler, "base=BTC&quote=USDT&period="+tt.period, token)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body)
			}
			if len(candles.queries) != 2 {
				t.Fatalf("made %d queries, want one per asset", len(candles.queries))
			}

			q := candles.queries[0]
			if q.resolution != tt.resolution {
				t.Errorf("resolution = %d, want %d", q.resolution, tt.resolution)
			}
			if q.limit != tt.limit {
				t.Errorf("limit = %d, want %d", q.limit, tt.limit)
			}
			if window := q.to.Sub(q.from); window != tt.window {
				t.Errorf("window = %v, want %v", window, tt.window)
			}
		})
	}
}

// Фиатная валюта не обновляется чаще раза в сутки, поэтому недельный график
// строится по её собственным суточным свечам, а не по часовым.
func TestHistoryFollowsAssetResolution(t *testing.T) {
	candles := &stubCandles{}
	handler, token := newHistoryEnv(t, candles)

	rec := getHistory(t, handler, "base=BTC&quote=RUB&period=1w", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}

	if candles.queries[0].resolution != 60 {
		t.Errorf("crypto resolution = %d, want 60", candles.queries[0].resolution)
	}
	if candles.queries[1].resolution != 1440 {
		t.Errorf("fiat resolution = %d, want 1440", candles.queries[1].resolution)
	}
	if candles.queries[1].limit != 7 {
		t.Errorf("fiat limit = %d, want 7", candles.queries[1].limit)
	}
}

// Период, который короче интервала обновления актива, данных дать не может.
// Это пустой ряд, а не ошибка: страница должна показать пустое состояние.
func TestHistoryReturnsEmptySeriesForUnsupportedPeriod(t *testing.T) {
	candles := &stubCandles{}
	handler, token := newHistoryEnv(t, candles)

	rec := getHistory(t, handler, "base=RUB&quote=USDT&period=1h", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body historyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Base.Candles) != 0 {
		t.Errorf("fiat series has %d candles, want none for an hourly window", len(body.Base.Candles))
	}
	// К базе за фиатом при этом вообще не ходим.
	for _, q := range candles.queries {
		if q.resolution == 1440 {
			t.Error("an unsupported period must not query the database")
		}
	}
}

func TestHistoryEncodesCandlesAsArrays(t *testing.T) {
	volume := "12.5"
	at := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	candles := &stubCandles{candles: []storage.Candle{{
		CollectedAt: at, Open: "63000", High: "63100", Low: "62900", Close: "63050", Volume: &volume,
	}}}
	handler, token := newHistoryEnv(t, candles)

	rec := getHistory(t, handler, "base=BTC&quote=USDT&period=1h", token)
	var body historyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(body.Base.Candles) != 1 {
		t.Fatalf("got %d candles, want 1", len(body.Base.Candles))
	}

	row := body.Base.Candles[0]
	if len(row) != 6 {
		t.Fatalf("row has %d values, want 6", len(row))
	}
	if ts, ok := row[0].(float64); !ok || int64(ts) != at.UnixMilli() {
		t.Errorf("timestamp = %v, want %d", row[0], at.UnixMilli())
	}
	// Цены передаются строками, иначе крупные значения теряют точность.
	if _, ok := row[1].(string); !ok {
		t.Errorf("open = %T, want a string", row[1])
	}
}

func TestHistoryRejectsBadParameters(t *testing.T) {
	handler, token := newHistoryEnv(t, &stubCandles{})

	tests := []struct {
		name  string
		query string
	}{
		{name: "missing period", query: "base=BTC&quote=USDT"},
		{name: "unknown period", query: "base=BTC&quote=USDT&period=2h"},
		{name: "unknown asset", query: "base=NOPE&quote=USDT&period=1h"},
		{name: "same asset twice", query: "base=BTC&quote=BTC&period=1h"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if rec := getHistory(t, handler, tt.query, token); rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}
		})
	}
}
