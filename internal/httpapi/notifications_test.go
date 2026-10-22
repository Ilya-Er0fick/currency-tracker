package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/narumov-diyar/currency-tracker/internal/auth"
	"github.com/narumov-diyar/currency-tracker/internal/storage"
)

type stubNotifications struct {
	unseen map[int64][]storage.Notification
	marked []int64
}

func (s *stubNotifications) Unseen(_ context.Context, userID int64) ([]storage.Notification, error) {
	if list, ok := s.unseen[userID]; ok {
		return list, nil
	}
	return []storage.Notification{}, nil
}

func (s *stubNotifications) MarkSeen(_ context.Context, userID int64) error {
	s.marked = append(s.marked, userID)
	delete(s.unseen, userID)
	return nil
}

func newNotificationEnv(t *testing.T, store *stubNotifications) (http.Handler, string) {
	t.Helper()

	tokens := auth.NewTokenIssuer(strings.Repeat("k", 32), time.Hour)
	handler := NewRouter(Deps{
		DB:            stubPinger{},
		Users:         newStubUsers(),
		Assets:        newStubAssets(),
		Pairs:         newStubPairs(),
		Candles:       &stubCandles{},
		Alerts:        newStubAlerts(),
		Notifications: store,
		Tokens:        tokens,
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	token, _, err := tokens.Issue(1)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	return handler, token
}

func request(t *testing.T, handler http.Handler, method, path, token string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestNotificationsRequireToken(t *testing.T) {
	handler, _ := newNotificationEnv(t, &stubNotifications{})

	for _, path := range []string{"/api/notifications", "/api/notifications/seen"} {
		method := http.MethodGet
		if strings.HasSuffix(path, "seen") {
			method = http.MethodPost
		}
		if rec := request(t, handler, method, path, ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want %d", path, rec.Code, http.StatusUnauthorized)
		}
	}
}

func TestListUnseenNotifications(t *testing.T) {
	store := &stubNotifications{unseen: map[int64][]storage.Notification{
		1: {{ID: 7, AlertRuleID: 3, Target: "BTC", Direction: "above", TargetValue: "70000"}},
	}}
	handler, token := newNotificationEnv(t, store)

	rec := request(t, handler, http.MethodGet, "/api/notifications", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var list []storage.Notification
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d notifications, want 1", len(list))
	}
	// Цель показывается словами: один лишь идентификатор правила
	// пользователю ничего не говорит.
	if list[0].Target != "BTC" {
		t.Errorf("target = %q, want BTC", list[0].Target)
	}
}

func TestMarkNotificationsSeen(t *testing.T) {
	store := &stubNotifications{unseen: map[int64][]storage.Notification{
		1: {{ID: 7, Target: "BTC"}},
	}}
	handler, token := newNotificationEnv(t, store)

	if rec := request(t, handler, http.MethodPost, "/api/notifications/seen", token); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}

	rec := request(t, handler, http.MethodGet, "/api/notifications", token)
	var list []storage.Notification
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("got %d notifications, want none after marking them seen", len(list))
	}
}

// Уведомления одного пользователя не должны быть видны другому.
func TestNotificationsIsolateUsers(t *testing.T) {
	store := &stubNotifications{unseen: map[int64][]storage.Notification{
		1: {{ID: 7, Target: "BTC"}},
	}}
	handler, _ := newNotificationEnv(t, store)

	other := auth.NewTokenIssuer(strings.Repeat("k", 32), time.Hour)
	otherToken, _, err := other.Issue(2)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	rec := request(t, handler, http.MethodGet, "/api/notifications", otherToken)
	var list []storage.Notification
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("another user sees %d notifications, want none", len(list))
	}
}
