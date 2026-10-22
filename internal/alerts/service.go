package alerts

import (
	"context"
	"log/slog"

	"github.com/narumov-diyar/currency-tracker/internal/storage"
)

type Store interface {
	PendingForAsset(ctx context.Context, assetID int) ([]storage.PendingRule, error)
	PendingForPairsWith(ctx context.Context, assetID int) ([]storage.PendingRule, error)
	Trigger(ctx context.Context, ruleID, userID int64) (bool, error)
}

type Service struct {
	store  Store
	logger *slog.Logger
}

func NewService(store Store, logger *slog.Logger) *Service {
	return &Service{store: store, logger: logger}
}

// CheckAsset проверяет правила, на которые влияет новая котировка актива:
// как поставленные на сам актив, так и на пары с его участием.
// Возвращает число сработавших правил.
func (s *Service) CheckAsset(ctx context.Context, assetID int) (int, error) {
	onAsset, err := s.store.PendingForAsset(ctx, assetID)
	if err != nil {
		return 0, err
	}
	onPairs, err := s.store.PendingForPairsWith(ctx, assetID)
	if err != nil {
		return 0, err
	}

	var fired int
	for _, rule := range append(onAsset, onPairs...) {
		if !shouldTrigger(rule) {
			continue
		}

		// Решение о срабатывании принимается здесь, но окончательным его
		// делает база: если правило уже перевели в сработавшее, повторного
		// уведомления не появится.
		triggered, err := s.store.Trigger(ctx, rule.ID, rule.UserID)
		if err != nil {
			return fired, err
		}
		if triggered {
			fired++
			s.logger.Info("alert triggered",
				"rule", rule.ID, "condition", rule.ConditionType,
				"direction", rule.Direction, "value", rule.Current)
		}
	}

	return fired, nil
}

func shouldTrigger(rule storage.PendingRule) bool {
	switch rule.ConditionType {
	case storage.ConditionThreshold:
		// Без предыдущего снимка переход определить не из чего: это первая
		// котировка актива, и говорить о пересечении уровня рано.
		if rule.Previous == nil {
			return false
		}
		return CrossedThreshold(rule.Direction, *rule.Previous, rule.Current, rule.TargetValue)

	case storage.ConditionPercent:
		// Окно ещё не набралось: сравнивать не с чем.
		if rule.Reference == nil {
			return false
		}
		return ExceededPercent(rule.Direction, *rule.Reference, rule.Current, rule.TargetValue)

	default:
		return false
	}
}
