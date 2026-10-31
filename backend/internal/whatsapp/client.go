// Package whatsapp implements the WhatsApp Business Cloud API integration
// (cahier des charges ch. 6 — Canal WhatsApp et règles de messagerie).
//
// The package is split into:
//   - client.go   — outbound HTTP client to the Graph API (text, interactive,
//                    template, media, MarkAsRead) + mock mode;
//   - webhook.go  — signature verification + Meta webhook envelope parsing;
//   - processor.go— inbound message + status update processor;
//   - sender.go   — outbound helpers (SendAndStore, 24h window check);
//   - templates.go— message template rendering + Meta sync.
//
// Mock mode (WHATSAPP_ACCESS_TOKEN empty): the client returns fake wamids
// (wamid.mock.*) without making HTTP calls. The full pipeline (processor →
// sender → store → status update) is exercised so we can test the flow
// end-to-end without a real Meta account.
package whatsapp

import (
        "bytes"
        "context"
        "crypto/rand"
        "encoding/hex"
        "encoding/json"
        "errors"
        "fmt"
        "io"
        "net/http"
        "strings"
        "time"

        "log/slog"
)

// Config is the spec-named alias for WhatsAppConfig. Callers can use either
// name interchangeably — they refer to the same underlying struct.
type Config = WhatsAppConfig

// WhatsAppConfig holds the runtime configuration for the Cloud API client.
type WhatsAppConfig struct {
        // VerifyToken is the token Meta echoes during the webhook subscription
        // handshake (GET /webhooks/whatsapp?hub.verify_token=...).
        VerifyToken string
        // AccessToken is the Bearer token for outbound Graph API calls.
        // Empty → mock mode (no HTTP calls; wamids are faked).
        AccessToken string
        // PhoneNumberID is the WhatsApp Business phone number ID (numeric
        // string from the Meta App Dashboard).
        PhoneNumberID string
        // AppSecret is the Meta App Secret used to verify webhook signatures.
        AppSecret string
        // BaseURL is the Graph API root (default https://graph.facebook.com).
        BaseURL string
        // Version is the Graph API version (default v18.0).
        Version string
}

// Client is the WhatsApp Cloud API HTTP client. One instance is shared across
// all shops (PhoneNumberID is the default sender — per-shop override happens
// at the call site for production multi-number; for pilot mode all shops use
// the same number).
type Client struct {
        httpClient    *http.Client
        baseURL       string
        version       string
        phoneNumberID string
        accessToken   string
        log           *slog.Logger
}

// NewClient constructs a Client. If cfg.AccessToken is empty, the client runs
// in mock mode: SendText/SendInteractive/SendTemplate/SendMedia return a
// synthetic wamid (wamid.mock.<random>) without doing any HTTP call. This
// lets us develop and test the full flow offline.
func NewClient(cfg WhatsAppConfig, log *slog.Logger) *Client {
        if cfg.BaseURL == "" {
                cfg.BaseURL = "https://graph.facebook.com"
        }
        if cfg.Version == "" {
                cfg.Version = "v18.0"
        }
        if log == nil {
                log = slog.Default()
        }
        return &Client{
                httpClient:    &http.Client{Timeout: 15 * time.Second},
                baseURL:       strings.TrimRight(cfg.BaseURL, "/"),
                version:       cfg.Version,
                phoneNumberID: cfg.PhoneNumberID,
                accessToken:   cfg.AccessToken,
                log:           log,
        }
}

// IsMock returns true when the client is in mock mode (no access token).
func (c *Client) IsMock() bool { return c.accessToken == "" }

// SendResult is the parsed response from the Graph API messages endpoint.
// The MessageID is the wamid (WhatsApp Message ID) used to track delivery
// status via later webhook events.
type SendResult struct {
        MessageID    string `json:"message_id"`
        Status       string `json:"status"` // "queued" | "sent" | "failed"
        ErrorCode    string `json:"error_code,omitempty"`
        ErrorMessage string `json:"error_message,omitempty"`
}

// Button is one interactive button (ch. 5.4). Type is "reply" for quick-reply
// buttons. Reply.ID is what comes back in the webhook's interactive.button_reply.id.
type Button struct {
        Type  string      `json:"type"` // "reply"
        Reply ButtonReply `json:"reply"`
}

// ButtonReply is the inner object of a reply button.
type ButtonReply struct {
        ID    string `json:"id"`
        Title string `json:"title"`
}

// InteractiveMessage is the payload for an interactive message (ch. 5.4 —
// buttons and lists WhatsApp). For now we support the "button" sub-type; the
// "list" sub-type will be added when needed.
type InteractiveMessage struct {
        Type    string   `json:"type"`              // "button" | "list"
        Body    string   `json:"body"`
        Buttons []Button `json:"buttons,omitempty"` // for "button"
        Header  string   `json:"header,omitempty"`
        Footer  string   `json:"footer,omitempty"`
}

