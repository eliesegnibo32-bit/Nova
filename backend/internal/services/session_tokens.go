// Package services contains the business-logic layer of NOVA. It sits
// between the HTTP handlers (which only do request/response marshaling) and
// the repository layer (which only does SQL). The auth service orchestrates
// user registration, login, 2FA, password reset, and audit logging.
package services

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// TempTokenPurpose is a string tag carried inside a temp token to prevent
// cross-flow confusion (a 2FA token can't be reused as a password-reset
// token, and vice versa).
type TempTokenPurpose string

const (
	// PurposeTwoFactor identifies the short-lived token issued between
	// "password OK" and "TOTP code verified" during the 2FA login flow.
	PurposeTwoFactor TempTokenPurpose = "2fa"
	// PurposePasswordReset identifies the longer-lived token issued by
	// RequestPasswordReset and consumed by ResetPassword.
	PurposePasswordReset TempTokenPurpose = "pwreset"
)

// tempTokenPayload is the signed JSON carried inside a temp token. It is
// similar in shape to auth.Session but carries a purpose tag and a much
// shorter lifetime.
type tempTokenPayload struct {
	UserID  uuid.UUID       `json:"uid"`
	Purpose TempTokenPurpose `json:"purp"`
	Iat     int64           `json:"iat"` // unix seconds — issued at
	Exp     int64           `json:"exp"` // unix seconds — expiry
}

// ErrInvalidTempToken is returned by VerifyTempToken when the signature is
// wrong, the payload is malformed, or the token has expired.
var ErrInvalidTempToken = errors.New("invalid temp token")

// ErrTempTokenExpired is returned when the token's signature is valid but
// the expiry has passed. Distinct from ErrInvalidTempToken so callers can
// surface a "code expired, please log in again" message.
var ErrTempTokenExpired = errors.New("temp token expired")

// ErrTempTokenPurposeMismatch is returned when the token's purpose does not
// match the expected purpose (e.g. presenting a password-reset token at the
// 2FA endpoint).
var ErrTempTokenPurposeMismatch = errors.New("temp token purpose mismatch")

// IssueTempToken returns a signed, base64url-encoded token carrying the
// given userID + purpose, valid for `duration`. The token is HMAC-SHA256
// signed with the same secret used for session cookies — that secret must
// be ≥ 32 bytes (see auth.SignSession).
//
// The token format mirrors the session cookie format ("payload.sig") for
// consistency. It is NOT stored server-side: verification is purely
// signature + expiry + purpose. This is fine for short-lived tokens (5 min
// for 2FA, 1h for password reset) because:
//   - An attacker who steals the secret can already forge sessions, so the
//     temp token adds no new attack surface.
//   - Revocation is implicit via expiry — no DB lookup needed.
//   - For password reset, we additionally verify that the user's
//     `updated_at` hasn't moved (i.e. they haven't changed their password
//     since the token was issued). This is done by the auth service, not
//     here, because it needs DB access.
func IssueTempToken(userID uuid.UUID, purpose TempTokenPurpose, secret []byte, duration time.Duration) (string, error) {
	if len(secret) < 32 {
		return "", fmt.Errorf("temp token: secret must be at least 32 bytes, got %d", len(secret))
	}
	if duration <= 0 {
		return "", fmt.Errorf("temp token: duration must be positive")
	}
	now := time.Now()
	p := tempTokenPayload{
		UserID:  userID,
		Purpose: purpose,
		Iat:     now.Unix(),
		Exp:     now.Add(duration).Unix(),
	}
	payload, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("temp token: marshal: %w", err)
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	sig := mac.Sum(nil)

	encPayload := base64.RawURLEncoding.EncodeToString(payload)
	encSig := base64.RawURLEncoding.EncodeToString(sig)
	return encPayload + "." + encSig, nil
}

// VerifyTempToken validates the signature, expiry, and purpose tag of a
// temp token. Returns the embedded user UUID on success, or one of the
// ErrInvalidTempToken / ErrTempTokenExpired / ErrTempTokenPurposeMismatch
// sentinels on failure.
func VerifyTempToken(token string, secret []byte, expectedPurpose TempTokenPurpose) (uuid.UUID, error) {
	dot := -1
	for i := 0; i < len(token); i++ {
		if token[i] == '.' {
			dot = i
			break
		}
	}
	if dot < 0 {
		return uuid.Nil, ErrInvalidTempToken
	}
	encPayload := token[:dot]
	encSig := token[dot+1:]

	payload, err := base64.RawURLEncoding.DecodeString(encPayload)
	if err != nil {
		return uuid.Nil, ErrInvalidTempToken
	}
	sig, err := base64.RawURLEncoding.DecodeString(encSig)
	if err != nil {
		return uuid.Nil, ErrInvalidTempToken
	}

	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	expectedSig := mac.Sum(nil)
	if !hmac.Equal(sig, expectedSig) {
		return uuid.Nil, ErrInvalidTempToken
	}

	var p tempTokenPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return uuid.Nil, ErrInvalidTempToken
	}
	if p.Purpose != expectedPurpose {
		return uuid.Nil, ErrTempTokenPurposeMismatch
	}
	if time.Now().Unix() > p.Exp {
		return uuid.Nil, ErrTempTokenExpired
	}
	return p.UserID, nil
}
