package storage

import (
	"context"
	"testing"
)

func TestAlertsCreateAndList(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	if _, err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	userID := seedUser(t, pool, "alerts@example.com")
	btc, usdt := assetID(t, pool, "BTC"), assetID(t, pool, "USDT")

	pairs := NewPairs(pool)
	pairID, err := pairs.Create(ctx, userID, btc, usdt)
	if err != nil {
		t.Fatalf("create pair: %v", err)
	}

	alerts := NewAlerts(pool)
	window := 60

	onAsset, err := alerts.Create(ctx, NewAlertRule{
		UserID: userID, TargetType: TargetAsset, AssetID: &btc,
		ConditionType: ConditionThreshold, Direction: "above", TargetValue: "70000",
	})
	if err != nil {
		t.Fatalf("create asset rule: %v", err)
	}
	if onAsset.Symbol == nil || *onAsset.Symbol != "BTC" {
		t.Errorf("symbol = %v, want BTC", onAsset.Symbol)
	}
	if onAsset.Status != StatusActive {
		t.Errorf("status = %q, want %q", onAsset.Status, StatusActive)
	}

	onPair, err := alerts.Create(ctx, NewAlertRule{
		UserID: userID, TargetType: TargetPair, PairID: &pairID,
		ConditionType: ConditionPercent, Direction: "below", TargetValue: "5",
		WindowMinutes: &window,
	})
	if err != nil {
		t.Fatalf("create pair rule: %v", err)
	}
	// Правило на пару показывается обеими сторонами, иначе в списке будет
	// непонятно, к чему оно относится.
	if onPair.Pair == nil || *onPair.Pair != "BTC/USDT" {
		t.Errorf("pair = %v, want BTC/USDT", onPair.Pair)
	}

	list, err := alerts.List(ctx, userID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d rules, want 2", len(list))
	}
}

// База сама отклоняет правила, нарушающие согласованность цели и условия,
// даже если проверка в обработчике будет пропущена.
func TestAlertsSchemaRejectsInconsistentRules(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	if _, err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	userID := seedUser(t, pool, "bad@example.com")
	btc := assetID(t, pool, "BTC")
	alerts := NewAlerts(pool)
	window := 60

	tests := []struct {
		name string
		rule NewAlertRule
	}{
		{
			name: "цель asset без актива",
			rule: NewAlertRule{UserID: userID, TargetType: TargetAsset,
				ConditionType: ConditionThreshold, Direction: "above", TargetValue: "1"},
		},
		{
			name: "процент без интервала",
			rule: NewAlertRule{UserID: userID, TargetType: TargetAsset, AssetID: &btc,
				ConditionType: ConditionPercent, Direction: "above", TargetValue: "5"},
		},
		{
			name: "неизвестное направление",
			rule: NewAlertRule{UserID: userID, TargetType: TargetAsset, AssetID: &btc,
				ConditionType: ConditionThreshold, Direction: "sideways", TargetValue: "1",
				WindowMinutes: &window},
		},
		{
			name: "неположительное значение",
			rule: NewAlertRule{UserID: userID, TargetType: TargetAsset, AssetID: &btc,
				ConditionType: ConditionThreshold, Direction: "above", TargetValue: "0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := alerts.Create(ctx, tt.rule); err == nil {
				t.Error("база приняла правило, которое должна была отклонить")
			}
		})
	}
}

// Изменение условия возвращает правило в активное состояние: прежняя отметка
// о срабатывании относилась к другому условию.
func TestAlertUpdateResetsTriggeredStatus(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	if _, err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	userID := seedUser(t, pool, "reset@example.com")
	btc := assetID(t, pool, "BTC")
	alerts := NewAlerts(pool)

	rule, err := alerts.Create(ctx, NewAlertRule{
		UserID: userID, TargetType: TargetAsset, AssetID: &btc,
		ConditionType: ConditionThreshold, Direction: "above", TargetValue: "70000",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := pool.Exec(ctx,
		`UPDATE alert_rules SET status = $1 WHERE id = $2`, StatusTriggered, rule.ID); err != nil {
		t.Fatalf("mark triggered: %v", err)
	}

	value := "80000"
	updated, err := alerts.Update(ctx, userID, rule.ID, AlertUpdate{TargetValue: &value})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Status != StatusActive {
		t.Errorf("status = %q, want %q after changing the condition", updated.Status, StatusActive)
	}
	if updated.TargetValue != "80000.00000000" {
		t.Errorf("target value = %q, want the new threshold", updated.TargetValue)
	}
}

func TestAlertsIsolateUsers(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	if _, err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	first := seedUser(t, pool, "first-alerts@example.com")
	second := seedUser(t, pool, "second-alerts@example.com")
	btc := assetID(t, pool, "BTC")
	alerts := NewAlerts(pool)

	rule, err := alerts.Create(ctx, NewAlertRule{
		UserID: first, TargetType: TargetAsset, AssetID: &btc,
		ConditionType: ConditionThreshold, Direction: "above", TargetValue: "70000",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	value := "1"
	if _, err := alerts.Update(ctx, second, rule.ID, AlertUpdate{TargetValue: &value}); err != ErrAlertNotFound {
		t.Errorf("update by another user: err = %v, want ErrAlertNotFound", err)
	}
	if err := alerts.Delete(ctx, second, rule.ID); err != ErrAlertNotFound {
		t.Errorf("delete by another user: err = %v, want ErrAlertNotFound", err)
	}

	list, err := alerts.List(ctx, second)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("another user sees %d rules, want none", len(list))
	}
}

// Правило на пару исчезает вместе с парой: условие по удалённой паре
// проверять не на чем.
func TestAlertRemovedWithItsPair(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	if _, err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	userID := seedUser(t, pool, "cascade@example.com")
	btc, usdt := assetID(t, pool, "BTC"), assetID(t, pool, "USDT")

	pairs := NewPairs(pool)
	pairID, err := pairs.Create(ctx, userID, btc, usdt)
	if err != nil {
		t.Fatalf("create pair: %v", err)
	}

	alerts := NewAlerts(pool)
	if _, err := alerts.Create(ctx, NewAlertRule{
		UserID: userID, TargetType: TargetPair, PairID: &pairID,
		ConditionType: ConditionThreshold, Direction: "above", TargetValue: "0.05",
	}); err != nil {
		t.Fatalf("create rule: %v", err)
	}

	if err := pairs.Delete(ctx, userID, pairID); err != nil {
		t.Fatalf("delete pair: %v", err)
	}

	list, err := alerts.List(ctx, userID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("got %d rules, want none after the pair was removed", len(list))
	}
}
