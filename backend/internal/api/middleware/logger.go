// Structured request logger. Emits one slog record per request with method,
// path, status, duration, request_id, and remote_addr. Health-check routes
// (/health) are logged at debug level to keep production logs readable.
package middleware

import (
	"net/http"
	"strings"
	"time"

	"log/slog"
)

// statusRecorder wraps http.ResponseWriter to capture the status code that
// was written by the handler.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// Logger returns a middleware that logs every request via the provided
// slog.Logger. The log record carries: method, path, status, duration_ms,
// request_id, remote_addr, bytes.
func Logger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}

			next.ServeHTTP(rec, r)

			duration := time.Since(start)
			status := rec.status
			if status == 0 {
				status = http.StatusOK
			}

			attrs := []slog.Attr{
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", status),
				slog.Int64("duration_ms", duration.Milliseconds()),
				slog.String("request_id", RequestIDFromContext(r.Context())),
				slog.String("remote_addr", r.RemoteAddr),
				slog.Int("bytes", rec.bytes),
			}

			level := slog.LevelInfo
			// Health checks: debug only.
			if strings.HasPrefix(r.URL.Path, "/health") {
				level = slog.LevelDebug
			}
			// 5xx: warn. 4xx: info. 3xx: info.
			if status >= 500 {
				level = slog.LevelError
			}

			log.LogAttrs(r.Context(), level, "http_request", attrs...)
		})
	}
}
