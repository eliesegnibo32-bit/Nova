// Inbound message + status update processor (ch. 6 — Canal WhatsApp).
//
// The processor is the bridge between the Meta webhook envelope (parsed in
// webhook.go) and NOVA's domain layer (AIService + repositories). It is run
// inside a goroutine launched by the webhook handler — the HTTP response is
// already 200 OK by the time the processor starts.
//
// Critical requirements (ch. 6 — Fiabilité du webhook):
//   - Deduplication by wamid (Meta retries webhook delivery).
//   - 24-hour window updated on every inbound message.
//   - Consent ("stop") applied immediately.
//   - Media (audio/image) NOT processed by the AI — polite reply + escalation.
//   - Error isolation: a failure on one message MUST NOT affect others.
package whatsapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"

	"nova-api/internal/models"
	"nova-api/internal/repository"
)

// AIProcessor is the subset of services.AIService used by the webhook
// processor. We define it here (rather than importing services.AIService
// directly) to break the import cycle services ↔ whatsapp: the services
// package imports whatsapp for the WhatsAppService facade, and the whatsapp
// processor needs to call back into the AI service.
//
// The concrete *services.AIService satisfies this interface (it implements
// ProcessStoredInboundMessage with the exact signature below).
type AIProcessor interface {
	ProcessStoredInboundMessage(
		ctx context.Context, shopID, conversationID, customerID uuid.UUID,
		customerName, customerPhone, message, msgType string,
		userID uuid.UUID, userRole string,
	) (*AIProcessorResult, error)
}

// SubscriptionChecker is the optional subscription-status hook (Task 10 —
// ch. 7.3). When wired, the processor consults it before calling the AI:
// if the shop's subscription is suspended or terminated, the AI call is
// skipped and a neutral auto-reply ("Ce service est temporairement
// indisponible.") is sent instead. The concrete impl is
// *services.SubscriptionService (via its IsShopServiceActive method).
type SubscriptionChecker interface {
	IsShopServiceActive(ctx context.Context, shopID uuid.UUID) (bool, error)
}

// NeutralInactiveReply is the auto-reply sent when the shop's subscription
// is suspended or terminated (ch. 7.3).
const NeutralInactiveReply = "Ce service est temporairement indisponible. Un commerçant vous répondra dès que possible."

// AIProcessorResult is the result returned by AIProcessor.ProcessStoredInboundMessage.
// It mirrors the subset of services.ProcessResult that the webhook processor
// actually uses. We re-declare it here to break the import cycle (the services
// package's ProcessResult would close the cycle if we imported it).
type AIProcessorResult struct {
	Reply           string
	ConversationID  string
	CustomerID      string
	TokensIn        int
	TokensOut       int
	Model           string
	LatencyMs       int
	CostEstimate    float64
	GuardrailPassed bool
	Escalated       bool
	Regenerated     bool
	Processed       bool
}

// Processor is the long-running webhook event processor. One instance is
// shared across all webhook deliveries (it's stateless — per-event context
// is built inside ProcessWebhook).
type Processor struct {
	client       *Client
	shopRepo     *repository.ShopRepository
	customerRepo *repository.CustomerRepository
	convRepo     *repository.ConversationRepository
	aiService    AIProcessor
	// subChecker is OPTIONAL (Task 10 — ch. 7.3). When nil, the processor
	// calls the AI for every inbound message (pre-Task-10 behavior). When
	// set, the processor consults it before the AI call: if the shop's
	// subscription is suspended or terminated, a neutral auto-reply is sent
	// instead and the AI is skipped (cost saving + ch. 7.3 compliance).
	subChecker SubscriptionChecker
	pool       *pgxpool.Pool
	log        *slog.Logger
}

// NewProcessor constructs a Processor. aiService may be nil in degraded mode
// (the processor will then store inbound messages but skip the AI call and
// log a warning — useful when bringing up the API without an LLM key).
//
// aiService is typed as the AIProcessor interface (not *services.AIService)
// to break the import cycle services ↔ whatsapp. The concrete
// *services.AIService satisfies this interface.
//
// subChecker is OPTIONAL (Task 10). Pass nil to skip the subscription check.
func NewProcessor(
	client *Client,
	shopRepo *repository.ShopRepository,
	customerRepo *repository.CustomerRepository,
	convRepo *repository.ConversationRepository,
	aiService AIProcessor,
	subChecker SubscriptionChecker,
	pool *pgxpool.Pool,
	log *slog.Logger,
) *Processor {
	if log == nil {
		log = slog.Default()
	}
	return &Processor{
		client:       client,
		shopRepo:     shopRepo,
		customerRepo: customerRepo,
		convRepo:     convRepo,
		aiService:    aiService,
		subChecker:   subChecker,
		pool:         pool,
		log:          log,
	}
}

