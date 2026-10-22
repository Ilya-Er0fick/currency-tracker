package alerts

import "testing"

func TestCrossedThresholdUpward(t *testing.T) {
	const target = 70000

	tests := []struct {
		name              string
		previous, current float64
		want              bool
	}{
		{name: "подошёл вплотную, но не дошёл", previous: 68000, current: 69999, want: false},
		{name: "дошёл ровно до уровня", previous: 69000, current: 70000, want: true},
		{name: "пересёк", previous: 69000, current: 71000, want: true},
		// Между снимками курс способен перепрыгнуть уровень целиком —
		// именно поэтому сравнивается переход, а не попадание в уровень.
		{name: "перепрыгнул через разрыв", previous: 60000, current: 85000, want: true},
		{name: "уже был выше: повторно не срабатывает", previous: 71000, current: 72000, want: false},
		{name: "движется вниз", previous: 72000, current: 69000, want: false},
		{name: "стоит на месте выше уровня", previous: 71000, current: 71000, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CrossedThreshold(DirectionAbove, tt.previous, tt.current, target); got != tt.want {
				t.Errorf("CrossedThreshold(above, %v, %v, %v) = %v, want %v",
					tt.previous, tt.current, target, got, tt.want)
			}
		})
	}
}

func TestCrossedThresholdDownward(t *testing.T) {
	const target = 60000

	tests := []struct {
		name              string
		previous, current float64
		want              bool
	}{
		{name: "подошёл вплотную", previous: 62000, current: 60001, want: false},
		{name: "дошёл ровно до уровня", previous: 61000, current: 60000, want: true},
		{name: "пересёк", previous: 61000, current: 59000, want: true},
		{name: "провалился через разрыв", previous: 70000, current: 50000, want: true},
		{name: "уже был ниже", previous: 59000, current: 58000, want: false},
		{name: "движется вверх", previous: 58000, current: 61000, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CrossedThreshold(DirectionBelow, tt.previous, tt.current, target); got != tt.want {
				t.Errorf("CrossedThreshold(below, %v, %v, %v) = %v, want %v",
					tt.previous, tt.current, target, got, tt.want)
			}
		})
	}
}

func TestCrossedThresholdUnknownDirection(t *testing.T) {
	if CrossedThreshold("sideways", 60000, 80000, 70000) {
		t.Error("неизвестное направление не должно приводить к срабатыванию")
	}
}

func TestExceededPercent(t *testing.T) {
	tests := []struct {
		name      string
		direction string
		reference float64
		current   float64
		target    float64
		want      bool
	}{
		{name: "рост ровно на порог", direction: DirectionAbove, reference: 100, current: 105, target: 5, want: true},
		{name: "рост больше порога", direction: DirectionAbove, reference: 100, current: 110, target: 5, want: true},
		{name: "рост меньше порога", direction: DirectionAbove, reference: 100, current: 104, target: 5, want: false},
		// Падение не должно удовлетворять правилу о росте.
		{name: "падение при правиле на рост", direction: DirectionAbove, reference: 100, current: 90, target: 5, want: false},

		{name: "падение ровно на порог", direction: DirectionBelow, reference: 100, current: 95, target: 5, want: true},
		{name: "падение больше порога", direction: DirectionBelow, reference: 100, current: 80, target: 5, want: true},
		{name: "падение меньше порога", direction: DirectionBelow, reference: 100, current: 97, target: 5, want: false},
		{name: "рост при правиле на падение", direction: DirectionBelow, reference: 100, current: 120, target: 5, want: false},

		// Нулевая точка отсчёта делает изменение неопределённым.
		{name: "нулевая точка отсчёта", direction: DirectionAbove, reference: 0, current: 100, target: 5, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExceededPercent(tt.direction, tt.reference, tt.current, tt.target)
			if got != tt.want {
				t.Errorf("ExceededPercent(%s, %v, %v, %v) = %v, want %v",
					tt.direction, tt.reference, tt.current, tt.target, got, tt.want)
			}
		})
	}
}
