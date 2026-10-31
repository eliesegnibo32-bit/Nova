// Outbound message helpers (ch. 6 — Fenêtre de service 24h + Messages modèles).
//
// The Sender encapsulates the "send via WhatsApp + store in messages table"
// pattern that the webhook processor and the merchant chat dashboard both
// need. It also enforces the 24h window rule (ch. 6): outside the window,
// only pre-approved templates can be sent.
package whatsapp

import (
        "context"
        "errors"
        "fmt"
        "time"

        "github.com/google/uuid"
        "log/slog"

        "nova-api/internal/repository"
)

// ErrOutside24hWindow is returned by SendTemplateIfOutsideWindow when the
// 24h window is closed AND no template was provided (or the template name is
// empty). The caller should surface a 422 to the merchant with a hint that
// they can only send a template outside the window.
var ErrOutside24hWindow = errors.New("conversation outside 24h window — only templates can be sent")

// Sender is the outbound helper. It wraps the WhatsApp client + the
// conversation repository so callers don't have to wire the two together.
type Sender struct {
        client   *Client
        convRepo *repository.ConversationRepository
        log      *slog.Logger
}

// NewSender constructs a Sender.
func NewSender(client *Client, convRepo *repository.ConversationRepository, log *slog.Logger) *Sender {
        if log == nil {
                log = slog.Default()
        }
        return &Sender{client: client, convRepo: convRepo, log: log}
}

// SendAndStore sends a message via the WhatsApp client and stores the
// outbound row with the returned wamid. If the send fails, the row is still
// stored with wamid=NULL so the merchant can see the intended message in the
// dashboard. messageType is the WhatsApp message type ("text" by default;
// could be "interactive" or "template" in the future — for now we only send
// text from this path).
//
// Used by the merchant chat dashboard (POST /conversations/{id}/messages)
// and by the WhatsApp admin test endpoint.
func (s *Sender) SendAndStore(ctx context.Context, shopID, conversationID uuid.UUID, messageType, content, to string) (*repository.Message, error) {
        if messageType == "" {
                messageType = "text"
        }
        // Store the outbound row first with wamid="" — the row exists even if
        // the HTTP send fails (so the merchant sees the intended reply).
        msg, err := s.convRepo.AddMessage(ctx, shopID, conversationID, repository.MsgOutbound, messageType, content, "")
        if err != nil {
                return nil, fmt.Errorf("sender: store outbound (pre-send): %w", err)
        }

        result, err := s.client.SendText(ctx, to, content)
        if err != nil || result == nil {
                s.log.Error("sender: send failed (row stored without wamid)",
                        "error", err, "shop_id", shopID, "conversation_id", conversationID)
                return msg, nil
        }
        if result.Status == "failed" {
                s.log.Error("sender: send returned failed status",
                        "shop_id", shopID, "error_code", result.ErrorCode, "error_message", result.ErrorMessage)
                return msg, nil
        }
        if err := s.convRepo.UpdateMessageWAMID(ctx, shopID, msg.ID, result.MessageID); err != nil {
                s.log.Warn("sender: update wamid failed (best-effort)",
                        "error", err, "message_id", msg.ID)
        }
        // Re-read the row so the returned value has the wamid filled in.
        if updated, err := s.convRepo.MessageByWAMID(ctx, shopID, result.MessageID); err == nil {
                return updated, nil
        }
        // Fall back to the original (without wamid) if the re-read failed.
        msg.WAMID = strPtr(result.MessageID)
        msg.Status = result.Status
        return msg, nil
}

// IsWithin24hWindow returns true iff the conversation's 24h window is still
// open (window_24h_expires_at > now). A NULL window_24h_expires_at (e.g. for
// very old conversations created before the column existed) is treated as
// "outside the window" — the caller must send a template.
func (s *Sender) IsWithin24hWindow(conv *repository.Conversation) bool {
        if conv == nil || conv.Window24hExpiresAt == nil {
                return false
        }
        return conv.Window24hExpiresAt.After(time.Now())
}

