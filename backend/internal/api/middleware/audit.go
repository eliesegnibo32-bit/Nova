// Audit middleware.
//
// Per the cahier des charges (ch. 11 Sécurité), every state-changing
// administrative action must be recorded in `audit_logs` (an append-only
// table guarded by a trigger, see migrations 010 + 011). The DB insert
// will be wired up in Task 4 once the audit_logs repository exists. For
// now this middleware emits a structured slog record so the audit trail is
// already queryable in the application logs.
//
// Usage:
//
//	r.With(middleware.Audit(log, "shop.create")).Post("/api/shops", ...)
//
// The middleware records the action only when the handler responds 2xx;
// failed attempts (4xx/5xx) are logged at debug level for forensics but
// are not persisted to the audit trail (they're surfaced through the
// normal request logger instead).
package middleware

import (
	"net/http"
	"strings"

	"log/slog"
)

// Audit returns a middleware that logs an audit event after the handler
// runs. The `action` string is a stable identifier like "shop.create" or
// "order.update_status" — it becomes the `action` column in audit_logs.
func Audit(log *slog.Logger, action string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)

			s, _ := SessionFromContext(r.Context())
			status := rec.status
			if status == 0 {
				status = http.StatusOK
			}

			attrs := []slog.Attr{
				slog.String("audit", action),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", status),
				slog.String("request_id", RequestIDFromContext(r.Context())),
				slog.String("ip", clientIP(r)),
				slog.String("user_agent", r.UserAgent()),
			}
			if s != nil {
				attrs = append(attrs,
					slog.String("user_id", s.UserID.String()),
					slog.String("role", s.Role),
				)
				if s.ShopID != nil {
					attrs = append(attrs, slog.String("shop_id", s.ShopID.String()))
				}
			}

			if status >= 200 && status < 300 {
				log.LogAttrs(r.Context(), slog.LevelInfo, "audit", attrs...)
			} else {
				// Failed action — keep at debug so it doesn't pollute the
				// audit trail but is still recoverable from logs.
				attrs = append(attrs, slog.Bool("failed", true))
				log.LogAttrs(r.Context(), slog.LevelDebug, "audit_failed", attrs...)
			}
		})
	}
}

// clientIP returns the best-effort client IP, honoring X-Forwarded-For and
// X-Real-IP when present (typical behind a reverse proxy or Neon's edge).
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first hop (closest to the client).
		if i := strings.Index(xff, ","); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	return r.RemoteAddr
}
