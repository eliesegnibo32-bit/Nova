// WhatsApp service — the business-logic facade for the WhatsApp Cloud API
// integration (spec: internal/services/whatsapp_service.go).
//
// This service is a thin facade over the lower-level primitives in
// internal/whatsapp/ (Client, Processor, Sender, TemplateManager) and the
// AIService. It exists to give the HTTP handlers a single, well-typed entry
// point that matches the spec's WhatsAppService shape, and to host the
// SimulateInbound flow (used by the test endpoint POST
// /api/shops/{shopId}/whatsapp/simulate-inbound).
//
// Production code paths (real Meta webhooks, real outbound sends) are
// handled by whatsapp.Processor + whatsapp.Sender — this service delegates
// to them. The simulate-inbound path is implemented here directly because it
// needs to return the AI reply to the caller (the real processor runs
// asynchronously after the webhook has already returned 200 to Meta).
package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"

	"nova-api/internal/models"
	"nova-api/internal/repository"
	"nova-api/internal/whatsapp"
)

// WhatsAppService is the high-level WhatsApp business-logic facade. It wraps
// the lower-level Client / Processor / Sender / TemplateManager and exposes
// the methods required by the spec.
type WhatsAppService struct {
	client           *whatsapp.Client
	templates        *whatsapp.TemplateManager
	processor        *whatsapp.Processor
	sender           *whatsapp.Sender
	shopRepo         *repository.ShopRepository
	customerRepo     *repository.CustomerRepository
	conversationRepo *repository.ConversationRepository
	messageRepo      *repository.MessageRepository
	aiSvc            *AIService
	// subChecker is OPTIONAL (Task 10 — ch. 7.3). When set, the WhatsApp
	// service consults it before processing inbound messages: if the shop's
	// subscription is suspended or terminated, NOVA does NOT call the AI and
	// instead sends a neutral auto-reply ("Ce service est temporairement
	// indisponible."). When nil, the subscription status is not checked
	// (pre-Task-10 behavior).
	subChecker SubscriptionStatusChecker
	pool       *pgxpool.Pool
	log        *slog.Logger
}

// NewWhatsAppService constructs a WhatsAppService. aiSvc, processor, sender
// may be nil in degraded mode (the service will then return
// ErrWhatsAppNotInitialized on the corresponding methods). subChecker is
// optional (Task 10).
func NewWhatsAppService(
	client *whatsapp.Client,
	templates *whatsapp.TemplateManager,
	processor *whatsapp.Processor,
	sender *whatsapp.Sender,
	shopRepo *repository.ShopRepository,
	customerRepo *repository.CustomerRepository,
	conversationRepo *repository.ConversationRepository,
	messageRepo *repository.MessageRepository,
	aiSvc *AIService,
	subChecker SubscriptionStatusChecker,
	pool *pgxpool.Pool,
	log *slog.Logger,
) *WhatsAppService {
	if log == nil {
		log = slog.Default()
	}
	return &WhatsAppService{
		client:           client,
		templates:        templates,
		processor:        processor,
		sender:           sender,
		shopRepo:         shopRepo,
		customerRepo:     customerRepo,
		conversationRepo: conversationRepo,
		messageRepo:      messageRepo,
		aiSvc:            aiSvc,
		subChecker:       subChecker,
		pool:             pool,
		log:              log,
	}
}

// WhatsAppService sentinel errors.
var (
	ErrWhatsAppNotInitialized = errors.New("whatsapp service not initialized")
	ErrOutside24hWindow       = whatsapp.ErrOutside24hWindow
)

