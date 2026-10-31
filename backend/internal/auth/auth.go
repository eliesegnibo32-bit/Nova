// Package auth implements session cookie management and password hashing
// for NOVA. The session is a JSON payload (user_id, shop_id, role, exp)
// signed with HMAC-SHA256 using a server-side secret, then base64url-encoded
// and stored in an HttpOnly cookie. This matches the cahier des charges
// (ch. 11 Sécurité): no client-side session store, no third-party JWT lib,
// cookies carry Secure + SameSite=Lax + HttpOnly.
//
// Why HMAC cookies vs JWT:
//   - Smaller payload, no JWT parsing attack surface (alg=none, kid abuse).
//   - Server can revoke immediately by rotating SESSION_SECRET (rare) or by
//     storing a per-session nonce in PG (future task).
//   - One round-trip cookie verification; no DB lookup needed for the
//     middleware hot path (DB lookup happens in /me for freshness).
package auth

import (
        "crypto/hmac"
        "crypto/sha256"
        "encoding/base64"
        "encoding/json"
        "errors"
        "fmt"
        "net/http"
        "time"

        "github.com/google/uuid"
        "golang.org/x/crypto/bcrypt"
)

// CookieName is the HTTP cookie key that carries the signed session.
const CookieName = "nova_session"

// DefaultSessionTTL is the cookie MaxAge when no ExpiresAt is set.
const DefaultSessionTTL = 7 * 24 * time.Hour // 7 days

// BcryptCost is set to 12 — a good 2024-era tradeoff (~250ms on commodity
// hardware) per the OWASP password storage cheat sheet. The cahier des
// charges recommends Argon2id; we use bcrypt now and will swap in Task 8
// (durcissement) without changing the auth.Session surface.
const BcryptCost = 12

// Session is the signed payload carried in the cookie.
//
//   - UserID:    always set after login
//   - ShopID:    the active shop context (may be nil for super_admin who
//                hasn't picked a shop, or for platform-level endpoints)
//   - Role:      the platform role (super_admin/admin) OR the per-shop role
//                (owner/employee). The middleware uses this for RequireRole.
//   - ExpiresAt: cookie + payload expiry. Verified on every request.
type Session struct {
        UserID    uuid.UUID  `json:"uid"`
        ShopID    *uuid.UUID `json:"sid,omitempty"`
        Role      string     `json:"rol"`
        ExpiresAt time.Time  `json:"exp"`
}

// ErrInvalidSession is returned by VerifySession when the signature does not
// match or the payload cannot be decoded.
var ErrInvalidSession = errors.New("invalid session")

// ErrExpiredSession is returned by VerifySession when the payload is
// well-formed but past its ExpiresAt.
var ErrExpiredSession = errors.New("expired session")

// SignSession serializes the session to JSON, computes an HMAC-SHA256
// signature with the provided secret, and returns a single base64url string
// of the form "<payload>.<signature>" suitable for a cookie value.
func SignSession(s Session, secret []byte) (string, error) {
        if len(secret) < 32 {
                return "", fmt.Errorf("session secret must be at least 32 bytes, got %d", len(secret))
        }
        payload, err := json.Marshal(s)
        if err != nil {
                return "", fmt.Errorf("marshal session: %w", err)
        }

        mac := hmac.New(sha256.New, secret)
        mac.Write(payload)
        sig := mac.Sum(nil)

        encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
        encodedSig := base64.RawURLEncoding.EncodeToString(sig)
        return encodedPayload + "." + encodedSig, nil
}

// VerifySession checks the HMAC signature, decodes the payload, and ensures
// the session has not expired. Returns a typed error so callers can
// distinguish "tampered" from "expired".
func VerifySession(cookieValue string, secret []byte) (*Session, error) {
        payloadB64, sigB64, ok := splitOnce(cookieValue, ".")
        if !ok {
                return nil, ErrInvalidSession
        }

        payload, err := base64.RawURLEncoding.DecodeString(payloadB64)
        if err != nil {
                return nil, ErrInvalidSession
        }
        sig, err := base64.RawURLEncoding.DecodeString(sigB64)
        if err != nil {
                return nil, ErrInvalidSession
        }

        mac := hmac.New(sha256.New, secret)
        mac.Write(payload)
        expectedSig := mac.Sum(nil)
        if !hmac.Equal(sig, expectedSig) {
                return nil, ErrInvalidSession
        }

        var s Session
        if err := json.Unmarshal(payload, &s); err != nil {
                return nil, ErrInvalidSession
        }
        if time.Now().After(s.ExpiresAt) {
                return nil, ErrExpiredSession
        }
        return &s, nil
}

// SetSessionCookie signs the session and writes the cookie to w. The Secure
// flag is set when secure=true (production); SameSite=Lax allows the cookie
// to be sent on top-level GET navigations (so a user clicking a NOVA link
// from another site keeps their session) while blocking CSRF on POSTs from
// other origins.
func SetSessionCookie(w http.ResponseWriter, s Session, secret []byte, secure bool) error {
        value, err := SignSession(s, secret)
        if err != nil {
                return err
        }

        maxAge := int(time.Until(s.ExpiresAt).Seconds())
        if maxAge <= 0 {
                maxAge = int(DefaultSessionTTL.Seconds())
        }

        sameSite := http.SameSiteLaxMode
        if secure {
                sameSite = http.SameSiteNoneMode
        }

        http.SetCookie(w, &http.Cookie{
                Name:     CookieName,
                Value:    value,
                Path:     "/",
                MaxAge:   maxAge,
                HttpOnly: true,
                Secure:   secure,
                SameSite: sameSite,
        })
        return nil
}

// ClearSessionCookie overwrites the session cookie with an immediately
// expired empty value, effectively logging the user out.
func ClearSessionCookie(w http.ResponseWriter) {
        http.SetCookie(w, &http.Cookie{
                Name:     CookieName,
                Value:    "",
                Path:     "/",
                MaxAge:   -1,
                HttpOnly: true,
                Secure:   true,
                SameSite: http.SameSiteNoneMode,
        })
}

// --- Password hashing ------------------------------------------------------

// HashPassword returns a bcrypt hash of the plain password using BcryptCost.
func HashPassword(plain string) (string, error) {
        if plain == "" {
                return "", errors.New("password cannot be empty")
        }
        b, err := bcrypt.GenerateFromPassword([]byte(plain), BcryptCost)
        if err != nil {
                return "", fmt.Errorf("bcrypt hash: %w", err)
        }
        return string(b), nil
}

// CheckPassword returns nil on a match, non-nil otherwise. Always runs in
// constant time relative to the stored hash (bcrypt property).
func CheckPassword(hash, plain string) error {
        return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain))
}

// --- helpers ---------------------------------------------------------------

// splitOnce is a tiny helper that splits s on the first occurrence of sep
// and returns the two halves. It exists to avoid pulling in strings.SplitN
// for a single use.
func splitOnce(s, sep string) (left, right string, ok bool) {
        for i := 0; i+len(sep) <= len(s); i++ {
                if s[i:i+len(sep)] == sep {
                        return s[:i], s[i+len(sep):], true
                }
        }
        return "", "", false
}
