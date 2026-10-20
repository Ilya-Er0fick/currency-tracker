// Пакет market описывает периоды графика и подбор детализации под них.
package market

import (
	"errors"
	"time"
)

var ErrUnknownPeriod = errors.New("unknown period")

// Период графика задаёт не только глубину окна, но и разрешение ряда.
//
// Детализация выбирается так, чтобы ответ укладывался в несколько сотен точек:
// график шириной около 1200 пикселей больше попросту не покажет, а передача
// десятков тысяч свечей за неделю увеличивала бы ответ на два порядка
// без всякой пользы.
type Period struct {
	Name       string
	Window     time.Duration
	Resolution int16 // в минутах, как в таблице свечей
}

var periods = map[string]Period{
	"1h": {Name: "1h", Window: time.Hour, Resolution: 1},
	"4h": {Name: "4h", Window: 4 * time.Hour, Resolution: 1},
	"1d": {Name: "1d", Window: 24 * time.Hour, Resolution: 5},
	"1w": {Name: "1w", Window: 7 * 24 * time.Hour, Resolution: 60},
}

func ParsePeriod(name string) (Period, error) {
	period, ok := periods[name]
	if !ok {
		return Period{}, ErrUnknownPeriod
	}
	return period, nil
}

// ResolutionFor подбирает разрешение ряда под конкретный актив.
//
// Разрешение периода — это желаемая детализация, но актив не может дать
// больше, чем обновляется сам: у фиатной валюты нет часовых свечей,
// поэтому для неё берётся её собственное суточное разрешение.
func (p Period) ResolutionFor(assetResolutionSeconds int) int16 {
	native := int16(assetResolutionSeconds / 60)
	if native > p.Resolution {
		return native
	}
	return p.Resolution
}

// PointsFor — сколько свечей актива укладывается в окно периода.
func (p Period) PointsFor(assetResolutionSeconds int) int {
	step := time.Duration(p.ResolutionFor(assetResolutionSeconds)) * time.Minute
	return int(p.Window / step)
}

// Supports сообщает, осмысленен ли период для актива с таким разрешением.
// Сравнивается с окном, а не с разрешением периода: суточный курс на
// недельном графике даст семь точек и вполне пригоден, а на часовом —
// ни одной.
func (p Period) Supports(assetResolutionSeconds int) bool {
	return time.Duration(assetResolutionSeconds)*time.Second <= p.Window
}
