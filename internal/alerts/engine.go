// Пакет alerts решает, должно ли правило сработать на новых котировках.
package alerts

const (
	DirectionAbove = "above"
	DirectionBelow = "below"
)

// CrossedThreshold сообщает, пересёк ли курс заданный уровень между
// двумя соседними снимками.
//
// Проверяется именно пересечение, а не равенство: цена редко попадает
// в уровень точно, а между снимками может перепрыгнуть его целиком —
// за минуту курс способен уйти с 69 000 сразу на 71 000. Сравнение
// «текущая больше порога» тоже не годится: оно срабатывало бы на каждом
// последующем снимке, пока цена держится выше.
func CrossedThreshold(direction string, previous, current, target float64) bool {
	switch direction {
	case DirectionAbove:
		return previous < target && current >= target
	case DirectionBelow:
		return previous > target && current <= target
	default:
		return false
	}
}

// ExceededPercent сообщает, изменился ли курс относительно значения
// в начале окна наблюдения сильнее заданного процента.
//
// В отличие от порога, здесь сравнивается состояние, а не переход:
// правило описывает «изменился более чем на N% за M минут», и это
// условие либо выполнено на текущий момент, либо нет.
func ExceededPercent(direction string, reference, current, targetPercent float64) bool {
	if reference == 0 {
		return false
	}

	change := (current/reference - 1) * 100

	switch direction {
	case DirectionAbove:
		return change >= targetPercent
	case DirectionBelow:
		return change <= -targetPercent
	default:
		return false
	}
}