// ProcessWebhook is the main entry point. For each entry/change:
//  1. Find the shop by the recipient phone number (Metadata.DisplayPhoneNumber).
//     If not found, log and skip (no shop for this number).
//  2. For each inbound `message`: call processInboundMessage.
//  3. For each `status`: call processStatusUpdate.
//
// All errors are logged but never returned to the caller — the webhook has
// already been ACKed with 200 OK. Returning an error would be misleading.
// Each message is processed independently (error isolation).
func (p *Processor) ProcessWebhook(ctx context.Context, event *WebhookEvent) error {
	if event == nil {
		return nil
	}
	for _, entry := range event.Entry {
		for _, change := range entry.Changes {
			if change.Field != "messages" {
				continue
			}
			// Look up the shop by the recipient phone number.
			displayPhone := change.Value.Metadata.DisplayPhoneNumber
			shop, err := p.shopRepo.GetByWhatsAppNumber(ctx, displayPhone)
			if err != nil {
				if errors.Is(err, repository.ErrNotFound) {
					p.log.Warn("whatsapp processor: no shop for recipient phone — skipping",
						"display_phone", displayPhone)
					continue
				}
				p.log.Error("whatsapp processor: lookup shop failed",
					"error", err, "display_phone", displayPhone)
				continue
			}

			// Process each inbound message (independent — error on one
			// doesn't affect the next).
			for _, msg := range change.Value.Messages {
				// Per-message context with a 30s timeout so a slow
				// AI call can't block the whole batch.
				msgCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
				if err := p.processInboundMessage(msgCtx, shop, change.Value.Contacts, msg); err != nil {
					p.log.Error("whatsapp processor: process inbound message failed",
						"error", err,
						"shop_id", shop.ID,
						"wamid", msg.ID,
						"from", msg.From,
					)
				}
				cancel()
			}

			// Process each status update (independent).
			for _, st := range change.Value.Statuses {
				stCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				if err := p.processStatusUpdate(stCtx, shop, st); err != nil {
					p.log.Warn("whatsapp processor: process status update failed",
						"error", err,
						"shop_id", shop.ID,
						"wamid", st.ID,
						"status", st.Status,
					)
				}
				cancel()
			}
		}
	}
	return nil
}

