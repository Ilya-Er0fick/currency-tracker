package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/narumov-diyar/currency-tracker/internal/auth"
	"github.com/narumov-diyar/currency-tracker/internal/storage"
)

const minPasswordLength = 8

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type tokenResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	creds, ok := decodeCredentials(w, r)
	if !ok {
		return
	}

	hash, err := auth.HashPassword(creds.Password)
	if err != nil {
		s.fail(w, "hash password", err)
		return
	}

	userID, err := s.users.Create(r.Context(), creds.Email, hash)
	if errors.Is(err, storage.ErrEmailTaken) {
		writeError(w, http.StatusConflict, "email is already registered")
		return
	}
	if err != nil {
		s.fail(w, "create user", err)
		return
	}

	s.issueToken(w, userID, http.StatusCreated)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	creds, ok := decodeCredentials(w, r)
	if !ok {
		return
	}

	user, err := s.users.ByEmail(r.Context(), creds.Email)
	if errors.Is(err, storage.ErrUserNotFound) {
		// Проверка выполняется даже для несуществующего адреса: иначе разница
		// во времени ответа выдала бы, зарегистрирован ли он.
		auth.CheckPasswordDummy(creds.Password)
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	if err != nil {
		s.fail(w, "find user", err)
		return
	}

	if !auth.CheckPassword(user.PasswordHash, creds.Password) {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	s.issueToken(w, user.ID, http.StatusOK)
}

func (s *Server) issueToken(w http.ResponseWriter, userID int64, status int) {
	token, expires, err := s.tokens.Issue(userID)
	if err != nil {
		s.fail(w, "issue token", err)
		return
	}
	writeJSON(w, status, tokenResponse{Token: token, ExpiresAt: expires})
}

func decodeCredentials(w http.ResponseWriter, r *http.Request) (credentials, bool) {
	var creds credentials
	if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
		writeError(w, http.StatusBadRequest, "malformed request body")
		return credentials{}, false
	}

	creds.Email = strings.TrimSpace(strings.ToLower(creds.Email))

	if creds.Email == "" || !strings.Contains(creds.Email, "@") {
		writeError(w, http.StatusBadRequest, "a valid email is required")
		return credentials{}, false
	}
	if len(creds.Password) < minPasswordLength {
		writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return credentials{}, false
	}

	return creds, true
}

func (s *Server) fail(w http.ResponseWriter, what string, err error) {
	s.logger.Error(what+" failed", "error", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

type contextKey int

const userIDKey contextKey = iota

// requireAuth пропускает запрос дальше только с действительным токеном
// и кладёт идентификатор пользователя в контекст запроса.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return s.authenticate(next, headerToken)
}

// requireStreamAuth принимает токен ещё и параметром запроса: браузер не умеет
// задавать заголовки на WebSocket-соединении, и другого способа передать его
// у клиента нет. Для обычных запросов остаётся только заголовок — значение
// в адресе оседает в истории браузера и в журналах посредников.
func (s *Server) requireStreamAuth(next http.HandlerFunc) http.HandlerFunc {
	return s.authenticate(next, func(r *http.Request) string {
		if token := headerToken(r); token != "" {
			return token
		}
		return strings.TrimSpace(r.URL.Query().Get("token"))
	})
}

func (s *Server) authenticate(next http.HandlerFunc, extract func(*http.Request) string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw := extract(r)
		if raw == "" {
			writeError(w, http.StatusUnauthorized, "authorization required")
			return
		}

		userID, err := s.tokens.Parse(raw)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "authorization required")
			return
		}

		next(w, r.WithContext(context.WithValue(r.Context(), userIDKey, userID)))
	}
}

func headerToken(r *http.Request) string {
	raw, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !found {
		return ""
	}
	return strings.TrimSpace(raw)
}

// UserID возвращает идентификатор пользователя, установленный requireAuth.
func UserID(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(userIDKey).(int64)
	return id, ok
}
