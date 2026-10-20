package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/narumov-diyar/currency-tracker/internal/market"
	"github.com/narumov-diyar/currency-tracker/internal/storage"
)

// Свечи передаются массивами без имён полей: при нескольких сотнях точек
// повторяющиеся ключи занимают больше места, чем сами значения.
// Порядок: время в миллисекундах, открытие, максимум, минимум, закрытие, объём.
type seriesResponse struct {
	Symbol string `json:"symbol"`
	// Разрешение указывается у каждой стороны отдельно: у пары из криптовалюты
	// и фиата они разные, потому что фиат не обновляется чаще раза в сутки.
	Resolution int16   `json:"resolution"`
	Candles    [][]any `json:"candles"`
}

type historyResponse struct {
	Period string         `json:"period"`
	From   time.Time      `json:"from"`
	To     time.Time      `json:"to"`
	Base   seriesResponse `json:"base"`
	Quote  seriesResponse `json:"quote"`
}

func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	period, err := market.ParsePeriod(query.Get("period"))
	if errors.Is(err, market.ErrUnknownPeriod) {
		writeError(w, http.StatusBadRequest, "period must be one of 1h, 4h, 1d, 1w")
		return
	}

	base, ok := s.resolveAsset(w, r, upper(query.Get("base")))
	if !ok {
		return
	}
	quote, ok := s.resolveAsset(w, r, upper(query.Get("quote")))
	if !ok {
		return
	}
	if base.ID == quote.ID {
		writeError(w, http.StatusBadRequest, "a pair needs two different assets")
		return
	}

	to := time.Now().UTC()
	from := to.Add(-period.Window)

	baseSeries, err := s.loadSeries(r, base, period, from, to)
	if err != nil {
		s.fail(w, "load base series", err)
		return
	}
	quoteSeries, err := s.loadSeries(r, quote, period, from, to)
	if err != nil {
		s.fail(w, "load quote series", err)
		return
	}

	writeJSON(w, http.StatusOK, historyResponse{
		Period: period.Name,
		From:   from,
		To:     to,
		Base:   baseSeries,
		Quote:  quoteSeries,
	})
}

// loadSeries отдаёт пустой ряд, если период мельче, чем обновляется актив:
// курс, меняющийся раз в сутки, на часовом графике данных не имеет.
func (s *Server) loadSeries(r *http.Request, asset storage.Asset, period market.Period,
	from, to time.Time) (seriesResponse, error) {

	resolution := period.ResolutionFor(asset.ResolutionSeconds)
	series := seriesResponse{Symbol: asset.Symbol, Resolution: resolution, Candles: [][]any{}}

	if !period.Supports(asset.ResolutionSeconds) {
		return series, nil
	}

	candles, err := s.candles.Series(r.Context(), asset.ID, resolution, from, to,
		period.PointsFor(asset.ResolutionSeconds))
	if err != nil {
		return seriesResponse{}, err
	}

	for _, c := range candles {
		series.Candles = append(series.Candles, []any{
			c.CollectedAt.UnixMilli(), c.Open, c.High, c.Low, c.Close, c.Volume,
		})
	}
	return series, nil
}