// processInboundMessage handles one inbound WhatsApp message from a customer.
//
// Flow (ch. 6 — Fenêtre de service 24h + Fiabilité):
//  1. Dedup by wamid — skip if a message with this wamid already exists.
//  2. Resolve the contact → customer (get-or-create).
//  3. Resolve / create the conversation for this customer phone.
//  4. Update the 24h window (window_24h_expires_at = now() + 24h).
//  5. Persist the inbound message (direction='inbound', wamid, type, content).
//  6. Handle consent: if the message text is "stop", set consent_marketing=false.
//  7. Map the WhatsApp message type to the AI engine message type:
//     text → "text"; image → "image"; audio → "audio"; video/document → "image";
//     interactive/button → "text" (extract the reply text).
//  8. Call aiService.ProcessStoredInboundMessage to get the AI reply.
//  9. Send the reply via the WhatsApp client (mock mode: wamid.mock.*).
//
// 10. Persist the outbound message with the returned wamid.
//
// Errors are isolated: a failure at any step is logged and the function
// returns the error (the caller logs it and continues to the next message).
func (p *Processor) processInboundMessage(ctx context.Context, shop *models.Shop, contacts []WhatsAppContact, msg WhatsAppMessage) error {
	if msg.ID == "" {
		return errors.New("whatsapp processor: inbound message has no wamid")
	}

	// 1. Dedup by wamid — Meta retries webhook delivery.
	exists, err := p.convRepo.WAMIDExists(ctx, shop.ID, msg.ID)
	if err != nil {
		return fmt.Errorf("dedup check: %w", err)
	}
	if exists {
		p.log.Info("whatsapp processor: duplicate wamid — skipping",
			"shop_id", shop.ID, "wamid", msg.ID)
		return nil
	}

	// 2. Resolve contact → customer. Meta sends the contact info along
	//    with the message; if there are multiple contacts, pick the one
	//    matching msg.From.
	var contactName string
	var phoneE164 string
	for _, c := range contacts {
		if c.WaID == msg.From || strings.HasSuffix(c.WaID, msg.From) {
			contactName = c.Profile.Name
			break
		}
	}
	if contactName == "" && len(contacts) > 0 {
		contactName = contacts[0].Profile.Name
	}
	phoneE164 = normalizeCustomerPhone(msg.From)
	if phoneE164 == "" {
		return fmt.Errorf("whatsapp processor: cannot parse phone from %q", msg.From)
	}

	cust, err := p.customerRepo.GetOrCreateByPhone(ctx, shop.ID, phoneE164, contactName)
	if err != nil {
		return fmt.Errorf("get or create customer: %w", err)
	}

	// 3. Resolve / create conversation.
	conv, err := p.convRepo.GetOrCreateByCustomerPhone(ctx, shop.ID, phoneE164, "whatsapp")
	if err != nil {
		return fmt.Errorf("get or create conversation: %w", err)
	}

	// 4. Update the 24h window (best-effort — already set by GetOrCreate for
	//    new conversations, but for existing ones we need to extend it).
	_ = p.convRepo.UpdateWindow24h(ctx, shop.ID, conv.ID, time.Now().Add(24*time.Hour))

	// 5. Persist the inbound message with the wamid.
	aiType, content := p.extractContentAndType(msg)
	if _, err := p.convRepo.AddMessage(ctx, shop.ID, conv.ID, repository.MsgInbound, aiType, content, msg.ID); err != nil {
		return fmt.Errorf("persist inbound message: %w", err)
	}

	// 6. Consent handling: "stop" opts out of marketing immediately (ch. 6).
	if isStopMessage(content) {
		if err := p.customerRepo.SetConsent(ctx, shop.ID, cust.ID, false, "whatsapp_stop_keyword"); err != nil {
			p.log.Warn("whatsapp processor: set consent (stop) failed", "error", err, "customer_id", cust.ID)
		} else {
			p.log.Info("whatsapp processor: marketing consent withdrawn (stop)",
				"shop_id", shop.ID, "customer_id", cust.ID)
		}
		// Acknowledge the opt-out with a polite message and exit — no AI needed.
		ackText := "Vous êtes désabonné des messages marketing de cette boutique. Vous pouvez continuer à discuter avec le service client à tout moment."
		if err := p.sendAndStoreReply(ctx, shop.ID, conv.ID, phoneE164, ackText); err != nil {
			p.log.Warn("whatsapp processor: send stop ack failed", "error", err)
		}
		return nil
	}

	// 7-8. Call the AI engine. We use a system-level "owner" user since
	//      the inbound webhook has no human user attached. The engine
	//      uses this for RLS on its tool calls.
	if p.aiService == nil {
		p.log.Warn("whatsapp processor: AI service not initialized — message stored, no reply",
			"shop_id", shop.ID, "wamid", msg.ID)
		return nil
	}
	// 7a. Subscription check (Task 10 — ch. 7.3): if the shop's
	//     subscription is suspended or terminated, skip the AI call and
	//     send a neutral auto-reply instead.
	if p.subChecker != nil {
		active, err := p.subChecker.IsShopServiceActive(ctx, shop.ID)
		if err != nil {
			p.log.Warn("whatsapp processor: subscription check failed (continuing)",
				"error", err, "shop_id", shop.ID)
		} else if !active {
			p.log.Info("whatsapp processor: shop not active — neutral auto-reply",
				"shop_id", shop.ID, "conversation_id", conv.ID)
			if err := p.sendAndStoreReply(ctx, shop.ID, conv.ID, phoneE164, NeutralInactiveReply); err != nil {
				p.log.Warn("whatsapp processor: send neutral reply failed", "error", err)
			}
			return nil
		}
	}
	result, err := p.aiService.ProcessStoredInboundMessage(
		ctx, shop.ID, conv.ID, cust.ID, contactName, phoneE164, content, aiType,
		uuid.Nil, string(models.RoleOwner),
	)
	if err != nil {
		return fmt.Errorf("ai process: %w", err)
	}
	if !result.Processed {
		// Conversation is in 'human' state — the merchant will reply
		// manually. Nothing to send.
		p.log.Info("whatsapp processor: conversation in human mode — no AI reply",
			"shop_id", shop.ID, "conversation_id", conv.ID)
		return nil
	}
	if result.Reply == "" {
		p.log.Warn("whatsapp processor: AI returned empty reply",
			"shop_id", shop.ID, "conversation_id", conv.ID)
		return nil
	}

	// 9-10. Send the reply via WhatsApp and store the outbound row with
	//       the returned wamid.
	if err := p.sendAndStoreReply(ctx, shop.ID, conv.ID, phoneE164, result.Reply); err != nil {
		return fmt.Errorf("send and store reply: %w", err)
	}

	// Best-effort: mark the inbound message as read (blue checkmarks).
	// We don't propagate the error — failing to mark as read doesn't
	// break the flow.
	if err := p.client.MarkAsRead(ctx, msg.ID); err != nil {
		p.log.Debug("whatsapp processor: mark as read failed (best-effort)", "error", err)
	}

	p.log.Info("whatsapp processor: inbound message processed",
		"shop_id", shop.ID,
		"conversation_id", conv.ID,
		"wamid", msg.ID,
		"from", phoneE164,
		"type", aiType,
		"reply_sent", true,
		"tokens_in", result.TokensIn,
		"tokens_out", result.TokensOut,
		"guardrail_passed", result.GuardrailPassed,
		"escalated", result.Escalated,
	)
	return nil
}

