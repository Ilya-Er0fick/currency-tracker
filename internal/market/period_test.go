package market

import (
	"testing"
	"time"
)

func TestParsePeriod(t *testing.T) {
	tests := []struct {
		name       string
		window     time.Duration
		resolution int16
		points     int
	}{
		{name: "1h", window: time.Hour, resolution: 1, points: 60},
		{name: "4h", window: 4 * time.Hour, resolution: 1, points: 240},
		{name: "1d", window: 24 * time.Hour, resolution: 5, points: 288},
		{name: "1w", window: 7 * 24 * time.Hour, resolution: 60, points: 168},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			period, err := ParsePeriod(tt.name)
			if err != nil {
				t.Fatalf("ParsePeriod: %v", err)
			}
			if period.Window != tt.window {
				t.Errorf("window = %v, want %v", period.Window, tt.window)
			}
			if period.Resolution != tt.resolution {
				t.Errorf("resolution = %d, want %d", period.Resolution, tt.resolution)
			}
			if got := period.PointsFor(60); got != tt.points {
				t.Errorf("points = %d, want %d", got, tt.points)
			}
		})
	}
}

// Ответ должен оставаться в пределах нескольких сотен точек: ради этого
// детализация и привязана к периоду.
func TestEveryPeriodStaysWithinPointBudget(t *testing.T) {
	for name := range periods {
		period, err := ParsePeriod(name)
		if err != nil {
			t.Fatalf("ParsePeriod(%q): %v", name, err)
		}
		if points := period.PointsFor(60); points < 50 || points > 600 {
			t.Errorf("%s gives %d points, want between 50 and 600", name, points)
		}
	}
}

func TestParsePeriodRejectsUnknown(t *testing.T) {
	for _, name := range []string{"", "2h", "1m", "1y", "1H"} {
		if _, err := ParsePeriod(name); err == nil {
			t.Errorf("period %q was accepted but must be rejected", name)
		}
	}
}

func TestSupports(t *testing.T) {
	const minute, day = 60, 86400

	tests := []struct {
		period string
		asset  int
		want   bool
	}{
		{period: "1h", asset: minute, want: true},
		{period: "4h", asset: minute, want: true},
		{period: "1d", asset: minute, want: true},
		{period: "1w", asset: minute, want: true},
		// Суточный курс не помещается в часовое окно, но на недельном графике
		// даёт семь точек.
		{period: "1h", asset: day, want: false},
		{period: "4h", asset: day, want: false},
		{period: "1d", asset: day, want: true},
		{period: "1w", asset: day, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.period+"/"+itoa(tt.asset), func(t *testing.T) {
			period, err := ParsePeriod(tt.period)
			if err != nil {
				t.Fatalf("ParsePeriod: %v", err)
			}
			if got := period.Supports(tt.asset); got != tt.want {
				t.Errorf("Supports(%d) = %v, want %v", tt.asset, got, tt.want)
			}
		})
	}
}

func itoa(v int) string {
	if v == 60 {
		return "минутный"
	}
	return "суточный"
}

// Актив не может дать больше свечей, чем обновляется сам: для фиатной
// валюты недельный график строится по суточным свечам, а не по часовым.
func TestResolutionFollowsAsset(t *testing.T) {
	const minute, day = 60, 86400

	tests := []struct {
		period     string
		asset      int
		resolution int16
		points     int
	}{
		{period: "1w", asset: minute, resolution: 60, points: 168},
		{period: "1w", asset: day, resolution: 1440, points: 7},
		{period: "1d", asset: minute, resolution: 5, points: 288},
		{period: "1d", asset: day, resolution: 1440, points: 1},
	}

	for _, tt := range tests {
		t.Run(tt.period, func(t *testing.T) {
			period, err := ParsePeriod(tt.period)
			if err != nil {
				t.Fatalf("ParsePeriod: %v", err)
			}
			if got := period.ResolutionFor(tt.asset); got != tt.resolution {
				t.Errorf("resolution = %d, want %d", got, tt.resolution)
			}
			if got := period.PointsFor(tt.asset); got != tt.points {
				t.Errorf("points = %d, want %d", got, tt.points)
			}
		})
	}
}
