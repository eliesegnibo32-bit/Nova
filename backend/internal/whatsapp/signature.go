// Webhook signature verification (ch. 6 — Canal WhatsApp, "Fiabilité du webhook").
//
// Meta signs every POST payload with the App Secret configured in the App
// Dashboard. The signature is sent in the `X-Hub-Signature-256` header as
// `sha256=<hex>`. The receiver MUST verify the signature before processing
// the payload — otherwise an attacker could forge inbound messages.
//
// We compute HMAC-SHA256 of the raw payload with the App Secret and compare
// it to the received signature using hmac.Equal (constant-time comparison to
// avoid timing attacks).
//
// The App Secret is loaded from env var WHATSAPP_APP_SECRET (see config.go).
// When empty (dev mode), the webhook handler accepts unsigned POSTs but logs
// a warning so the operator knows to set WHATSAPP_APP_SECRET before going live.
package whatsapp

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
)

// VerifySignature verifies the X-Hub-Signature-256 header against the raw
// webhook payload. Returns true iff the signature matches. Empty appSecret,
// empty signature, or a signature without the "sha256=" prefix return false
// (fail-closed). Uses crypto/subtle.ConstantTimeCompare under the hood (via
// hmac.Equal) to avoid timing attacks.
//
// This is the spec-named alias for VerifyWebhookSignature. Both names work
// and refer to the same implementation.
func VerifySignature(appSecret string, payload []byte, signature string) bool {
	return VerifyWebhookSignature(payload, signature, appSecret)
}

// VerifyWebhookSignature computes the HMAC-SHA256 of payload using appSecret
// and compares it with the signature Meta sent in X-Hub-Signature-256. The
// signature format is "sha256=<hex>". Returns true iff the signature matches.
//
// We use hmac.Equal (which delegates to crypto/subtle.ConstantTimeCompare)
// to avoid timing attacks. Empty appSecret or empty signature return false
// (fail-closed).
func VerifyWebhookSignature(payload []byte, signature, appSecret string) bool {
	if appSecret == "" || signature == "" {
		return false
	}
	const prefix = "sha256="
	if !strings.HasPrefix(signature, prefix) {
		return false
	}
	received, err := hex.DecodeString(strings.TrimPrefix(signature, prefix))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write(payload)
	expected := mac.Sum(nil)
	// hmac.Equal wraps crypto/subtle.ConstantTimeCompare — constant-time.
	return hmac.Equal(received, expected)
}

// _ is a sentinel that ensures crypto/subtle stays imported even if a future
// refactor stops using hmac.Equal (defensive — keeps the timing-attack-safe
// comparison available without re-importing).
var _ = subtle.ConstantTimeCompare
