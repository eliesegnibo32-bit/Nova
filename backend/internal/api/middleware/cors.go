// Package middleware groups the cross-cutting HTTP middlewares used by the
// NOVA API: CORS, request-id + structured logging, session authentication,
// tenant/shop context, and audit logging.
package middleware

import (
	"net/http"

	"github.com/go-chi/cors"
)

// CORS returns a chi-compatible middleware that allows the configured
// origins, the standard set of methods, the headers NOVA needs, and credentials.
//
// When allowedOrigins contains "*" (wildcard), the middleware reflects the
// request's Origin header back — this is required because browsers refuse to
// send credentials (cookies) when the server responds with
// Access-Control-Allow-Origin: * (the spec mandates an explicit origin
// when Allow-Credentials is true).
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	if len(allowedOrigins) == 0 {
		allowedOrigins = []string{"http://localhost:3000"}
	}

	// Detect wildcard — switch to origin-reflection mode for credentials.
	wildcard := false
	origins := make([]string, 0, len(allowedOrigins))
	for _, o := range allowedOrigins {
		if o == "*" {
			wildcard = true
			continue
		}
		origins = append(origins, o)
	}
	if wildcard {
		return cors.Handler(cors.Options{
			AllowedOrigins:   []string{},
			AllowOriginFunc:  func(r *http.Request, origin string) bool { return true },
			AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
			AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Shop-ID", "X-Request-ID", "Idempotency-Key"},
			ExposedHeaders:   []string{"X-Request-ID"},
			AllowCredentials: true,
			MaxAge:           300,
		})
	}
	return cors.Handler(cors.Options{
		AllowedOrigins:   origins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Shop-ID", "X-Request-ID", "Idempotency-Key"},
		ExposedHeaders:   []string{"X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           300,
	})
}
