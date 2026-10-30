// TOTP (Time-based One-Time Password) implementation per RFC 6238.
//
// Used by NOVA to enforce 2FA on admin accounts (super_admin / admin) per the
// cahier des charges ch. 9.4 and ch. 11.1. The implementation:
//
//   - SHA-1 HMAC (RFC 6238 default; matches Google Authenticator, Authy, etc.)
//   - 30-second time step
//   - 6-digit truncation
//   - ±1 step window tolerance on validation (90s total skew)
//   - 20-byte (160-bit) random base32 secret
//
// The QR code is delivered as a `data:image/png;base64,...` URI so the
// frontend can render it as a single `<img>` tag without any extra HTTP call.
package auth

import (
        "crypto/hmac"
        "crypto/rand"
        "crypto/sha1"
        "encoding/base32"
        "encoding/base64"
        "encoding/binary"
        "fmt"
        "net/url"
        "strings"
        "time"

        "github.com/skip2/go-qrcode"
)

// totpTimeStep is the RFC 6238 default time step in seconds.
const totpTimeStep = 30

// totpDigits is the number of digits in the generated code.
const totpDigits = 6

// totpSecretLen is the recommended secret length in bytes (RFC 4226 §4, 160
// bits — the de-facto standard for authenticator apps).
const totpSecretLen = 20

// GenerateTOTPSecret returns a base32-encoded random 20-byte secret suitable
// for provisioning in an authenticator app.
func GenerateTOTPSecret() (string, error) {
        buf := make([]byte, totpSecretLen)
        if _, err := rand.Read(buf); err != nil {
                return "", fmt.Errorf("totp: read random: %w", err)
        }
        // StdEncoding (with padding) is what most authenticator apps expect.
        return base32.StdEncoding.EncodeToString(buf), nil
}

// ValidateTOTP returns true if `code` matches the TOTP computed from `secret`
// at the current time, allowing ±window time steps of skew. A window of 1
// means the previous, current, and next 30s steps are all accepted (90s
// total tolerance) — the RFC 6238 §5.2 recommendation.
//
// `secret` must be base32 (standard, with or without padding). `code` is the
// 6-digit string typed by the user; we trim whitespace and ignore dashes/
// spaces so "123-456" works the same as "123456".
func ValidateTOTP(secret, code string, window int) bool {
        if window < 0 {
                window = 0
        }
        // Normalize the user input: drop dashes/spaces, uppercase to be lenient
        // with base32 confusion (codes are digits only, but defensive).
        cleaned := strings.NewReplacer(" ", "", "-", "").Replace(code)
        if len(cleaned) != totpDigits {
                return false
        }

        key, err := base32.StdEncoding.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
        if err != nil {
                // Try without padding — some libraries omit the trailing "=".
                key, err = base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
                if err != nil {
                        return false
                }
        }

        now := time.Now().Unix()
        currentStep := now / totpTimeStep
        for offset := -window; offset <= window; offset++ {
                expected := computeTOTP(key, currentStep+int64(offset))
                if hmac.Equal([]byte(expected), []byte(cleaned)) {
                        return true
                }
        }
        return false
}

// computeTOTP returns the 6-digit TOTP for the given key and time-step
// counter. Implements the HOTP truncate-and-modulo per RFC 4226.
func computeTOTP(key []byte, counter int64) string {
        var msg [8]byte
        binary.BigEndian.PutUint64(msg[:], uint64(counter))

        mac := hmac.New(sha1.New, key)
        mac.Write(msg[:])
        sum := mac.Sum(nil)

        // Dynamic truncation: low 4 bits of the last byte select a 4-byte window.
        offset := int(sum[len(sum)-1] & 0x0F)
        bin := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7FFFFFFF
        code := bin % uint32(pow10(totpDigits))
        return fmt.Sprintf("%0*d", totpDigits, code)
}

// pow10 returns 10^n for small n (n <= 9). Avoids importing math just for
// this trivial calculation.
func pow10(n int) int {
        v := 1
        for i := 0; i < n; i++ {
                v *= 10
        }
        return v
}

// TOTPProvisioningURI returns an `otpauth://totp/...` URI per the de-facto
// standard used by Google Authenticator and friends. Format:
//
//      otpauth://totp/<issuer>:<account>?secret=<base32>&issuer=<issuer>&algorithm=SHA1&digits=6&period=30
//
// The URI is what gets encoded into the QR code. The frontend renders it via
// the data URI returned by GenerateQRCodePNG.
func TOTPProvisioningURI(account, issuer, secret string) string {
        if issuer == "" {
                issuer = "NOVA"
        }
        if account == "" {
                account = "user"
        }
        // `label` per RFC 6238: "Issuer:Account" — both URL-encoded, ":" kept.
        label := url.PathEscape(issuer) + ":" + url.PathEscape(account)
        q := url.Values{}
        q.Set("secret", secret)
        q.Set("issuer", issuer)
        q.Set("algorithm", "SHA1")
        q.Set("digits", "6")
        q.Set("period", "30")
        return "otpauth://totp/" + label + "?" + q.Encode()
}

// GenerateQRCodePNG returns a `data:image/png;base64,...` URI encoding a PNG
// of the QR code for the given otpauth URI. The frontend drops this directly
// into `<img src="<data URI>">`. 256x256 px, sufficient for mobile scanning.
func GenerateQRCodePNG(uri string) (string, error) {
        pngBytes, err := qrcode.Encode(uri, qrcode.Medium, 256)
        if err != nil {
                return "", fmt.Errorf("totp: encode qr: %w", err)
        }
        b64 := base64.StdEncoding.EncodeToString(pngBytes)
        return "data:image/png;base64," + b64, nil
}
