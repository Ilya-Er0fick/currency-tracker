package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/narumov-diyar/currency-tracker/internal/storage"
)

func (s *Server) listAssets(w http.ResponseWriter, r *http.Request) {
	assets, err := s.assets.List(r.Context())
	if err != nil {
		s.fail(w, "list assets", err)
		return
	}
	writeJSON(w, http.StatusOK, assets)
}

func (s *Server) listWatchlist(w http.ResponseWriter, r *http.Request) {
	userID, _ := UserID(r.Context())

	pairs, err := s.pairs.List(r.Context(), userID)
	if err != nil {
		s.fail(w, "list watchlist", err)
		return
	}
	writeJSON(w, http.StatusOK, pairs)
}

type pairRequest struct {
	Base  string `json:"base"`
	Quote string `json:"quote"`
}

func (s *Server) addPair(w http.ResponseWriter, r *http.Request) {
	userID, _ := UserID(r.Context())

	var req pairRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "malformed request body")
		return
	}

	req.Base = strings.ToUpper(strings.TrimSpace(req.Base))
	req.Quote = strings.ToUpper(strings.TrimSpace(req.Quote))

	if req.Base == req.Quote {
		writeError(w, http.StatusBadRequest, "a pair needs two different assets")
		return
	}

	base, ok := s.resolveAsset(w, r, req.Base)
	if !ok {
		return
	}
	quote, ok := s.resolveAsset(w, r, req.Quote)
	if !ok {
		return
	}

	id, err := s.pairs.Create(r.Context(), userID, base.ID, quote.ID)
	if errors.Is(err, storage.ErrPairExists) {
		writeError(w, http.StatusConflict, "pair is already in the watchlist")
		return
	}
	if err != nil {
		s.fail(w, "create pair", err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":    id,
		"base":  storage.PairSide{Symbol: base.Symbol, Type: base.Type},
		"quote": storage.PairSide{Symbol: quote.Symbol, Type: quote.Type},
	})
}

func (s *Server) resolveAsset(w http.ResponseWriter, r *http.Request, symbol string) (storage.Asset, bool) {
	asset, err := s.assets.BySymbol(r.Context(), symbol)
	if errors.Is(err, storage.ErrAssetNotFound) {
		writeError(w, http.StatusBadRequest, "unknown asset: "+symbol)
		return storage.Asset{}, false
	}
	if err != nil {
		s.fail(w, "find asset", err)
		return storage.Asset{}, false
	}
	return asset, true
}

func (s *Server) removePair(w http.ResponseWriter, r *http.Request) {
	userID, _ := UserID(r.Context())

	pairID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "pair id must be a number")
		return
	}

	err = s.pairs.Delete(r.Context(), userID, pairID)
	// Чужая пара и несуществующая дают один и тот же ответ: иначе по коду
	// можно было бы узнать, какие идентификаторы заняты другими пользователями.
	if errors.Is(err, storage.ErrPairNotFound) {
		writeError(w, http.StatusNotFound, "pair not found")
		return
	}
	if err != nil {
		s.fail(w, "delete pair", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
