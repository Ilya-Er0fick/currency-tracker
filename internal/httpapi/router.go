package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/narumov-diyar/currency-tracker/internal/storage"
)

// Pinger описывает ту часть пула соединений, которая нужна обработчикам.
// Благодаря этому их можно проверять без поднятой базы.
type Pinger interface {
	Ping(ctx context.Context) error
}

type UserStore interface {
	Create(ctx context.Context, email, passwordHash string) (int64, error)
	ByEmail(ctx context.Context, email string) (storage.User, error)
}

type TokenIssuer interface {
	Issue(userID int64) (string, time.Time, error)
	Parse(raw string) (int64, error)
}

type Deps struct {
	DB     Pinger
	Users  UserStore
	Tokens TokenIssuer
	Logger *slog.Logger
}

type Server struct {
	pool   Pinger
	users  UserStore
	tokens TokenIssuer
	logger *slog.Logger
}

func NewRouter(d Deps) http.Handler {
	s := &Server{pool: d.DB, users: d.Users, tokens: d.Tokens, logger: d.Logger}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("POST /api/auth/register", s.register)
	mux.HandleFunc("POST /api/auth/login", s.login)

	return requestLogger(d.Logger)(mux)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := s.pool.Ping(ctx); err != nil {
		s.logger.Error("health check failed", "error", err)
		writeError(w, http.StatusServiceUnavailable, "database is unavailable")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
