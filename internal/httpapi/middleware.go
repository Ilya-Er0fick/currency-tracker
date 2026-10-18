package httpapi

import (
	"log/slog"
	"net/http"
	"time"
)

// statusRecorder запоминает код ответа: http.ResponseWriter его не отдаёт,
// а он нужен для записи в журнал.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// requestLogger пишет в STDOUT метод, путь, код и длительность каждого запроса.
// Длительность нужна, чтобы проверять требования ко времени отклика на работающем сервисе.
func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(recorder, r)

			logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", recorder.status,
				"duration_ms", time.Since(started).Milliseconds(),
			)
		})
	}
}
