// WhatsApp Cloud API webhook handler (ch. 6 — Canal WhatsApp et règles de
// messagerie).
//
// Meta calls this endpoint to:
//  1. GET /webhooks/whatsapp — verify the webhook subscription at app setup.
//     Meta sends hub.mode=subscribe, hub.challenge=<int>, hub.verify_token=<token>.
//     We respond with the challenge (plain text) if the verify_token matches
//     WHATSAPP_VERIFY_TOKEN, or 403 otherwise.
//
//  2. POST /webhooks/whatsapp — deliver inbound messages and status updates.
//     The payload is a JSON envelope from Meta. We:
//      a. Read the raw body (for signature verification).
//      b. Verify the X-Hub-Signature-256 header (HMAC-SHA256 with AppSecret).
//      c. Parse the webhook event.
//      d. Respond 200 IMMEDIATELY (Meta requires fast response, < 5s).
//      e. Launch a goroutine to process the event asynchronously.
//
// This endpoint is NOT shop-scoped (Meta calls it with a phone number, not a
// shop_id). The processor resolves the shop from the recipient phone number
// via ShopRepository.GetByWhatsAppNumber.
package handlers

import (
        "context"
        "encoding/json"
        "io"
        "net/http"
        "time"

        "github.com/go-chi/chi/v5"
        "log/slog"

        "nova-api/internal/whatsapp"
)

// WhatsAppWebhookHandler handles Meta's webhook verify + inbound events.
type WhatsAppWebhookHandler struct {
        processor         *whatsapp.Processor
        verifyToken       string
        appSecret         string
        // signatureRequired is true when AppSecret is set — in that case we
        // reject any POST without a valid X-Hub-Signature-256 header. When
        // AppSecret is empty (dev mode), we accept unsigned POSTs but log a
        // warning so the operator knows to set WHATSAPP_APP_SECRET.
        signatureRequired bool
        log               *slog.Logger
}

// NewWhatsAppWebhookHandler constructs a full webhook handler with the given
// processor + Meta credentials. verifyToken and appSecret come from
// WHATSAPP_VERIFY_TOKEN / WHATSAPP_APP_SECRET.
func NewWhatsAppWebhookHandler(processor *whatsapp.Processor, verifyToken, appSecret string, log *slog.Logger) *WhatsAppWebhookHandler {
        if log == nil {
                log = slog.Default()
        }
        return &WhatsAppWebhookHandler{
                processor:         processor,
                verifyToken:       verifyToken,
                appSecret:         appSecret,
                signatureRequired: appSecret != "",
                log:               log,
        }
}

// Register adds the webhook routes to the given chi.Router (NOT under /api —
// Meta's webhook URL is configured at the app level, typically /webhooks/whatsapp).
func (h *WhatsAppWebhookHandler) Register(r chi.Router) {
        r.Get("/webhooks/whatsapp", h.Verify)
        r.Post("/webhooks/whatsapp", h.Receive)
}

// Verify handles Meta's webhook subscription handshake (GET).
//
// Meta sends:
//
//      GET /webhooks/whatsapp?hub.mode=subscribe&hub.challenge=12345&hub.verify_token=TOKEN
//
// We respond with the challenge (as plain text) when the verify_token matches.
func (h *WhatsAppWebhookHandler) Verify(w http.ResponseWriter, r *http.Request) {
        status, body := whatsapp.HandleVerify(r, h.verifyToken)
        if status != http.StatusOK {
                http.Error(w, body, status)
                return
        }
        w.Header().Set("Content-Type", "text/plain; charset=utf-8")
        w.WriteHeader(http.StatusOK)
        _, _ = io.WriteString(w, body)
}

// Receive handles inbound messages + status updates (POST).
//
// Flow (ch. 6 — Fiabilité du webhook):
//   1. Read the raw body (we need the exact bytes for HMAC verification).
//   2. Verify the X-Hub-Signature-256 header. If AppSecret is set, an
//      invalid signature → 403 (fail-closed). If AppSecret is empty, accept
//      the POST but log a warning (dev mode).
//   3. Parse the webhook envelope. Invalid JSON → 400.
//   4. Respond 200 IMMEDIATELY (Meta requires < 5s response). Process
//      asynchronously in a goroutine.
//   5. The goroutine calls processor.ProcessWebhook with a fresh context
//      (decoupled from the HTTP request's lifetime).
func (h *WhatsAppWebhookHandler) Receive(w http.ResponseWriter, r *http.Request) {
        body, err := io.ReadAll(r.Body)
        if err != nil {
                http.Error(w, "failed to read body", http.StatusBadRequest)
                return
        }
        defer r.Body.Close()

        // 2. Signature verification.
        sig := r.Header.Get("X-Hub-Signature-256")
        if h.signatureRequired {
                if !whatsapp.VerifyWebhookSignature(body, sig, h.appSecret) {
                        h.log.Warn("whatsapp webhook: invalid signature — rejecting",
                                "ip", r.RemoteAddr,
                                "has_sig", sig != "",
                                "body_len", len(body))
                        http.Error(w, "invalid signature", http.StatusForbidden)
                        return
                }
        } else {
                // Dev mode — AppSecret not set. Accept the POST but warn.
                if sig == "" {
                        h.log.Warn("whatsapp webhook: signature not verified (WHATSAPP_APP_SECRET empty) — accepting in dev mode")
                } else if !whatsapp.VerifyWebhookSignature(body, sig, h.appSecret) {
                        h.log.Warn("whatsapp webhook: signature present but AppSecret is empty — accepting")
                }
        }

        // 3. Parse the envelope. We validate it's JSON; if it's empty or
        //    malformed we return 400 (Meta will retry with a valid payload).
        if len(body) == 0 {
                http.Error(w, "empty body", http.StatusBadRequest)
                return
        }
        event, err := whatsapp.ParseWebhookEvent(body)
        if err != nil {
                // Best-effort probe: if it's not even valid JSON, return 400.
                var probe map[string]any
                if jsonErr := json.Unmarshal(body, &probe); jsonErr != nil {
                        h.log.Warn("whatsapp webhook: invalid JSON — rejecting",
                                "error", jsonErr.Error(), "body_len", len(body))
                        http.Error(w, "invalid JSON body", http.StatusBadRequest)
                        return
                }
                // It's valid JSON but missing required fields — accept (Meta
                // sometimes sends keep-alive pings). Don't fail.
                h.log.Debug("whatsapp webhook: valid JSON but missing fields — accepting",
                        "error", err.Error())
                w.Header().Set("Content-Type", "application/json; charset=utf-8")
                w.WriteHeader(http.StatusOK)
                _, _ = io.WriteString(w, `{"ok":true,"ignored":true}`)
                return
        }

        // 4. ACK 200 IMMEDIATELY. Meta retries on slow / no response.
        w.Header().Set("Content-Type", "application/json; charset=utf-8")
        w.WriteHeader(http.StatusOK)
        _, _ = io.WriteString(w, `{"ok":true}`)

        // 5. Process asynchronously. We use a fresh context (decoupled from
        //    the HTTP request — which may be closed by the time the
        //    processor finishes). A 60s timeout prevents runaway goroutines.
        if h.processor == nil {
                h.log.Warn("whatsapp webhook: processor is nil — event dropped",
                        "object", event.Object, "entries", len(event.Entry))
                return
        }
        go func(ev *whatsapp.WebhookEvent) {
                ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
                defer cancel()
                if err := h.processor.ProcessWebhook(ctx, ev); err != nil {
                        h.log.Error("whatsapp webhook: async process failed",
                                "error", err, "object", ev.Object)
                }
        }(event)
}
