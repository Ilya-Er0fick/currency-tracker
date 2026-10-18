package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// Pinger описывает ту часть пула соединений, которая нужна обработчикам.
// Благодаря этому их можно проверять без поднятой базы.
type Pinger interface {
	Ping(ctx context.Context) error
}

type Server struct {
	pool   Pinger
	logger *slog.Logger
}

func NewRouter(pool Pinger, logger *slog.Logger) http.Handler {
	s := &Server{pool: pool, logger: logger}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)

	return requestLogger(logger)(mux)
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