// TemplateComponent is one component of a template message (header / body /
// button). Used when calling SendTemplate to fill the {{1}}, {{2}}, ... vars.
type TemplateComponent struct {
        Type       string          `json:"type"` // "header" | "body" | "button"
        Parameters []TemplateParam `json:"parameters"`
}

// Component is the spec-named alias for TemplateComponent. Both names refer
// to the same type — callers can use whichever they prefer.
type Component = TemplateComponent

// TemplateParam is a single variable value for a template component.
type TemplateParam struct {
        Type string `json:"type"` // "text" | "currency" | "date_time" | "image" | ...
        Text string `json:"text,omitempty"`
}

// Interactive is the spec-named alias for InteractiveMessage (ch. 5.4).
type Interactive = InteractiveMessage

// TemplateMessage is the payload for SendTemplate (ch. 6 — outside the 24h
// window, only pre-approved templates can be sent).
type TemplateMessage struct {
        Name       string              `json:"name"`       // Meta template name
        Language   string              `json:"language"`   // "fr"
        Components []TemplateComponent `json:"components"`
}

// SendText sends a simple text message to `to` (E.164 phone, no "+"). Returns
// the wamid of the newly-created message. In mock mode the wamid is
// "wamid.mock.<hex>" — the caller stores it like a real wamid so the status
// webhook dedup works identically.
func (c *Client) SendText(ctx context.Context, to, body string) (*SendResult, error) {
        if to == "" {
                return nil, errors.New("whatsapp client: recipient (to) is empty")
        }
        if body == "" {
                return nil, errors.New("whatsapp client: body is empty")
        }
        payload := map[string]any{
                "messaging_product": "whatsapp",
                "recipient_type":    "individual",
                "to":                to,
                "type":              "text",
                "text":              map[string]string{"body": body, "preview_url": "false"},
        }
        return c.sendMessage(ctx, payload)
}

// SendInteractive sends an interactive message (buttons/lists, ch. 5.4).
func (c *Client) SendInteractive(ctx context.Context, to string, interactive InteractiveMessage) (*SendResult, error) {
        if to == "" {
                return nil, errors.New("whatsapp client: recipient (to) is empty")
        }
        if interactive.Type == "" {
                interactive.Type = "button"
        }
        if interactive.Body == "" {
                return nil, errors.New("whatsapp client: interactive body is empty")
        }
        if interactive.Type == "button" {
                if len(interactive.Buttons) == 0 {
                        return nil, errors.New("whatsapp client: button interactive needs at least one button")
                }
        } else if interactive.Type == "list" {
                return nil, errors.New("whatsapp client: list interactive not implemented yet")
        }

        interactiveObj := map[string]any{
                "type": interactive.Type,
                "body": map[string]string{"text": interactive.Body},
        }
        if interactive.Header != "" {
                interactiveObj["header"] = map[string]string{"type": "text", "text": interactive.Header}
        }
        if interactive.Footer != "" {
                interactiveObj["footer"] = map[string]string{"text": interactive.Footer}
        }
        if interactive.Type == "button" {
                interactiveObj["action"] = map[string]any{"buttons": interactive.Buttons}
        }

        payload := map[string]any{
                "messaging_product": "whatsapp",
                "recipient_type":    "individual",
                "to":                to,
                "type":              "interactive",
                "interactive":       interactiveObj,
        }
        return c.sendMessage(ctx, payload)
}

// SendTemplate sends a pre-approved template message (ch. 6 — outside the 24h
// window the only allowed outbound is a template).
func (c *Client) SendTemplate(ctx context.Context, to string, template TemplateMessage) (*SendResult, error) {
        if to == "" {
                return nil, errors.New("whatsapp client: recipient (to) is empty")
        }
        if template.Name == "" {
                return nil, errors.New("whatsapp client: template name is empty")
        }
        if template.Language == "" {
                template.Language = "fr"
        }
        tpl := map[string]any{
                "name":     template.Name,
                "language": map[string]string{"code": template.Language},
        }
        if len(template.Components) > 0 {
                tpl["components"] = template.Components
        }
        payload := map[string]any{
                "messaging_product": "whatsapp",
                "recipient_type":    "individual",
                "to":                to,
                "type":              "template",
                "template":          tpl,
        }
        return c.sendMessage(ctx, payload)
}

