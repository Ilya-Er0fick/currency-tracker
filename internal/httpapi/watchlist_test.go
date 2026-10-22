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

type stubAssets struct {
	bySymbol map[string]storage.Asset
}

func newStubAssets() *stubAssets {
	return &stubAssets{bySymbol: map[string]storage.Asset{
		"BTC":  {ID: 1, Symbol: "BTC", Type: "crypto", Source: "bybit", ResolutionSeconds: 60, SupportsRealtime: true},
		"USDT": {ID: 2, Symbol: "USDT", Type: "crypto", Source: "bybit", ResolutionSeconds: 60, SupportsRealtime: true},
		"RUB":  {ID: 3, Symbol: "RUB", Type: "fiat", Source: "er-api", ResolutionSeconds: 86400},
		"KZT":  {ID: 4, Symbol: "KZT", Type: "fiat", Source: "er-api", ResolutionSeconds: 86400},
	}}
}

func (s *stubAssets) List(context.Context) ([]storage.Asset, error) {
	assets := make([]storage.Asset, 0, len(s.bySymbol))
	for _, a := range s.bySymbol {
		assets = append(assets, a)
	}
	return assets, nil
}

func (s *stubAssets) BySymbol(_ context.Context, symbol string) (storage.Asset, error) {
	asset, ok := s.bySymbol[symbol]
	if !ok {
		return storage.Asset{}, storage.ErrAssetNotFound
	}
	return asset, nil
}

type stubPairs struct {
	rows   map[int64][]storage.Pair
	owners map[int64]int64
	nextID int64
	seen   map[string]bool
}

func newStubPairs() *stubPairs {
	return &stubPairs{
		rows:   map[int64][]storage.Pair{},
		owners: map[int64]int64{},
		nextID: 1,
		seen:   map[string]bool{},
	}
}

func (s *stubPairs) List(_ context.Context, userID int64) ([]storage.Pair, error) {
	return s.rows[userID], nil
}

func (s *stubPairs) Create(_ context.Context, userID int64, base, quote int) (int64, error) {
	key := string(rune(userID)) + ":" + string(rune(base)) + ":" + string(rune(quote))
	if s.seen[key] {
		return 0, storage.ErrPairExists
	}
	s.seen[key] = true

	id := s.nextID
	s.nextID++
	s.owners[id] = userID
	s.rows[userID] = append(s.rows[userID], storage.Pair{ID: id})
	return id, nil
}

func (s *stubPairs) Owned(_ context.Context, userID, pairID int64) (bool, error) {
	owner, ok := s.owners[pairID]
	return ok && owner == userID, nil
}

func (s *stubPairs) Delete(_ context.Context, userID, pairID int64) error {
	if owner, ok := s.owners[pairID]; !ok || owner != userID {
		return storage.ErrPairNotFound
	}
	delete(s.owners, pairID)
	return nil
}

type watchlistEnv struct {
	handler http.Handler
	token   string
	pairs   *stubPairs
}

func newWatchlistEnv(t *testing.T) watchlistEnv {
	t.Helper()

	tokens := auth.NewTokenIssuer(strings.Repeat("k", 32), time.Hour)
	pairs := newStubPairs()
	handler := NewRouter(Deps{
		DB:     stubPinger{},
		Users:  newStubUsers(),
		Assets: newStubAssets(),
		Pairs:  pairs,
		Tokens: tokens,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	token, _, err := tokens.Issue(1)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	return watchlistEnv{handler: handler, token: token, pairs: pairs}
}

func (e watchlistEnv) do(t *testing.T, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

func TestAssetCatalogIsPublic(t *testing.T) {
	env := newWatchlistEnv(t)

	rec := env.do(t, http.MethodGet, "/api/assets", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var assets []storage.Asset
	if err := json.Unmarshal(rec.Body.Bytes(), &assets); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(assets) != 4 {
		t.Errorf("got %d assets, want 4", len(assets))
	}
}

func TestWatchlistRequiresToken(t *testing.T) {
	env := newWatchlistEnv(t)

	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "list", method: http.MethodGet, path: "/api/watchlist"},
		{name: "add", method: http.MethodPost, path: "/api/watchlist", body: `{"base":"BTC","quote":"USDT"}`},
		{name: "remove", method: http.MethodDelete, path: "/api/watchlist/1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if rec := env.do(t, tt.method, tt.path, tt.body, ""); rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
			}
		})
	}
}

func TestAddPair(t *testing.T) {
	env := newWatchlistEnv(t)

	rec := env.do(t, http.MethodPost, "/api/watchlist", `{"base":"btc","quote":" usdt "}`, env.token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body)
	}

	// Символы приводятся к верхнему регистру, иначе "btc" и "BTC" считались бы
	// разными активами и одну и ту же пару можно было бы добавить дважды.
	var created struct {
		Base  storage.PairSide `json:"base"`
		Quote storage.PairSide `json:"quote"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Base.Symbol != "BTC" || created.Quote.Symbol != "USDT" {
		t.Errorf("symbols = %s/%s, want BTC/USDT", created.Base.Symbol, created.Quote.Symbol)
	}
}

func TestAddPairRejectsBadInput(t *testing.T) {
	env := newWatchlistEnv(t)

	tests := []struct {
		name string
		body string
		want int
	}{
		{name: "malformed json", body: `{`, want: http.StatusBadRequest},
		{name: "same asset twice", body: `{"base":"BTC","quote":"BTC"}`, want: http.StatusBadRequest},
		{name: "unknown base", body: `{"base":"NOPE","quote":"USDT"}`, want: http.StatusBadRequest},
		{name: "unknown quote", body: `{"base":"BTC","quote":"NOPE"}`, want: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if rec := env.do(t, http.MethodPost, "/api/watchlist", tt.body, env.token); rec.Code != tt.want {
				t.Errorf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

func TestAddPairRejectsDuplicate(t *testing.T) {
	env := newWatchlistEnv(t)
	body := `{"base":"BTC","quote":"USDT"}`

	if rec := env.do(t, http.MethodPost, "/api/watchlist", body, env.token); rec.Code != http.StatusCreated {
		t.Fatalf("first add failed: %d", rec.Code)
	}
	if rec := env.do(t, http.MethodPost, "/api/watchlist", body, env.token); rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestRemovePair(t *testing.T) {
	env := newWatchlistEnv(t)
	env.do(t, http.MethodPost, "/api/watchlist", `{"base":"BTC","quote":"USDT"}`, env.token)

	if rec := env.do(t, http.MethodDelete, "/api/watchlist/1", "", env.token); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if rec := env.do(t, http.MethodDelete, "/api/watchlist/1", "", env.token); rec.Code != http.StatusNotFound {
		t.Errorf("repeated delete status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// Чужая пара должна выглядеть как несуществующая, иначе по коду ответа
// можно было бы перебрать занятые идентификаторы.
func TestRemovePairOfAnotherUser(t *testing.T) {
	env := newWatchlistEnv(t)
	env.do(t, http.MethodPost, "/api/watchlist", `{"base":"BTC","quote":"USDT"}`, env.token)

	other := auth.NewTokenIssuer(strings.Repeat("k", 32), time.Hour)
	otherToken, _, err := other.Issue(2)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	if rec := env.do(t, http.MethodDelete, "/api/watchlist/1", "", otherToken); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestRemovePairRejectsNonNumericID(t *testing.T) {
	env := newWatchlistEnv(t)

	if rec := env.do(t, http.MethodDelete, "/api/watchlist/abc", "", env.token); rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