// processStatusUpdate updates the delivery status of an outbound message
// (sent / delivered / read / failed). The status is keyed by the wamid of OUR
// outbound message (the one we got back from the Send API).
//
// On 'failed' we log the error code+title so the ops team can investigate.
// On 'delivered' / 'read' we update the corresponding timestamp.
func (p *Processor) processStatusUpdate(ctx context.Context, shop *models.Shop, st WhatsAppStatus) error {
	if st.ID == "" {
		return errors.New("whatsapp processor: status update has no wamid")
	}
	if err := p.convRepo.UpdateMessageStatus(ctx, shop.ID, st.ID, st.Status); err != nil {
		if errors.Is(err, repository.ErrMessageNotFound) {
			// Status update for a message we don't track (e.g.
			// message sent from the Meta App directly, or a
			// template notification). Silently ignore.
			p.log.Debug("whatsapp processor: status for untracked message — skipping",
				"shop_id", shop.ID, "wamid", st.ID, "status", st.Status)
			return nil
		}
		return fmt.Errorf("update message status: %w", err)
	}
	if st.Status == "failed" && len(st.Errors) > 0 {
		p.log.Error("whatsapp processor: outbound message failed",
			"shop_id", shop.ID,
			"wamid", st.ID,
			"error_code", st.Errors[0].Code,
			"error_title", st.Errors[0].Title,
			"error_message", st.Errors[0].Message,
		)
	} else {
		p.log.Info("whatsapp processor: outbound message status updated",
			"shop_id", shop.ID,
			"wamid", st.ID,
			"status", st.Status,
		)
	}
	return nil
}

