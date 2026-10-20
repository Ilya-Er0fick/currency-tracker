package httpapi

import (
	"errors"
	"net/http"
	"strconv"
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

// Кросс-курс отдаётся парами «время, курс»: это одна кривая, свечей у неё нет.
// Максимум и минимум отношения не выводятся из максимумов сторон, поэтому
// строить из них свечу было бы подлогом.
type crossResponse struct {
	Pair       string  `json:"pair"`
	Resolution int16   `json:"resolution"`
	Points     [][]any `json:"points"`
}

type historyResponse struct {
	Period    string           `json:"period"`
	Mode      string           `json:"mode"`
	From      time.Time        `json:"from"`
	To        time.Time        `json:"to"`
	Base      *seriesResponse  `json:"base,omitempty"`
	Quote     *seriesResponse  `json:"quote,omitempty"`
	Cross     *crossResponse   `json:"cross,omitempty"`
	Analytics market.Analytics `json:"analytics"`
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

	mode := query.Get("mode")
	if mode == "" {
		mode = modeCompare
	}
	if mode != modeCompare && mode != modeCross {
		writeError(w, http.StatusBadRequest, "mode must be compare or cross")
		return
	}

	to := time.Now().UTC()
	from := to.Add(-period.Window)

	// Ряды обеих сторон нужны в любом режиме: показатели считаются по ним,
	// а не по готовой кривой кросс-курса.
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

	response := historyResponse{
		Period:    period.Name,
		Mode:      mode,
		From:      from,
		To:        to,
		Analytics: market.Analyse(analyticsPoints(baseSeries), analyticsPoints(quoteSeries)),
	}

	if mode == modeCross {
		cross, err := s.loadCross(r, base, quote, period, from, to)
		if err != nil {
			s.fail(w, "load cross series", err)
			return
		}
		response.Cross = cross
		writeJSON(w, http.StatusOK, response)
		return
	}

	response.Base = &baseSeries
	response.Quote = &quoteSeries
	writeJSON(w, http.StatusOK, response)
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

const (
	modeCompare = "compare"
	modeCross   = "cross"
)

// loadCross строит одну кривую отношения.
//
// Сетка времени берётся у того актива, который обновляется чаще: пара
// из криптовалюты и фиата должна меняться поминутно, а не раз в сутки.
// Второй ряд подтягивается переносом последнего известного значения.
func (s *Server) loadCross(r *http.Request, base, quote storage.Asset, period market.Period,
	from, to time.Time) (*crossResponse, error) {

	baseRes := period.ResolutionFor(base.ResolutionSeconds)
	quoteRes := period.ResolutionFor(quote.ResolutionSeconds)

	params := storage.CrossSeriesParams{
		GridAssetID: base.ID, GridResolution: baseRes,
		CarriedAssetID: quote.ID, CarriedResolution: quoteRes,
		GridIsBase: true,
		From:       from, To: to,
	}
	gridResolution := baseRes

	if quoteRes < baseRes {
		params = storage.CrossSeriesParams{
			GridAssetID: quote.ID, GridResolution: quoteRes,
			CarriedAssetID: base.ID, CarriedResolution: baseRes,
			GridIsBase: false,
			From:       from, To: to,
		}
		gridResolution = quoteRes
	}
	params.Limit = int(period.Window / (time.Duration(gridResolution) * time.Minute))

	cross := &crossResponse{
		Pair:       base.Symbol + "/" + quote.Symbol,
		Resolution: gridResolution,
		Points:     [][]any{},
	}

	// Требование предъявляется только к сетке: она задаёт точки кривой.
	// Переносимой стороне обновляться в пределах окна не нужно — берётся
	// её последнее известное значение, пусть и многодневной давности.
	// Иначе пара из криптовалюты и фиата осталась бы пустой именно в том
	// случае, ради которого перенос и сделан.
	gridResolutionSeconds := base.ResolutionSeconds
	if quoteRes < baseRes {
		gridResolutionSeconds = quote.ResolutionSeconds
	}
	if !period.Supports(gridResolutionSeconds) {
		return cross, nil
	}

	points, err := s.candles.CrossSeries(r.Context(), params)
	if err != nil {
		return nil, err
	}
	for _, p := range points {
		cross.Points = append(cross.Points, []any{p.CollectedAt.UnixMilli(), p.Rate})
	}
	return cross, nil
}

// analyticsPoints переводит ряд в числа для расчёта показателей.
//
// Цены приводятся к числу с плавающей точкой: корреляция и разброс
// доходностей — величины статистические, и точности double для них с запасом
// хватает. В самих рядах цены по-прежнему остаются строками, потому что там
// они показываются пользователю как есть.
func analyticsPoints(series seriesResponse) []market.Point {
	points := make([]market.Point, 0, len(series.Candles))
	for _, row := range series.Candles {
		ts, okTime := row[0].(int64)
		raw, okClose := row[4].(string)
		if !okTime || !okClose {
			continue
		}
		close, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			continue
		}
		points = append(points, market.Point{Time: time.UnixMilli(ts).UTC(), Close: close})
	}
	return points
}