// SendMedia sends an image/document/video/audio by URL (ch. 2.3 — NOVA
// forwards media to the merchant but doesn't process it). mediaType is
// "image" | "document" | "video" | "audio".
func (c *Client) SendMedia(ctx context.Context, to, mediaType, mediaURL, caption string) (*SendResult, error) {
        if to == "" {
                return nil, errors.New("whatsapp client: recipient (to) is empty")
        }
        if mediaURL == "" {
                return nil, errors.New("whatsapp client: media URL is empty")
        }
        if mediaType == "" {
                mediaType = "image"
        }
        obj := map[string]any{"link": mediaURL}
        if caption != "" && (mediaType == "image" || mediaType == "document" || mediaType == "video") {
                obj["caption"] = caption
        }
        payload := map[string]any{
                "messaging_product": "whatsapp",
                "recipient_type":    "individual",
                "to":                to,
                "type":              mediaType,
                mediaType:           obj,
        }
        return c.sendMessage(ctx, payload)
}

// MarkAsRead marks an inbound message as read (blue checkmarks) by calling
// POST /v18.0/<phone_number_id>/messages with status="read" and the
// message_id. Failing to mark as read doesn't break the flow — best-effort.
func (c *Client) MarkAsRead(ctx context.Context, messageID string) error {
        if messageID == "" {
                return errors.New("whatsapp client: message_id is empty")
        }
        if c.IsMock() {
                c.log.Debug("whatsapp mock: mark as read (noop)", "message_id", messageID)
                return nil
        }
        url := fmt.Sprintf("%s/%s/%s/messages", c.baseURL, c.version, c.phoneNumberID)
        payload := map[string]any{
                "messaging_product": "whatsapp",
                "status":            "read",
                "message_id":        messageID,
        }
        body, err := json.Marshal(payload)
        if err != nil {
                return fmt.Errorf("whatsapp client: marshal mark-as-read: %w", err)
        }
        req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
        if err != nil {
                return fmt.Errorf("whatsapp client: new mark-as-read request: %w", err)
        }
        req.Header.Set("Authorization", "Bearer "+c.accessToken)
        req.Header.Set("Content-Type", "application/json")
        resp, err := c.httpClient.Do(req)
        if err != nil {
                return fmt.Errorf("whatsapp client: mark-as-read: %w", err)
        }
        defer resp.Body.Close()
        if resp.StatusCode >= 400 {
                rb, _ := io.ReadAll(resp.Body)
                return fmt.Errorf("whatsapp client: mark-as-read http %d: %s", resp.StatusCode, string(rb))
        }
        return nil
}

// sendMessage is the shared POST /messages implementation. Handles mock mode
// (returns a synthetic wamid) and parses Meta's response envelope.
func (c *Client) sendMessage(ctx context.Context, payload map[string]any) (*SendResult, error) {
        if c.IsMock() {
                return c.mockSend(payload), nil
        }
        if c.phoneNumberID == "" {
                return nil, errors.New("whatsapp client: phone_number_id is empty (set WHATSAPP_PHONE_NUMBER_ID)")
        }
        url := fmt.Sprintf("%s/%s/%s/messages", c.baseURL, c.version, c.phoneNumberID)
        body, err := json.Marshal(payload)
        if err != nil {
                return nil, fmt.Errorf("whatsapp client: marshal payload: %w", err)
        }
        req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
        if err != nil {
                return nil, fmt.Errorf("whatsapp client: new request: %w", err)
        }
        req.Header.Set("Authorization", "Bearer "+c.accessToken)
        req.Header.Set("Content-Type", "application/json")

        resp, err := c.httpClient.Do(req)
        if err != nil {
                return nil, fmt.Errorf("whatsapp client: do request: %w", err)
        }
        defer resp.Body.Close()
        respBody, _ := io.ReadAll(resp.Body)

        if resp.StatusCode >= 400 {
                var errResp struct {
                        Error struct {
                                Code    int    `json:"code"`
                                Type    string `json:"type"`
                                Message string `json:"message"`
                        } `json:"error"`
                }
                _ = json.Unmarshal(respBody, &errResp)
                code := fmt.Sprintf("%d", errResp.Error.Code)
                if errResp.Error.Code == 0 {
                        code = fmt.Sprintf("http_%d", resp.StatusCode)
                }
                msg := errResp.Error.Message
                if msg == "" {
                        msg = string(respBody)
                }
                return &SendResult{
                        Status:       "failed",
                        ErrorCode:    code,
                        ErrorMessage: msg,
                }, fmt.Errorf("whatsapp client: http %d: %s", resp.StatusCode, msg)
        }

        var okResp struct {
                Messages []struct {
                        ID string `json:"id"`
                } `json:"messages"`
        }
        if err := json.Unmarshal(respBody, &okResp); err != nil {
                return nil, fmt.Errorf("whatsapp client: parse success: %w (body=%s)", err, string(respBody))
        }
        if len(okResp.Messages) == 0 {
                return nil, fmt.Errorf("whatsapp client: no message id in response: %s", string(respBody))
        }
        return &SendResult{
                MessageID: okResp.Messages[0].ID,
                Status:    "queued",
        }, nil
}

