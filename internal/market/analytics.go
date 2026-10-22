package market

import (
	"math"
	"time"
)

// Показателей не бывает на коротком ряде: по двум точкам разброс
// доходностей ничего не значит, а корреляция формально равна единице.
const minReturns = 3

type Point struct {
	Time  time.Time
	Close float64
}

// Analytics — показатели по паре за выбранный период.
// Пустое поле означает, что данных не хватило, и это не то же самое, что ноль.
type Analytics struct {
	BaseChange  *float64 `json:"base_change"`
	QuoteChange *float64 `json:"quote_change"`
	ChangeDiff  *float64 `json:"change_diff"`
	BaseVol     *float64 `json:"base_volatility"`
	QuoteVol    *float64 `json:"quote_volatility"`
	Correlation *float64 `json:"correlation"`
}

func Analyse(base, quote []Point) Analytics {
	var a Analytics

	a.BaseChange = percentChange(base)
	a.QuoteChange = percentChange(quote)
	if a.BaseChange != nil && a.QuoteChange != nil {
		diff := *a.BaseChange - *a.QuoteChange
		a.ChangeDiff = &diff
	}

	a.BaseVol = volatility(base)
	a.QuoteVol = volatility(quote)

	baseReturns, quoteReturns := alignedReturns(base, quote)
	a.Correlation = pearson(baseReturns, quoteReturns)

	return a
}

// percentChange — изменение от первой точки периода к последней, в процентах.
func percentChange(points []Point) *float64 {
	if len(points) < 2 {
		return nil
	}

	first, last := points[0].Close, points[len(points)-1].Close
	if first == 0 {
		return nil
	}

	change := (last/first - 1) * 100
	return &change
}

// volatility — стандартное отклонение доходностей, в процентах.
// Считается по доходностям, а не по ценам: разброс самих цен зависит
// от масштаба актива, и сравнивать биткоин с рублём по нему бессмысленно.
func volatility(points []Point) *float64 {
	r := returns(points)
	if len(r) < minReturns {
		return nil
	}

	deviation := stddev(r)
	if deviation == nil {
		return nil
	}

	percent := *deviation * 100
	return &percent
}

func returns(points []Point) []float64 {
	if len(points) < 2 {
		return nil
	}

	r := make([]float64, 0, len(points)-1)
	for i := 1; i < len(points); i++ {
		previous := points[i-1].Close
		if previous == 0 {
			continue
		}
		r = append(r, points[i].Close/previous-1)
	}
	return r
}

func stddev(values []float64) *float64 {
	if len(values) < 2 {
		return nil
	}

	var sum float64
	for _, v := range values {
		sum += v
	}
	mean := sum / float64(len(values))

	var squares float64
	for _, v := range values {
		squares += (v - mean) * (v - mean)
	}

	// Выборочная дисперсия: ряд — это наблюдения за период, а не вся совокупность.
	result := math.Sqrt(squares / float64(len(values)-1))
	return &result
}

// alignedReturns сопоставляет ряды по времени и считает доходности только
// по совпавшим моментам. Без этого пара из криптовалюты и фиата дала бы
// бессмысленную пару рядов разной длины.
func alignedReturns(base, quote []Point) ([]float64, []float64) {
	byTime := make(map[int64]float64, len(quote))
	for _, p := range quote {
		byTime[p.Time.UnixMilli()] = p.Close
	}

	var pairedBase, pairedQuote []Point
	for _, p := range base {
		if close, ok := byTime[p.Time.UnixMilli()]; ok {
			pairedBase = append(pairedBase, p)
			pairedQuote = append(pairedQuote, Point{Time: p.Time, Close: close})
		}
	}

	return returns(pairedBase), returns(pairedQuote)
}

// pearson — коэффициент корреляции доходностей.
//
// Корреляция считается именно по доходностям: у двух растущих рядов
// корреляция цен окажется близкой к единице просто потому, что оба растут,
// и ничего о совместном движении не скажет.
func pearson(xs, ys []float64) *float64 {
	if len(xs) != len(ys) || len(xs) < minReturns {
		return nil
	}

	var sumX, sumY float64
	for i := range xs {
		sumX += xs[i]
		sumY += ys[i]
	}
	meanX, meanY := sumX/float64(len(xs)), sumY/float64(len(ys))

	var covariance, varianceX, varianceY float64
	for i := range xs {
		dx, dy := xs[i]-meanX, ys[i]-meanY
		covariance += dx * dy
		varianceX += dx * dx
		varianceY += dy * dy
	}

	// Если одна из сторон не менялась, разброса нет и корреляция не определена.
	// Так бывает у фиатной валюты внутри суток: её курс переносится без изменений.
	if varianceX == 0 || varianceY == 0 {
		return nil
	}

	result := covariance / math.Sqrt(varianceX*varianceY)
	return &result
}