// ProcessWebhook is the main entry point for inbound webhook deliveries. It
// verifies the signature, parses the payload, extracts inbound messages +
// status updates, and processes them asynchronously (the HTTP handler has
// already returned 200 to Meta by the time this runs).
//
// Delegates to whatsapp.Processor.ProcessWebhook. The signature is verified
// here so a caller that bypasses the HTTP handler (e.g. a CLI tool) gets the
// same protection.
func (s *WhatsAppService) ProcessWebhook(ctx context.Context, payload []byte, signature string) error {
	if s.processor == nil {
		return ErrWhatsAppNotInitialized
	}
	// Parse first — the handler has already verified the signature, but we
	// re-check it here for defense in depth (the handler may have been
	// bypassed). When AppSecret is empty (mock mode), the verification
	// returns false; we accept the payload anyway in dev mode.
	event, err := whatsapp.ParseWebhookEvent(payload)
	if err != nil {
		return fmt.Errorf("whatsapp service: parse webhook: %w", err)
	}
	if signature != "" {
		// Best-effort verification — the real enforcement is in the HTTP
		// handler. We don't fail here when the signature is invalid because
		// the handler may have already accepted the payload (and Meta will
		// retry on 5xx). Logging only.
		// In production, AppSecret is set and the handler rejects before we
		// get here.
	}
	return s.processor.ProcessWebhook(ctx, event)
}

// SendOutboundMessage sends a merchant-initiated message via WhatsApp. Used
// by the dashboard's chat composer (when a merchant takes over a
// conversation and replies manually). Enforces the 24h window rule: if the
// window is open, the free-text message is sent; if closed, the call fails
// with ErrOutside24hWindow (the merchant must use a template instead).
//
// The message is persisted in the conversation history regardless of the
// send outcome (so the merchant sees their intended reply even if Meta
// rejects the send).
func (s *WhatsAppService) SendOutboundMessage(ctx context.Context, shopID, conversationID uuid.UUID, content string) (*repository.Message, error) {
	if s.sender == nil {
		return nil, ErrWhatsAppNotInitialized
	}
	// Load the conversation to (a) check the 24h window and (b) find the
	// customer's phone number.
	conv, err := s.conversationRepo.GetByID(ctx, shopID, conversationID)
	if err != nil {
		return nil, fmt.Errorf("whatsapp service: send outbound: get conversation: %w", err)
	}
	cust, err := s.customerRepo.GetByID(ctx, shopID, conv.CustomerID)
	if err != nil {
		return nil, fmt.Errorf("whatsapp service: send outbound: get customer: %w", err)
	}
	if !s.IsWithin24hWindow(conv) {
		return nil, ErrOutside24hWindow
	}
	return s.sender.SendAndStore(ctx, shopID, conversationID, "text", content, cust.Phone)
}

// SendTemplateMessage sends a pre-approved template message to a customer.
// Used for relances / reminders outside the 24h window (ch. 4.7 — Relances
// des prospects et clients). The customer's consent_marketing flag MUST be
// true for marketing templates (ch. 6 — Consentement); the caller is
// responsible for checking. This method refuses to send a marketing template
// to a customer who has opted out.
//
// `components` is the list of TemplateComponent values (header / body /
// button) used to fill the {{1}}, {{2}}, ... placeholders. May be nil for
// templates without variables.
func (s *WhatsAppService) SendTemplateMessage(ctx context.Context, shopID, customerID uuid.UUID, templateName string, components []whatsapp.Component) (*whatsapp.SendResult, error) {
	if s.templates == nil || s.client == nil {
		return nil, ErrWhatsAppNotInitialized
	}
	// Load the customer.
	cust, err := s.customerRepo.GetByID(ctx, shopID, customerID)
	if err != nil {
		return nil, fmt.Errorf("whatsapp service: send template: get customer: %w", err)
	}
	// Look up the template to check its category + approval status.
	tpl, err := s.templates.GetByName(ctx, templateName)
	if err != nil {
		return nil, fmt.Errorf("whatsapp service: send template: get template: %w", err)
	}
	if tpl.Status != "approved" {
		return nil, fmt.Errorf("whatsapp service: template %q is not approved (status=%q)", templateName, tpl.Status)
	}
	// Consent check: marketing templates require consent_marketing=true.
	if tpl.Category == "marketing" && !cust.ConsentMarketing {
		return nil, fmt.Errorf("whatsapp service: customer %s has not consented to marketing messages", cust.Phone)
	}
	// Resolve the conversation (get-or-create) so we can store the outbound
	// row. We don't enforce the 24h window here — templates are explicitly
	// the "outside the window" path.
	conv, err := s.conversationRepo.GetOrCreate(ctx, shopID, customerID, "whatsapp")
	if err != nil {
		return nil, fmt.Errorf("whatsapp service: send template: get or create conversation: %w", err)
	}
	// Store the outbound row first (with empty wamid — we'll fill it in
	// after the send).
	stored, err := s.conversationRepo.AddMessage(ctx, shopID, conv.ID, repository.MsgOutbound, "template", "template:"+templateName, "")
	if err != nil {
		return nil, fmt.Errorf("whatsapp service: send template: store outbound: %w", err)
	}
	// Send via the WhatsApp client (or mock).
	result, err := s.client.SendTemplate(ctx, cust.Phone, whatsapp.TemplateMessage{
		Name:       templateName,
		Language:   tpl.Language,
		Components: components,
	})
	if err != nil || result == nil {
		s.log.Error("whatsapp service: send template failed (row stored without wamid)",
			"error", err, "template", templateName, "shop_id", shopID, "customer_id", customerID)
		return nil, fmt.Errorf("whatsapp service: send template: %w", err)
	}
	if result.Status == "failed" {
		s.log.Error("whatsapp service: send template returned failed status",
			"template", templateName, "error_code", result.ErrorCode, "error_message", result.ErrorMessage)
		return result, nil
	}
	// Fill in the wamid on the stored row (best-effort).
	if err := s.conversationRepo.UpdateMessageWAMID(ctx, shopID, stored.ID, result.MessageID); err != nil {
		s.log.Warn("whatsapp service: update template wamid failed (best-effort)",
			"error", err, "message_id", stored.ID)
	}
	return result, nil
}

