// Request ID middleware: generates a UUID per request, stores it in the
// request context, and echoes it back in the X-Request-ID response header
// so client-side logs can be correlated with server logs.
package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

// ctxKey is an unexported type so no other package can collide with our
// context keys.
type ctxKey int

const (
	// keyRequestID stores the per-request UUID string.
	keyRequestID ctxKey = iota
	// keySession stores the *auth.Session once the Auth middleware has
	// verified the cookie.
	keySession
)

// RequestIDHeader is the response header name carrying the request UUID.
const RequestIDHeader = "X-Request-ID"

// RequestID generates a UUIDv4 for each incoming request and stores it in
// the request context under keyRequestID. The same value is set on the
// response header so callers can correlate.
//
// If the incoming request already carries an X-Request-ID (e.g. from an
// upstream proxy or the Next.js server component), that value is reused
// as long as it parses as a UUID — this preserves end-to-end trace IDs.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if _, err := uuid.Parse(id); err != nil {
			id = uuid.NewString()
		}
		w.Header().Set(RequestIDHeader, id)
		ctx := context.WithValue(r.Context(), keyRequestID, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFromContext returns the request UUID string, or "" if unset.
func RequestIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(keyRequestID).(string)
	return v
}