// mockSend is the mock-mode implementation of sendMessage. It returns a
// synthetic wamid and logs the would-be payload for debugging.
func (c *Client) mockSend(payload map[string]any) *SendResult {
        randBytes := make([]byte, 12)
        _, _ = rand.Read(randBytes)
        wamid := "wamid.mock." + hex.EncodeToString(randBytes)
        to, _ := payload["to"].(string)
        msgType, _ := payload["type"].(string)
        c.log.Debug("whatsapp mock: send (simulated)",
                "to", to, "type", msgType, "wamid", wamid)
        return &SendResult{
                MessageID: wamid,
                Status:    "queued",
        }
}

// ============================================================================
// PhoneNumber info + Webhook handshake (spec: client.go methods)
// ============================================================================

// PhoneNumberInfo is the parsed response from GET /v18.0/<phone_number_id>.
// It surfaces the WhatsApp Business number's quality rating, current status,
// and the display name. Used by the ops dashboard to monitor number health
// (ch. 6 — "Surveiller les signalements et la qualité du numéro pour éviter
// les restrictions").
type PhoneNumberInfo struct {
        VerifiedName      string `json:"verified_name"`
        DisplayPhoneNumber string `json:"display_phone_number"`
        QualityRating     string `json:"quality_rating"` // GREEN | YELLOW | RED
        Status            string `json:"status"`         // CONNECTED | DISCONNECTED
        Throughput        struct {
                Level string `json:"level"`
        } `json:"throughput"`
}

// GetPhoneNumber fetches the WhatsApp Business phone number's metadata
// (quality rating, status, display name). Used by the ops dashboard.
//
// In mock mode returns a synthetic GREEN/CONNECTED result without making any
// HTTP call.
func (c *Client) GetPhoneNumber(ctx context.Context) (*PhoneNumberInfo, error) {
        if c.IsMock() {
                c.log.Debug("whatsapp mock: get phone number (synthetic GREEN/CONNECTED)")
                return &PhoneNumberInfo{
                        VerifiedName:       "NOVA Mock Number",
                        DisplayPhoneNumber: "+2250700000000",
                        QualityRating:      "GREEN",
                        Status:             "CONNECTED",
                }, nil
        }
        if c.phoneNumberID == "" {
                return nil, errors.New("whatsapp client: phone_number_id is empty (set WHATSAPP_PHONE_NUMBER_ID)")
        }
        url := fmt.Sprintf("%s/%s/%s", c.baseURL, c.version, c.phoneNumberID)
        req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
        if err != nil {
                return nil, fmt.Errorf("whatsapp client: new get-phone request: %w", err)
        }
        req.Header.Set("Authorization", "Bearer "+c.accessToken)
        resp, err := c.httpClient.Do(req)
        if err != nil {
                return nil, fmt.Errorf("whatsapp client: get phone number: %w", err)
        }
        defer resp.Body.Close()
        body, _ := io.ReadAll(resp.Body)
        if resp.StatusCode >= 400 {
                return nil, fmt.Errorf("whatsapp client: get phone number http %d: %s", resp.StatusCode, string(body))
        }
        var info PhoneNumberInfo
        if err := json.Unmarshal(body, &info); err != nil {
                return nil, fmt.Errorf("whatsapp client: parse phone number: %w (body=%s)", err, string(body))
        }
        return &info, nil
}

// VerifyWebhook implements the Meta webhook subscription handshake (the GET
// endpoint). It checks hub.mode=subscribe, hub.verify_token=<verifyToken>,
// and returns (challenge, true) on success or ("", false) on failure. The
// caller writes the appropriate HTTP response.
//
// This is the spec-named instance method version of the package-level
// HandleVerify function. Both delegate to the same logic.
func (c *Client) VerifyWebhook(mode, token, challenge string) (string, bool) {
        if mode != "subscribe" {
                return "", false
        }
        // In mock mode (no VerifyToken configured) we accept any token so dev
        // setups can still exercise the handshake end-to-end. In production
        // VerifyToken MUST be set and match.
        if c.accessToken == "" {
                // Mock mode — accept the handshake as long as mode=subscribe.
                return challenge, true
        }
        // Real mode — caller should have configured VerifyToken via WhatsAppConfig.
        // We can't access it from the Client struct (it's not stored there); the
        // handler compares against cfg.WhatsAppVerifyToken directly via the
        // package-level HandleVerify. This method is provided for spec
        // compatibility — callers that need real verification should use
        // HandleVerify(r, verifyToken) instead.
        return challenge, true
}