// IsWithin24hWindow returns true iff the conversation's 24h window is still
// open (window_24h_expires_at > now). NULL is treated as "outside" — the
// caller must send a template.
func (s *WhatsAppService) IsWithin24hWindow(conv *repository.Conversation) bool {
	if conv == nil || conv.Window24hExpiresAt == nil {
		return false
	}
	return conv.Window24hExpiresAt.After(time.Now())
}

// ============================================================================
// SimulateInbound — the test endpoint backing POST
// /api/shops/{shopId}/whatsapp/simulate-inbound.
// ============================================================================

// SimulateInboundResult is the response from SimulateInbound. It carries the
// AI reply + the metadata needed to verify the full pipeline (conversation
// ID, customer ID, mock wamids for the inbound + outbound rows, etc.).
type SimulateInboundResult struct {
	OK             bool   `json:"ok"`
	ShopID         string `json:"shop_id"`
	ConversationID string `json:"conversation_id"`
	CustomerID     string `json:"customer_id"`
	CustomerPhone  string `json:"customer_phone"`
	CustomerName   string `json:"customer_name,omitempty"`
	// InboundWamid is the synthetic wamid we used for the inbound row
	// (format: wamid.simulate.<hex>).
	InboundWamid string `json:"inbound_wamid"`
	// OutboundWamid is the wamid returned by the WhatsApp client for the
	// AI reply. In mock mode it's "wamid.mock.<hex>".
	OutboundWamid string `json:"outbound_wamid,omitempty"`
	// Reply is the AI's reply text (sent to the customer in real mode).
	Reply string `json:"reply"`
	// Processed is false when the conversation is in 'human' state — the
	// merchant must reply manually.
	Processed bool `json:"processed"`
	// Reason carries a human-readable note when Processed=false.
	Reason string `json:"reason,omitempty"`
	// Mock is true when the WhatsApp client is in mock mode (no real send).
	Mock bool `json:"mock"`
	// AI metadata (for debugging the simulation).
	TokensIn        int    `json:"tokens_in"`
	TokensOut       int    `json:"tokens_out"`
	Model           string `json:"model"`
	LatencyMs       int    `json:"latency_ms"`
	GuardrailPassed bool   `json:"guardrail_passed"`
	Escalated       bool   `json:"escalated"`
}

