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

type stubUsers struct {
	byEmail map[string]storage.User
	nextID  int64
}

func newStubUsers() *stubUsers {
	return &stubUsers{byEmail: map[string]storage.User{}, nextID: 1}
}

func (s *stubUsers) Create(_ context.Context, email, passwordHash string) (int64, error) {
	if _, exists := s.byEmail[email]; exists {
		return 0, storage.ErrEmailTaken
	}
	id := s.nextID
	s.nextID++
	s.byEmail[email] = storage.User{ID: id, Email: email, PasswordHash: passwordHash}
	return id, nil
}

func (s *stubUsers) ByEmail(_ context.Context, email string) (storage.User, error) {
	user, exists := s.byEmail[email]
	if !exists {
		return storage.User{}, storage.ErrUserNotFound
	}
	return user, nil
}

func newAuthRouter(users UserStore) (http.Handler, TokenIssuer) {
	tokens := auth.NewTokenIssuer(strings.Repeat("k", 32), time.Hour)
	handler := NewRouter(Deps{
		DB:     stubPinger{},
		Users:  users,
		Tokens: tokens,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	return handler, tokens
}

func post(t *testing.T, handler http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(rec, req)
	return rec
}

func TestRegisterIssuesToken(t *testing.T) {
	handler, tokens := newAuthRouter(newStubUsers())

	rec := post(t, handler, "/api/auth/register", `{"email":"User@Example.com","password":"secret123"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body)
	}

	var body tokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	userID, err := tokens.Parse(body.Token)
	if err != nil {
		t.Fatalf("the issued token does not parse: %v", err)
	}
	if userID != 1 {
		t.Errorf("user id = %d, want 1", userID)
	}
}

func TestRegisterRejectsDuplicateEmail(t *testing.T) {
	handler, _ := newAuthRouter(newStubUsers())
	body := `{"email":"user@example.com","password":"secret123"}`

	if rec := post(t, handler, "/api/auth/register", body); rec.Code != http.StatusCreated {
		t.Fatalf("first registration failed: %d", rec.Code)
	}

	rec := post(t, handler, "/api/auth/register", body)
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

// Адрес приводится к нижнему регистру, иначе один и тот же ящик можно было бы
// зарегистрировать дважды, написав его по-разному.
func TestRegisterNormalisesEmail(t *testing.T) {
	handler, _ := newAuthRouter(newStubUsers())

	post(t, handler, "/api/auth/register", `{"email":"User@Example.com","password":"secret123"}`)
	rec := post(t, handler, "/api/auth/register", `{"email":"  user@example.com ","password":"secret123"}`)

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestRegisterValidatesInput(t *testing.T) {
	handler, _ := newAuthRouter(newStubUsers())

	tests := []struct {
		name string
		body string
	}{
		{name: "malformed json", body: `{`},
		{name: "empty email", body: `{"email":"","password":"secret123"}`},
		{name: "email without at sign", body: `{"email":"nobody","password":"secret123"}`},
		{name: "short password", body: `{"email":"a@b.c","password":"short"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if rec := post(t, handler, "/api/auth/register", tt.body); rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestLoginSucceedsAndFailsIdentically(t *testing.T) {
	handler, _ := newAuthRouter(newStubUsers())
	post(t, handler, "/api/auth/register", `{"email":"user@example.com","password":"secret123"}`)

	ok := post(t, handler, "/api/auth/login", `{"email":"user@example.com","password":"secret123"}`)
	if ok.Code != http.StatusOK {
		t.Fatalf("valid login status = %d, want %d", ok.Code, http.StatusOK)
	}

	wrongPassword := post(t, handler, "/api/auth/login", `{"email":"user@example.com","password":"wrongpassword"}`)
	unknownEmail := post(t, handler, "/api/auth/login", `{"email":"nobody@example.com","password":"secret123"}`)

	if wrongPassword.Code != http.StatusUnauthorized || unknownEmail.Code != http.StatusUnauthorized {
		t.Fatalf("statuses = %d and %d, want %d for both",
			wrongPassword.Code, unknownEmail.Code, http.StatusUnauthorized)
	}

	// Ответы совпадают дословно: иначе по разнице видно, зарегистрирован ли адрес.
	if wrongPassword.Body.String() != unknownEmail.Body.String() {
		t.Errorf("responses differ and reveal whether the email exists:\n%s\n%s",
			wrongPassword.Body, unknownEmail.Body)
	}
}

func TestRequireAuth(t *testing.T) {
	users := newStubUsers()
	handler, tokens := newAuthRouter(users)

	rec := post(t, handler, "/api/auth/register", `{"email":"user@example.com","password":"secret123"}`)
	var issued tokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &issued); err != nil {
		t.Fatalf("decode: %v", err)
	}

	server := &Server{tokens: tokens, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	var seenUserID int64
	protected := server.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		seenUserID, _ = UserID(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		name   string
		header string
		want   int
	}{
		{name: "no header", header: "", want: http.StatusUnauthorized},
		{name: "without bearer prefix", header: issued.Token, want: http.StatusUnauthorized},
		{name: "empty bearer", header: "Bearer ", want: http.StatusUnauthorized},
		{name: "garbage token", header: "Bearer not.a.token", want: http.StatusUnauthorized},
		{name: "valid token", header: "Bearer " + issued.Token, want: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seenUserID = 0
			r := httptest.NewRequest(http.MethodGet, "/protected", nil)
			if tt.header != "" {
				r.Header.Set("Authorization", tt.header)
			}
			w := httptest.NewRecorder()
			protected(w, r)

			if w.Code != tt.want {
				t.Fatalf("status = %d, want %d", w.Code, tt.want)
			}
			if tt.want == http.StatusOK && seenUserID != 1 {
				t.Errorf("user id in context = %d, want 1", seenUserID)
			}
		})
	}
}

// Браузер не умеет задавать заголовки на WebSocket-соединении, поэтому для
// потока токен принимается и параметром запроса.
func TestStreamAuthAcceptsTokenFromQuery(t *testing.T) {
	tokens := auth.NewTokenIssuer(strings.Repeat("k", 32), time.Hour)
	token, _, err := tokens.Issue(42)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	server := &Server{tokens: tokens}
	var seen int64
	handler := server.requireStreamAuth(func(w http.ResponseWriter, r *http.Request) {
		seen, _ = UserID(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		name    string
		query   string
		header  string
		want    int
		wantUID int64
	}{
		{name: "параметром запроса", query: "?token=" + token, want: http.StatusOK, wantUID: 42},
		{name: "заголовком", header: "Bearer " + token, want: http.StatusOK, wantUID: 42},
		{name: "без токена", want: http.StatusUnauthorized},
		{name: "с негодным токеном", query: "?token=nonsense", want: http.StatusUnauthorized},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			seen = 0
			req := httptest.NewRequest(http.MethodGet, "/api/stream"+tc.query, nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}

			rec := httptest.NewRecorder()
			handler(rec, req)

			if rec.Code != tc.want {
				t.Fatalf("статус = %d, ожидался %d", rec.Code, tc.want)
			}
			if seen != tc.wantUID {
				t.Errorf("пользователь = %d, ожидался %d", seen, tc.wantUID)
			}
		})
	}
}

// Обычные эндпоинты токен из адреса принимать не должны: значение в адресе
// оседает в истории браузера и в журналах посредников.
func TestRequireAuthIgnoresQueryToken(t *testing.T) {
	tokens := auth.NewTokenIssuer(strings.Repeat("k", 32), time.Hour)
	token, _, err := tokens.Issue(42)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	server := &Server{tokens: tokens}
	handler := server.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/api/watchlist?token="+token, nil))

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("статус = %d, ожидался %d", rec.Code, http.StatusUnauthorized)
	}
}