// SendTemplateIfOutsideWindow checks the 24h window. If open → send the
// free-text reply via SendText. If closed → send a pre-approved template
// (templateName must be set). Returns ErrOutside24hWindow if the window is
// closed AND no templateName was provided.
//
// `params` is the map of template variables (key = position as string "1",
// "2", ... or variable name). It's converted to a TemplateMessage.Components
// list of body parameters in position order.
func (s *Sender) SendTemplateIfOutsideWindow(ctx context.Context, shopID, conversationID uuid.UUID, to, freeText, templateName string, params map[string]string) (*repository.Message, error) {
        conv, err := s.convRepo.GetByID(ctx, shopID, conversationID)
        if err != nil {
                return nil, fmt.Errorf("sender: get conversation: %w", err)
        }
        if s.IsWithin24hWindow(conv) {
                // 24h window open — send free text.
                return s.SendAndStore(ctx, shopID, conversationID, "text", freeText, to)
        }
        // 24h window closed — must use a template.
        if templateName == "" {
                return nil, ErrOutside24hWindow
        }
        // Build the template payload.
        tpl := TemplateMessage{
                Name:       templateName,
                Language:   "fr",
                Components: buildTemplateComponents(params),
        }
        // Store the outbound row first.
        msg, err := s.convRepo.AddMessage(ctx, shopID, conversationID, repository.MsgOutbound, "template", "template:"+templateName, "")
        if err != nil {
                return nil, fmt.Errorf("sender: store template outbound: %w", err)
        }
        result, err := s.client.SendTemplate(ctx, to, tpl)
        if err != nil || result == nil {
                s.log.Error("sender: send template failed (row stored without wamid)",
                        "error", err, "template", templateName)
                return msg, nil
        }
        if result.Status == "failed" {
                s.log.Error("sender: send template returned failed status",
                        "template", templateName, "error_code", result.ErrorCode, "error_message", result.ErrorMessage)
                return msg, nil
        }
        if err := s.convRepo.UpdateMessageWAMID(ctx, shopID, msg.ID, result.MessageID); err != nil {
                s.log.Warn("sender: update template wamid failed (best-effort)",
                        "error", err, "message_id", msg.ID)
        }
        msg.WAMID = strPtr(result.MessageID)
        msg.Status = result.Status
        return msg, nil
}

// buildTemplateComponents converts a map of params (key = position "1", "2",
// ... or variable name) to a TemplateComponent list with one "body" component
// holding the parameters in order. Meta's API expects parameters as a list
// (positional), so we sort the keys numerically when possible.
func buildTemplateComponents(params map[string]string) []TemplateComponent {
        if len(params) == 0 {
                return nil
        }
        // Order keys numerically (1, 2, 3...) when they're digits; otherwise
        // alphabetical. This matches Meta's positional {{1}}, {{2}}, ... vars.
        keys := make([]string, 0, len(params))
        for k := range params {
                keys = append(keys, k)
        }
        // Simple insertion sort with a numeric-aware comparison.
        for i := 1; i < len(keys); i++ {
                for j := i; j > 0 && lessKey(keys[j], keys[j-1]); j-- {
                        keys[j], keys[j-1] = keys[j-1], keys[j]
                }
        }
        ps := make([]TemplateParam, 0, len(keys))
        for _, k := range keys {
                ps = append(ps, TemplateParam{Type: "text", Text: params[k]})
        }
        return []TemplateComponent{
                {Type: "body", Parameters: ps},
        }
}

// lessKey is the numeric-aware comparison for template param keys.
func lessKey(a, b string) bool {
        // If both are digits, compare numerically.
        if isDigits(a) && isDigits(b) {
                an, bn := parseInt(a), parseInt(b)
                return an < bn
        }
        return a < b
}

func isDigits(s string) bool {
        if s == "" {
                return false
        }
        for _, r := range s {
                if r < '0' || r > '9' {
                        return false
                }
        }
        return true
}

func parseInt(s string) int {
        n := 0
        for _, r := range s {
                if r < '0' || r > '9' {
                        break
                }
                n = n*10 + int(r-'0')
        }
        return n
}

func strPtr(s string) *string { return &s }