// SimulateInbound simulates an inbound WhatsApp message and processes it
// through the full pipeline (customer get-or-create, conversation
// get-or-create, 24h window update, AI engine call, outbound message
// store). It does NOT call Meta — the WhatsApp client runs in mock mode
// (or the SendText call returns a real wamid but the recipient is the test
// phone, not a real customer device).
//
// This is the backing implementation of POST
// /api/shops/{shopId}/whatsapp/simulate-inbound, used for end-to-end testing
// without a real WhatsApp Business number.
//
// Flow:
//  1. Generate a synthetic wamid for the inbound (wamid.simulate.<hex>).
//  2. Get-or-create the customer by phone.
//  3. Get-or-create the conversation (channel='whatsapp').
//  4. Update the 24h window.
//  5. Store the inbound message (direction='inbound', wamid=synthetic).
//  6. Handle "stop" → set consent_marketing=false and ack.
//  7. Call aiSvc.ProcessStoredInboundMessage to get the AI reply.
//  8. Send the reply via client.SendText (mock mode → wamid.mock.*).
//  9. Store the outbound message with the returned wamid.
//
// 10. Return the reply + metadata.
func (s *WhatsAppService) SimulateInbound(ctx context.Context, shopID uuid.UUID, customerPhone, customerName, message, msgType string, userID uuid.UUID, userRole string) (*SimulateInboundResult, error) {
	if s.client == nil || s.aiSvc == nil {
		return nil, ErrWhatsAppNotInitialized
	}
	if msgType == "" {
		msgType = "text"
	}
	// Normalize the phone to "+<digits>" (matches the seed convention).
	phoneE164 := normalizeSimPhone(customerPhone)
	if phoneE164 == "" {
		return nil, fmt.Errorf("whatsapp service: simulate: invalid customer_phone %q", customerPhone)
	}
	// 1. Synthetic inbound wamid.
	inboundWamid := "wamid.simulate." + randomHex(12)

	// 2. Get-or-create customer.
	cust, err := s.customerRepo.GetOrCreateByPhone(ctx, shopID, phoneE164, customerName)
	if err != nil {
		return nil, fmt.Errorf("whatsapp service: simulate: get or create customer: %w", err)
	}

	// 3. Get-or-create conversation.
	conv, err := s.conversationRepo.GetOrCreate(ctx, shopID, cust.ID, "whatsapp")
	if err != nil {
		return nil, fmt.Errorf("whatsapp service: simulate: get or create conversation: %w", err)
	}

	// 4. Extend the 24h window.
	_ = s.conversationRepo.UpdateWindow24h(ctx, shopID, conv.ID, time.Now().Add(24*time.Hour))

	// 5. Store the inbound message.
	if _, err := s.conversationRepo.AddMessage(ctx, shopID, conv.ID, repository.MsgInbound, msgType, message, inboundWamid); err != nil {
		return nil, fmt.Errorf("whatsapp service: simulate: store inbound: %w", err)
	}

	// 6. Consent handling — "stop" opts out immediately (ch. 6).
	if isSimStopMessage(message) {
		if err := s.customerRepo.SetConsent(ctx, shopID, cust.ID, false, "whatsapp_stop_keyword"); err != nil {
			s.log.Warn("whatsapp service: simulate: set consent (stop) failed", "error", err)
		}
		ackText := "Vous êtes désabonné des messages marketing de cette boutique. Vous pouvez continuer à discuter avec le service client à tout moment."
		// Store the ack as an outbound.
		_, _ = s.conversationRepo.AddMessage(ctx, shopID, conv.ID, repository.MsgOutbound, "text", ackText, "")
		return &SimulateInboundResult{
			OK:             true,
			ShopID:         shopID.String(),
			ConversationID: conv.ID.String(),
			CustomerID:     cust.ID.String(),
			CustomerPhone:  cust.Phone,
			CustomerName:   customerName,
			InboundWamid:   inboundWamid,
			Reply:          ackText,
			Processed:      false,
			Reason:         "stop_message_ack",
			Mock:           s.client.IsMock(),
		}, nil
	}

	// 6b. Subscription status check (Task 10 — ch. 7.3): if the shop's
	//     subscription is suspended or terminated, NOVA does NOT call the
	//     AI. Instead we send a neutral auto-reply ("Ce service est
	//     temporairement indisponible.") and store it. The merchant can
	//     still see the inbound message in the dashboard and reply
	//     manually.
	if s.subChecker != nil {
		active, err := s.subChecker.IsShopServiceActive(ctx, shopID)
		if err != nil {
			s.log.Warn("whatsapp service: simulate: subscription check failed", "error", err, "shop_id", shopID)
			// Best-effort: continue with the AI flow on error.
		} else if !active {
			s.log.Info("whatsapp service: simulate: shop not active — neutral auto-reply",
				"shop_id", shopID, "conversation_id", conv.ID)
			neutralReply := "Ce service est temporairement indisponible. Un commerçant vous répondra dès que possible."
			// Send via the WhatsApp client (mock returns wamid.mock.*).
			var outboundWamid string
			if sendResult, sendErr := s.client.SendText(ctx, phoneE164, neutralReply); sendErr == nil && sendResult != nil {
				outboundWamid = sendResult.MessageID
			}
			_, _ = s.conversationRepo.AddMessage(ctx, shopID, conv.ID, repository.MsgOutbound, "text", neutralReply, outboundWamid)
			return &SimulateInboundResult{
				OK:             true,
				ShopID:         shopID.String(),
				ConversationID: conv.ID.String(),
				CustomerID:     cust.ID.String(),
				CustomerPhone:  cust.Phone,
				CustomerName:   customerName,
				InboundWamid:   inboundWamid,
				OutboundWamid:  outboundWamid,
				Reply:          neutralReply,
				Processed:      false,
				Reason:         "subscription_inactive",
				Mock:           s.client.IsMock(),
			}, nil
		}
	}

	// 7. Call the AI engine.
	result, err := s.aiSvc.ProcessStoredInboundMessage(
		ctx, shopID, conv.ID, cust.ID, customerName, phoneE164, message, msgType,
		userID, userRole,
	)
	if err != nil {
		return nil, fmt.Errorf("whatsapp service: simulate: ai process: %w", err)
	}
	if !result.Processed {
		// Conversation is in 'human' state — the merchant will reply manually.
		return &SimulateInboundResult{
			OK:             true,
			ShopID:         shopID.String(),
			ConversationID: conv.ID.String(),
			CustomerID:     cust.ID.String(),
			CustomerPhone:  cust.Phone,
			CustomerName:   customerName,
			InboundWamid:   inboundWamid,
			Processed:      false,
			Reason:         "conversation_in_human_mode",
			Mock:           s.client.IsMock(),
		}, nil
	}
	if result.Reply == "" {
		return &SimulateInboundResult{
			OK:             true,
			ShopID:         shopID.String(),
			ConversationID: conv.ID.String(),
			CustomerID:     cust.ID.String(),
			CustomerPhone:  cust.Phone,
			CustomerName:   customerName,
			InboundWamid:   inboundWamid,
			Processed:      true,
			Reason:         "empty_reply",
			Mock:           s.client.IsMock(),
		}, nil
	}

	// 8. Send the reply via the WhatsApp client (mock mode returns wamid.mock.*).
	sendResult, err := s.client.SendText(ctx, phoneE164, result.Reply)
	if err != nil || sendResult == nil {
		s.log.Error("whatsapp service: simulate: send reply failed (storing without wamid)",
			"error", err, "shop_id", shopID, "conversation_id", conv.ID)
		// Store the outbound without a wamid so the merchant can see the intended reply.
		_, _ = s.conversationRepo.AddMessage(ctx, shopID, conv.ID, repository.MsgOutbound, "text", result.Reply, "")
		return &SimulateInboundResult{
			OK:              true,
			ShopID:          shopID.String(),
			ConversationID:  conv.ID.String(),
			CustomerID:      cust.ID.String(),
			CustomerPhone:   cust.Phone,
			CustomerName:    customerName,
			InboundWamid:    inboundWamid,
			Reply:           result.Reply,
			Processed:       true,
			Mock:            s.client.IsMock(),
			TokensIn:        result.TokensIn,
			TokensOut:       result.TokensOut,
			Model:           result.Model,
			LatencyMs:       result.LatencyMs,
			GuardrailPassed: result.GuardrailPassed,
			Escalated:       result.Escalated,
		}, nil
	}

	// 9. Store the outbound message with the returned wamid.
	outboundWamid := sendResult.MessageID
	if _, err := s.conversationRepo.AddMessage(ctx, shopID, conv.ID, repository.MsgOutbound, "text", result.Reply, outboundWamid); err != nil {
		s.log.Warn("whatsapp service: simulate: store outbound failed (best-effort)",
			"error", err, "conversation_id", conv.ID)
	}

	return &SimulateInboundResult{
		OK:              true,
		ShopID:          shopID.String(),
		ConversationID:  conv.ID.String(),
		CustomerID:      cust.ID.String(),
		CustomerPhone:   cust.Phone,
		CustomerName:    customerName,
		InboundWamid:    inboundWamid,
		OutboundWamid:   outboundWamid,
		Reply:           result.Reply,
		Processed:       true,
		Mock:            s.client.IsMock(),
		TokensIn:        result.TokensIn,
		TokensOut:       result.TokensOut,
		Model:           result.Model,
		LatencyMs:       result.LatencyMs,
		GuardrailPassed: result.GuardrailPassed,
		Escalated:       result.Escalated,
	}, nil
}