// extractContentAndType maps a WhatsApp inbound message to the (aiType, content)
// pair expected by the AI engine.
//
// Per ch. 2.3 (Notes vocales, photos): audio and image are NOT processed by
// the AI. We still return ("audio"|"image", placeholder) so the engine's
// handleNonTextMessage can intercept and respond with the polite escalation
// reply. The actual media metadata is stored as JSON in the content column.
func (p *Processor) extractContentAndType(msg WhatsAppMessage) (string, string) {
	switch msg.Type {
	case "text":
		if msg.Text != nil {
			return "text", msg.Text.Body
		}
		return "text", ""
	case "image":
		// ch. 2.3: image is escalated. Store the media metadata as JSON
		// in content; the AI engine sees msgType="image" and intercepts.
		return "image", serializeMediaMeta(msg.Image, "image")
	case "audio":
		// ch. 2.3: audio is escalated.
		return "audio", serializeMediaMeta(msg.Audio, "audio")
	case "video":
		// Treat as media (escalation).
		return "image", serializeMediaMeta(msg.Video, "video")
	case "document":
		return "image", serializeMediaMeta(msg.Document, "document")
	case "interactive":
		if msg.Interactive != nil {
			if msg.Interactive.ButtonReply != nil {
				// Use the button title as the customer's "message".
				return "text", msg.Interactive.ButtonReply.Title
			}
			if msg.Interactive.ListReply != nil {
				return "text", msg.Interactive.ListReply.Title
			}
		}
		return "text", ""
	case "button":
		if msg.Button != nil {
			if msg.Button.Text != "" {
				return "text", msg.Button.Text
			}
			return "text", msg.Button.Payload
		}
		return "text", ""
	default:
		// Unsupported message type — log and treat as text with a
		// placeholder so the AI can respond gracefully.
		return "text", fmt.Sprintf("[message type %q non supporté]", msg.Type)
	}
}

// sendAndStoreReply sends the reply text via the WhatsApp client and stores
// the outbound message row with the returned wamid. If the send fails, the
// message is still stored (with wamid=NULL) so the merchant can see the
// intended reply in the dashboard; the failure is logged.
func (p *Processor) sendAndStoreReply(ctx context.Context, shopID, conversationID uuid.UUID, to, replyText string) error {
	// Store the outbound message with wamid="" first. This way the row
	// exists even if the send fails (so the merchant can see the intended
	// reply in the dashboard).
	msg, err := p.convRepo.AddMessage(ctx, shopID, conversationID, repository.MsgOutbound, "text", replyText, "")
	if err != nil {
		return fmt.Errorf("store outbound (pre-send): %w", err)
	}
	// Send via the WhatsApp Cloud API (or mock).
	result, err := p.client.SendText(ctx, to, replyText)
	if err != nil || result == nil {
		p.log.Error("whatsapp processor: send reply failed (message stored without wamid)",
			"error", err,
			"shop_id", shopID,
			"conversation_id", conversationID,
			"message_id", msg.ID,
		)
		return nil // don't fail the whole flow — message is stored
	}
	if result.Status == "failed" {
		p.log.Error("whatsapp processor: send reply returned failed status",
			"shop_id", shopID,
			"conversation_id", conversationID,
			"message_id", msg.ID,
			"error_code", result.ErrorCode,
			"error_message", result.ErrorMessage,
		)
		return nil
	}
	// Fill in the wamid on the stored row.
	if err := p.convRepo.UpdateMessageWAMID(ctx, shopID, msg.ID, result.MessageID); err != nil {
		p.log.Warn("whatsapp processor: update outbound wamid failed (best-effort)",
			"error", err, "message_id", msg.ID, "wamid", result.MessageID)
	}
	return nil
}

// --- helpers ----------------------------------------------------------------

// normalizeCustomerPhone normalizes a phone number to E.164 (digits only, no
// leading +). Meta sends `from` and `wa_id` as digits without the "+"; we
// store customers by their digits-only phone for consistency with the seed
// data (which uses "+2250700000001" — we strip the "+").
func normalizeCustomerPhone(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	digits := b.String()
	if digits == "" {
		return ""
	}
	// Store as "+<digits>" to match the seed convention. The customer
	// UNIQUE constraint is on (shop_id, phone) so we need consistent
	// formatting.
	return "+" + digits
}

// isStopMessage returns true if the message text is exactly "stop" (case
// insensitive, trimmed). Per ch. 6, the "stop" opposition is respected
// immediately — we set consent_marketing=false and ack the opt-out.
func isStopMessage(s string) bool {
	t := strings.ToLower(strings.TrimSpace(s))
	return t == "stop" || t == "désabonner" || t == "desabonner" || t == "unsubscribe"
}

// serializeMediaMeta marshals a media struct to JSON for storage in the
// messages.content column. Returns "[media]" on marshal failure (defensive).
func serializeMediaMeta(v any, kind string) string {
	if v == nil {
		return "[" + kind + "]"
	}
	b, err := json.Marshal(v)
	if err != nil || len(b) == 0 {
		return "[" + kind + "]"
	}
	return string(b)
}
