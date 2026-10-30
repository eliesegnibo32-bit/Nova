// Session authentication middleware.
//
// Auth        — verifies the nova_session cookie and stores *auth.Session
//               in the request context. Does NOT reject unauthenticated
//               requests (use RequireAuth on protected routes).
// RequireAuth — 401 if no valid session is present.
// RequireRole — 403 if the session role is not in the allowed list.
// RequireShop — 403 if the session has no active shop (ShopID == nil).
package middleware

import (
	"context"
	"encoding/json"
	"net/http"

	"nova-api/internal/auth"
)

// SessionFromContext returns the verified session stored by Auth, or
// (nil, false) if the request was unauthenticated or the cookie was bad.
func SessionFromContext(ctx context.Context) (*auth.Session, bool) {
	v, _ := ctx.Value(keySession).(*auth.Session)
	return v, v != nil
}

// Auth returns a middleware that reads the nova_session cookie, verifies
// its HMAC signature and expiry, and — on success — stores *auth.Session
// in the request context for downstream handlers and middlewares.
//
// On failure (missing, tampered, or expired cookie) the request continues
// without a session; RequireAuth is responsible for rejecting protected
// routes with a 401.
func Auth(sessionSecret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(auth.CookieName)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			s, err := auth.VerifySession(cookie.Value, sessionSecret)
			if err != nil {
				// Tampered or expired: clear the cookie so the browser
				// drops it, then continue unauthenticated.
				auth.ClearSessionCookie(w)
				next.ServeHTTP(w, r)
				return
			}
			ctx := context.WithValue(r.Context(), keySession, s)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAuth rejects the request with 401 if no session is present.
// The body is a small JSON error envelope.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, ok := SessionFromContext(r.Context())
		if !ok || s == nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireRole returns a middleware that allows only the listed roles. The
// role comparison is case-sensitive and matches the values stored in
// auth.Session (super_admin, admin, owner, employee).
func RequireRole(roles ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(roles))
	for _, r := range roles {
		allowed[r] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s, ok := SessionFromContext(r.Context())
			if !ok || s == nil {
				writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required.")
				return
			}
			if _, allowedRole := allowed[s.Role]; !allowedRole {
				writeError(w, http.StatusForbidden, "forbidden", "Your role is not permitted to perform this action.")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireShop rejects the request with 403 if the session has no active
// shop. Use on owner/employee endpoints that operate on a single shop.
func RequireShop(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, ok := SessionFromContext(r.Context())
		if !ok || s == nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required.")
			return
		}
		if s.ShopID == nil {
			writeError(w, http.StatusForbidden, "no_active_shop", "No active shop selected for this session.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// writeError emits a small JSON error envelope. It is intentionally simple
// — a richer version lives in the handlers package; middleware keeps its
// own copy to avoid an import cycle (handlers imports middleware).
func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":   code,
		"message": message,
	})
}
