// Webhook verification + signature + Meta envelope parsing (ch. 6 — Canal
// WhatsApp, "Fiabilité du webhook").
//
// Meta calls the webhook in two ways:
//   - GET /webhooks/whatsapp?hub.mode=subscribe&hub.verify_token=...&hub.challenge=...
//     to verify the subscription at app setup time.
//   - POST /webhooks/whatsapp with a JSON envelope (X-Hub-Signature-256
//     header = HMAC-SHA256 of the body with the App Secret) for every inbound
//     message / status update / template approval event.
//
// Critical rules:
//   1. The webhook MUST respond 200 in < 5 seconds — Meta retries on timeout.
//   2. The signature MUST be verified before processing.
//   3. Inbound messages MUST be deduplicated by wamid (Meta retries).
//   4. The processing itself is asynchronous (goroutine).
package whatsapp

import (
        "encoding/json"
        "errors"
        "fmt"
        "net/http"
)

// HandleVerify implements the GET handshake: it checks hub.mode=subscribe,
// hub.verify_token=<verifyToken>, and returns (200, challenge) on success or
// (statusCode, errorMessage) on failure. The caller writes the response.
//
// Status codes:
//   - 400 if hub.mode != "subscribe";
//   - 503 if verifyToken is empty (server misconfigured);
//   - 403 if the token doesn't match;
//   - 200 with the challenge on success.
func HandleVerify(r *http.Request, verifyToken string) (int, string) {
        q := r.URL.Query()
        if q.Get("hub.mode") != "subscribe" {
                return http.StatusBadRequest, "missing or invalid hub.mode"
        }
        if verifyToken == "" {
                return http.StatusServiceUnavailable, "WHATSAPP_VERIFY_TOKEN not configured"
        }
        if q.Get("hub.verify_token") != verifyToken {
                return http.StatusForbidden, "invalid verify token"
        }
        return http.StatusOK, q.Get("hub.challenge")
}

// --- Meta webhook envelope --------------------------------------------------
//
// The full envelope is:
//
//   {
//     "object": "whatsapp_business_account",
//     "entry": [{
//       "id": "123",
//       "changes": [{
//         "field": "messages",
//         "value": {
//           "messaging_product": "whatsapp",
//           "metadata": {"display_phone_number": "+225...", "phone_number_id": "1029..."},
//           "contacts": [{"profile": {"name": "Awa"}, "wa_id": "2250700000000"}],
//           "messages": [{...}],
//           "statuses": [{...}]
//         }
//       }]
//     }]
//   }
//
// At most one of `messages` / `statuses` is populated per change.

// WebhookEvent is the top-level envelope.
type WebhookEvent struct {
        Object string         `json:"object"`
        Entry  []WebhookEntry `json:"entry"`
}

// WebhookEntry is one entry (one per WhatsApp Business Account).
type WebhookEntry struct {
        ID      string          `json:"id"`
        Changes []WebhookChange `json:"changes"`
}

// WebhookChange is one change (always "messages" field for our webhook).
type WebhookChange struct {
        Field string       `json:"field"`
        Value WebhookValue `json:"value"`
}

// WebhookValue carries the messaging metadata + contacts + messages + statuses.
type WebhookValue struct {
        MessagingProduct string           `json:"messaging_product"`
        Metadata         WebhookMetadata  `json:"metadata"`
        Contacts         []WhatsAppContact `json:"contacts,omitempty"`
        Messages         []WhatsAppMessage `json:"messages,omitempty"`
        Statuses         []WhatsAppStatus  `json:"statuses,omitempty"`
}

// WebhookMetadata is the recipient phone metadata.
type WebhookMetadata struct {
        DisplayPhoneNumber string `json:"display_phone_number"`
        PhoneNumberID      string `json:"phone_number_id"`
}

// WhatsAppContact is the sender info Meta includes with inbound messages.
type WhatsAppContact struct {
        Profile struct {
                Name string `json:"name"`
        } `json:"profile"`
        WaID string `json:"wa_id"` // phone number (E.164 without "+")
}

// WhatsAppMessage is one inbound message from a customer.
type WhatsAppMessage struct {
        From        string `json:"from"`
        ID          string `json:"id"`   // wamid
        Type        string `json:"type"` // text | image | audio | video | document | interactive | button | ...
        Timestamp   string `json:"timestamp"`
        Text        *struct {
                Body string `json:"body"`
        } `json:"text,omitempty"`
        Image *struct {
                ID      string `json:"id"`
                Caption string `json:"caption,omitempty"`
                MIMEType string `json:"mime_type,omitempty"`
                SHA256  string `json:"sha256,omitempty"`
        } `json:"image,omitempty"`
        Audio *struct {
                ID       string `json:"id"`
                MIMEType string `json:"mime_type,omitempty"`
                SHA256   string `json:"sha256,omitempty"`
        } `json:"audio,omitempty"`
        Video *struct {
                ID       string `json:"id"`
                Caption  string `json:"caption,omitempty"`
                MIMEType string `json:"mime_type,omitempty"`
        } `json:"video,omitempty"`
        Document *struct {
                ID       string `json:"id"`
                Caption  string `json:"caption,omitempty"`
                MIMEType string `json:"mime_type,omitempty"`
                Filename string `json:"filename,omitempty"`
        } `json:"document,omitempty"`
        Interactive *struct {
                Type        string `json:"type"` // button_reply | list_reply
                ButtonReply *struct {
                        ID    string `json:"id"`
                        Title string `json:"title"`
                } `json:"button_reply,omitempty"`
                ListReply *struct {
                        ID    string `json:"id"`
                        Title string `json:"title"`
                        Description string `json:"description,omitempty"`
                } `json:"list_reply,omitempty"`
        } `json:"interactive,omitempty"`
        Button *struct {
                Text    string `json:"text"`
                Payload string `json:"payload"`
        } `json:"button,omitempty"`
        // Context is set when the customer replies to one of our outbound
        // messages. Context.ID is the wamid of OUR message.
        Context *struct {
                From string `json:"from"`
                ID   string `json:"id"`
        } `json:"context,omitempty"`
        // Errors is set when the message itself couldn't be processed
        // (e.g. unsupported message type). Best-effort.
        Errors []WhatsAppError `json:"errors,omitempty"`
}

// WhatsAppStatus is one delivery status update for an outbound message.
type WhatsAppStatus struct {
        ID        string          `json:"id"` // wamid of OUR message
        Status    string          `json:"status"` // sent | delivered | read | failed | deleted
        Recipient string          `json:"recipient_id"`
        Timestamp string          `json:"timestamp"`
        Errors    []WhatsAppError `json:"errors,omitempty"`
}

// WhatsAppError is one error detail.
type WhatsAppError struct {
        Code    int    `json:"code"`
        Title   string `json:"title,omitempty"`
        Message string `json:"message,omitempty"`
}

// ParseWebhookEvent parses the Meta webhook JSON envelope.
func ParseWebhookEvent(payload []byte) (*WebhookEvent, error) {
        if len(payload) == 0 {
                return nil, errors.New("whatsapp webhook: empty payload")
        }
        var event WebhookEvent
        if err := json.Unmarshal(payload, &event); err != nil {
                return nil, fmt.Errorf("whatsapp webhook: parse: %w", err)
        }
        if event.Object == "" {
                return nil, errors.New("whatsapp webhook: missing 'object' field")
        }
        return &event, nil
}
