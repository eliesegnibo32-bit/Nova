// Unit tests for the temp token (2FA / password-reset) implementation.
package services

import (
        "errors"
        "testing"
        "time"

        "github.com/google/uuid"
)

func testSecret() []byte {
        // 32 bytes — the minimum accepted by IssueTempToken.
        return []byte("0123456789ABCDEF0123456789ABCDEF")
}

func TestIssueAndVerifyTempToken(t *testing.T) {
        uid := uuid.New()
        tok, err := IssueTempToken(uid, PurposeTwoFactor, testSecret(), 5*time.Minute)
        if err != nil {
                t.Fatalf("IssueTempToken: %v", err)
        }
        if tok == "" {
                t.Fatal("empty token")
        }
        got, err := VerifyTempToken(tok, testSecret(), PurposeTwoFactor)
        if err != nil {
                t.Fatalf("VerifyTempToken: %v", err)
        }
        if got != uid {
                t.Fatalf("expected %s, got %s", uid, got)
        }
}

func TestVerifyTempTokenExpired(t *testing.T) {
        uid := uuid.New()
        // Issue with a 1-second duration. Exp is stored as a unix-second
        // timestamp, so the token is only verifiably expired once
        // time.Now().Unix() strictly exceeds Exp — worst case that's ~2s
        // after issuance (if we issued just before a second boundary). We
        // sleep 2.2s to be safely past the boundary on any reasonable host.
        tok, err := IssueTempToken(uid, PurposeTwoFactor, testSecret(), 1*time.Second)
        if err != nil {
                t.Fatalf("IssueTempToken: %v", err)
        }
        time.Sleep(2200 * time.Millisecond)
        _, err = VerifyTempToken(tok, testSecret(), PurposeTwoFactor)
        if !errors.Is(err, ErrTempTokenExpired) {
                t.Fatalf("expected ErrTempTokenExpired, got %v", err)
        }
}

func TestVerifyTempTokenPurposeMismatch(t *testing.T) {
        uid := uuid.New()
        tok, err := IssueTempToken(uid, PurposePasswordReset, testSecret(), 5*time.Minute)
        if err != nil {
                t.Fatalf("IssueTempToken: %v", err)
        }
        _, err = VerifyTempToken(tok, testSecret(), PurposeTwoFactor)
        if !errors.Is(err, ErrTempTokenPurposeMismatch) {
                t.Fatalf("expected ErrTempTokenPurposeMismatch, got %v", err)
        }
}

func TestVerifyTempTokenBadSignature(t *testing.T) {
        uid := uuid.New()
        tok, err := IssueTempToken(uid, PurposeTwoFactor, testSecret(), 5*time.Minute)
        if err != nil {
                t.Fatalf("IssueTempToken: %v", err)
        }
        // Verify with a different secret.
        other := []byte("FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF")
        _, err = VerifyTempToken(tok, other, PurposeTwoFactor)
        if !errors.Is(err, ErrInvalidTempToken) {
                t.Fatalf("expected ErrInvalidTempToken, got %v", err)
        }
}

func TestVerifyTempTokenMalformed(t *testing.T) {
        for _, tc := range []string{
                "",
                "no-dot-here",
                "abc.def.ghi", // extra dot is fine, but content isn't base64
                "!!!.!!!",
        } {
                _, err := VerifyTempToken(tc, testSecret(), PurposeTwoFactor)
                if err == nil {
                        t.Fatalf("expected error for %q, got nil", tc)
                }
        }
}

func TestIssueTempTokenRejectsShortSecret(t *testing.T) {
        _, err := IssueTempToken(uuid.New(), PurposeTwoFactor, []byte("short"), time.Minute)
        if err == nil {
                t.Fatal("expected error for short secret")
        }
}