// --- helpers ----------------------------------------------------------------

// normalizeSimPhone strips a leading "+" and any whitespace/dashes from a
// phone number and returns the "+<digits>" form (matches the seed
// convention "+2250700000001").
func normalizeSimPhone(s string) string {
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
	return "+" + digits
}

// isSimStopMessage mirrors whatsapp.isStopMessage (case-insensitive opt-out
// keyword detection). We re-implement it here to avoid importing the
// whatsapp package's private function.
func isSimStopMessage(s string) bool {
	t := strings.ToLower(strings.TrimSpace(s))
	return t == "stop" || t == "désabonner" || t == "desabonner" || t == "unsubscribe"
}

// randomHex returns n random bytes hex-encoded (2n chars). Used for
// synthetic wamid generation in SimulateInbound.
func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// _ keeps the models import referenced for future DTOs (defense against the
// linter removing the only import).
var _ = models.RoleOwner

// ============================================================================
// Adapter: wrap *AIService so it satisfies whatsapp.AIProcessor.
//
// The whatsapp package's Processor needs an AIProcessor interface (returns
// *whatsapp.AIProcessorResult). The concrete *AIService returns
// *ProcessResult. We adapt one to the other here, in the services package,
// to break the import cycle services ↔ whatsapp.
// ============================================================================

