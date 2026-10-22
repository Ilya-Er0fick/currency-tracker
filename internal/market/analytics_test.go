package market

import (
	"math"
	"testing"
	"time"
)

func series(closes ...float64) []Point {
	start := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	points := make([]Point, len(closes))
	for i, c := range closes {
		points[i] = Point{Time: start.Add(time.Duration(i) * time.Minute), Close: c}
	}
	return points
}

func requireValue(t *testing.T, got *float64, want, tolerance float64, name string) {
	t.Helper()

	if got == nil {
		t.Fatalf("%s is empty, want %v", name, want)
	}
	if math.Abs(*got-want) > tolerance {
		t.Errorf("%s = %v, want %v", name, *got, want)
	}
}

func TestPercentChange(t *testing.T) {
	a := Analyse(series(100, 110, 120), series(200, 200, 180))

	requireValue(t, a.BaseChange, 20, 1e-9, "изменение базового актива")
	requireValue(t, a.QuoteChange, -10, 1e-9, "изменение котируемого актива")
	// Разность показывает, насколько один актив обогнал другой.
	requireValue(t, a.ChangeDiff, 30, 1e-9, "разность изменений")
}

// Корреляция считается по доходностям, а не по ценам. Два ряда ниже растут
// оба, но движутся по-разному: корреляция цен была бы почти единицей
// и ничего бы не показала.
func TestCorrelationUsesReturnsNotPrices(t *testing.T) {
	base := series(100, 110, 120, 130, 140)
	quote := series(10, 11, 12, 13, 14)

	// Строго пропорциональные ряды дают полную корреляцию доходностей.
	a := Analyse(base, quote)
	requireValue(t, a.Correlation, 1, 1e-9, "корреляция пропорциональных рядов")

	// Противоположное движение даёт минус единицу.
	opposite := Analyse(series(100, 110, 100, 110, 100), series(100, 90, 100, 90, 100))
	requireValue(t, opposite.Correlation, -1, 1e-9, "корреляция встречных рядов")
}

func TestVolatility(t *testing.T) {
	// Ровно растущий ряд: доходности одинаковы, разброса нет.
	steady := Analyse(series(100, 110, 121, 133.1, 146.41), series(1, 1, 1, 1, 1))
	requireValue(t, steady.BaseVol, 0, 1e-9, "волатильность ровного роста")

	// Скачки в разные стороны дают заметный разброс.
	jumpy := Analyse(series(100, 150, 100, 150, 100), series(1, 1, 1, 1, 1))
	if jumpy.BaseVol == nil || *jumpy.BaseVol < 10 {
		t.Errorf("волатильность скачущего ряда = %v, ожидалась заметная", jumpy.BaseVol)
	}
}

func TestInsufficientData(t *testing.T) {
	tests := []struct {
		name  string
		base  []Point
		quote []Point
	}{
		{name: "пустые ряды"},
		{name: "одна точка", base: series(100), quote: series(100)},
		{name: "две точки", base: series(100, 110), quote: series(100, 120)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := Analyse(tt.base, tt.quote)

			if a.Correlation != nil {
				t.Errorf("корреляция = %v, ожидалась пустой", *a.Correlation)
			}
			if a.BaseVol != nil {
				t.Errorf("волатильность = %v, ожидалась пустой", *a.BaseVol)
			}
		})
	}
}

// У фиатной валюты внутри суток курс один и тот же: разброса нет,
// и корреляция не определена. Это пустое значение, а не ноль.
func TestCorrelationUndefinedForConstantSeries(t *testing.T) {
	a := Analyse(series(100, 110, 120, 130), series(50, 50, 50, 50))

	if a.Correlation != nil {
		t.Errorf("корреляция = %v, ожидалась пустой для постоянного ряда", *a.Correlation)
	}
	requireValue(t, a.QuoteVol, 0, 1e-9, "волатильность постоянного ряда")
}

// Ряды разной частоты сопоставляются по времени: считать корреляцию
// по позициям в массивах нельзя, они относятся к разным моментам.
func TestCorrelationAlignsSeriesByTime(t *testing.T) {
	start := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)

	base := []Point{
		{Time: start, Close: 100},
		{Time: start.Add(time.Minute), Close: 110},
		{Time: start.Add(2 * time.Minute), Close: 120},
		{Time: start.Add(3 * time.Minute), Close: 130},
		{Time: start.Add(4 * time.Minute), Close: 140},
	}
	// Котируемый ряд реже и смещён: совпадают только три момента.
	quote := []Point{
		{Time: start, Close: 10},
		{Time: start.Add(2 * time.Minute), Close: 12},
		{Time: start.Add(4 * time.Minute), Close: 14},
		{Time: start.Add(6 * time.Minute), Close: 16},
	}

	a := Analyse(base, quote)

	// Трёх совпавших моментов хватает на две доходности, а нужно три.
	if a.Correlation != nil {
		t.Errorf("корреляция = %v, ожидалась пустой при двух совпавших доходностях", *a.Correlation)
	}

	// Добавим совпадений — корреляция появится.
	quote = append(quote, Point{Time: start.Add(time.Minute), Close: 11},
		Point{Time: start.Add(3 * time.Minute), Close: 13})
	if full := Analyse(base, quote); full.Correlation == nil {
		t.Error("корреляция должна считаться, когда совпавших моментов достаточно")
	}
}

func TestZeroPriceDoesNotBreakCalculations(t *testing.T) {
	a := Analyse(series(0, 100, 110, 120), series(10, 11, 12, 13))

	// Первая цена нулевая: изменение от неё не определено.
	if a.BaseChange != nil {
		t.Errorf("изменение = %v, ожидалось пустым при нулевой начальной цене", *a.BaseChange)
	}
	// При этом расчёт не падает и остальные показатели считаются.
	if a.QuoteChange == nil {
		t.Error("изменение котируемого актива должно считаться")
	}
}