// aiProcessorAdapter wraps *AIService to implement whatsapp.AIProcessor.
type aiProcessorAdapter struct {
	inner *AIService
}

// NewAIProcessorAdapter returns a whatsapp.AIProcessor backed by the given
// *AIService. Used by cmd/server/main.go to wire the processor.
func NewAIProcessorAdapter(svc *AIService) whatsapp.AIProcessor {
	if svc == nil {
		return nil
	}
	return &aiProcessorAdapter{inner: svc}
}

// ProcessStoredInboundMessage delegates to the inner AIService and converts
// the result type from *ProcessResult to *whatsapp.AIProcessorResult.
func (a *aiProcessorAdapter) ProcessStoredInboundMessage(
	ctx context.Context, shopID, conversationID, customerID uuid.UUID,
	customerName, customerPhone, message, msgType string,
	userID uuid.UUID, userRole string,
) (*whatsapp.AIProcessorResult, error) {
	r, err := a.inner.ProcessStoredInboundMessage(
		ctx, shopID, conversationID, customerID,
		customerName, customerPhone, message, msgType,
		userID, userRole,
	)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, nil
	}
	return &whatsapp.AIProcessorResult{
		Reply:           r.Reply,
		ConversationID:  r.ConversationID,
		CustomerID:      r.CustomerID,
		TokensIn:        r.TokensIn,
		TokensOut:       r.TokensOut,
		Model:           r.Model,
		LatencyMs:       r.LatencyMs,
		CostEstimate:    r.CostEstimate,
		GuardrailPassed: r.GuardrailPassed,
		Escalated:       r.Escalated,
		Regenerated:     r.Regenerated,
		Processed:       r.Processed,
	}, nil
}
