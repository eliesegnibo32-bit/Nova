// AI service — the high-level entry point for AI-powered conversations.
//
// The service orchestrates: customer get-or-create, conversation get-or-create,
// inbound message persistence, conversation state checks (human vs AI), 24h
// window management, the engine.ProcessMessage call, outbound message
// persistence, and merchant takeover / hand-back.
//
// HTTP handlers in internal/api/handlers/ai.go call into this service. The
// service never touches HTTP directly — it returns plain Go values and
// sentinel errors.
package services

import (
        "context"
        "errors"
        "fmt"
        "time"

        "github.com/google/uuid"
        "github.com/jackc/pgx/v5/pgxpool"
        "log/slog"

        "nova-api/internal/ai"
        "nova-api/internal/models"
        "nova-api/internal/repository"
)

// AI service sentinel errors.
var (
        ErrAIEngineNotInitialized  = errors.New("ai engine not initialized")
        ErrConversationInHumanMode = errors.New("conversation is in human mode — AI cannot process")
        ErrConversationClosed      = errors.New("conversation is closed")
        ErrShopInactive            = errors.New("shop is not active")
)

// AIService is the high-level AI conversation service.
type AIService struct {
        engine           *ai.Engine
        conversationRepo *repository.ConversationRepository
        customerRepo     *repository.CustomerRepository
        aiUsageRepo      *repository.AIUsageRepository
        notifRepo        *repository.NotificationsRepository
        auditRepo        *repository.AuditRepository
        debouncer        *ai.Debouncer
        pool             *pgxpool.Pool
        log              *slog.Logger
}

// NewAIService constructs an AIService.
func NewAIService(
        engine *ai.Engine,
        conversationRepo *repository.ConversationRepository,
        customerRepo *repository.CustomerRepository,
        aiUsageRepo *repository.AIUsageRepository,
        notifRepo *repository.NotificationsRepository,
        auditRepo *repository.AuditRepository,
        debouncer *ai.Debouncer,
        pool *pgxpool.Pool,
        log *slog.Logger,
) *AIService {
        if log == nil {
                log = slog.Default()
        }
        return &AIService{
                engine:           engine,
                conversationRepo: conversationRepo,
                customerRepo:     customerRepo,
                aiUsageRepo:      aiUsageRepo,
                notifRepo:        notifRepo,
                auditRepo:        auditRepo,
                debouncer:        debouncer,
                pool:             pool,
                log:              log,
        }
}

// ProcessResult is the output of ProcessIncomingMessage.
type ProcessResult struct {
        Reply               string              `json:"reply"`
        ConversationID      string              `json:"conversation_id"`
        CustomerID          string              `json:"customer_id"`
        ToolCallsMade       []ai.ToolCallRecord `json:"tool_calls_made,omitempty"`
        TokensIn            int                 `json:"tokens_in"`
        TokensOut           int                 `json:"tokens_out"`
        Model               string              `json:"model"`
        LatencyMs           int                 `json:"latency_ms"`
        CostEstimate        float64             `json:"cost_estimate"`
        GuardrailPassed     bool                `json:"guardrail_passed"`
        GuardrailViolations []string            `json:"guardrail_violations,omitempty"`
        Escalated           bool                `json:"escalated"`
        Regenerated         bool                `json:"regenerated"`
        Processed           bool                `json:"processed"` // false if conversation is in human mode
}

// ProcessIncomingMessage is the main entry point for a customer message.
// It's called by the WhatsApp webhook handler (Task 9) and by the simulation
// console handler (Task 8).
//
// The flow:
//  1. Get-or-create the customer by phone.
//  2. Get-or-create the conversation (state='ai' by default).
//  3. Persist the inbound message.
//  4. If conversation is in 'human' or 'closed' state, don't process with AI.
//  5. Update the 24h window.
//  6. Call engine.ProcessMessage.
//  7. Persist the outbound reply.
//  8. Return the reply (for WhatsApp to send, or for the simulation console).
//
// bypassDebounce=true means "process immediately" — used by the simulation
// console (ch. 5.6 — mode test). When false, the message is buffered by the
// debouncer and the function returns nil (the actual processing happens
// asynchronously when the debounce window fires).
func (s *AIService) ProcessIncomingMessage(ctx context.Context, shopID uuid.UUID, customerPhone, customerName, message, msgType string, bypassDebounce bool, userID uuid.UUID, userRole string) (*ProcessResult, error) {
        if s.engine == nil {
                return nil, ErrAIEngineNotInitialized
        }
        if msgType == "" {
                msgType = "text"
        }

        // 1. Get-or-create the customer.
        cust, err := s.customerRepo.GetOrCreateByPhone(ctx, shopID, customerPhone, customerName)
        if err != nil {
                return nil, fmt.Errorf("ai service: get or create customer: %w", err)
        }

        // 2. Get-or-create the conversation.
        conv, err := s.conversationRepo.GetOrCreate(ctx, shopID, cust.ID, "whatsapp")
        if err != nil {
                return nil, fmt.Errorf("ai service: get or create conversation: %w", err)
        }

        // 3. Persist the inbound message.
        if _, err := s.conversationRepo.AddMessage(ctx, shopID, conv.ID, repository.MsgInbound, msgType, message, ""); err != nil {
                return nil, fmt.Errorf("ai service: add inbound message: %w", err)
        }

        // 4. Check conversation state — if human/closed, don't process with AI.
        if conv.State == "human" {
                // Notify the merchant (best-effort).
                return &ProcessResult{
                        ConversationID: conv.ID.String(),
                        CustomerID:     cust.ID.String(),
                        Processed:      false,
                }, nil
        }
        if conv.State == "closed" {
                return nil, ErrConversationClosed
        }

        // 5. Update the 24h window (best-effort).
        _ = s.conversationRepo.UpdateWindow24h(ctx, shopID, conv.ID, time.Now().Add(24*time.Hour))

        // 6. Call the engine (synchronously when bypassDebounce=true; otherwise
        //    via the debouncer).
        if !bypassDebounce && s.debouncer != nil {
                // Async path: enqueue the message and return immediately. The actual
                // processing happens when the debounce window fires. The webhook
                // returns 200 OK to Meta immediately.
                go func() {
                        ch := s.debouncer.Add(conv.ID.String(), message)
                        go func() {
                                batch := <-ch
                                if len(batch) == 0 {
                                        return
                                }
                                // Join the batch into one message (debounce contract).
                                joined := batch[0]
                                for _, m := range batch[1:] {
                                        joined += "\n" + m
                                }
                                bgCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
                                defer cancel()
                                if _, err := s.engine.ProcessMessage(bgCtx, ai.ProcessMessageRequest{
                                        ShopID:          shopID,
                                        ConversationID:  conv.ID,
                                        CustomerID:      cust.ID,
                                        CustomerMessage: joined,
                                        CustomerName:    customerName,
                                        CustomerPhone:   customerPhone,
                                        MessageType:     msgType,
                                        UserID:          userID,
                                        UserRole:        userRole,
                                }); err != nil {
                                        s.log.Error("ai service: debounced process message failed", "error", err, "conversation_id", conv.ID)
                                }
                        }()
                }()
                return &ProcessResult{
                        ConversationID: conv.ID.String(),
                        CustomerID:     cust.ID.String(),
                        Processed:      false, // debounced — actual reply comes async
                }, nil
        }

        // Synchronous path (simulation console or bypassDebounce=true).
        resp, err := s.engine.ProcessMessage(ctx, ai.ProcessMessageRequest{
                ShopID:          shopID,
                ConversationID:  conv.ID,
                CustomerID:      cust.ID,
                CustomerMessage: message,
                CustomerName:    customerName,
                CustomerPhone:   customerPhone,
                MessageType:     msgType,
                UserID:          userID,
                UserRole:        userRole,
        })
        if err != nil {
                return nil, fmt.Errorf("ai service: engine process: %w", err)
        }

        // 7. Persist the outbound reply (best-effort).
        if resp.ReplyText != "" {
                if _, err := s.conversationRepo.AddMessage(ctx, shopID, conv.ID, repository.MsgOutbound, "text", resp.ReplyText, ""); err != nil {
                        s.log.Warn("ai service: persist outbound reply failed", "error", err)
                }
        }

        // 8. Return the reply.
        return &ProcessResult{
                Reply:               resp.ReplyText,
                ConversationID:      conv.ID.String(),
                CustomerID:          cust.ID.String(),
                ToolCallsMade:       resp.ToolCallsMade,
                TokensIn:            resp.TokensIn,
                TokensOut:           resp.TokensOut,
                Model:               resp.Model,
                LatencyMs:           resp.LatencyMs,
                CostEstimate:        resp.CostEstimate,
                GuardrailPassed:     resp.GuardrailPassed,
                GuardrailViolations: resp.GuardrailViolations,
                Escalated:           resp.Escalated,
                Regenerated:         resp.Regenerated,
                Processed:           true,
        }, nil
}

// TakeOver transitions a conversation to 'human' state, taken over by userID.
// After this, the AI will NOT process incoming messages from this conversation
// (they're buffered for the merchant).
func (s *AIService) TakeOver(ctx context.Context, shopID, conversationID, userID uuid.UUID) error {
        if err := s.conversationRepo.UpdateState(ctx, shopID, conversationID, "human", &userID); err != nil {
                return fmt.Errorf("ai service: takeover: %w", err)
        }
        // Audit log (best-effort — never block the takeover on an audit failure).
        if s.auditRepo != nil {
                afterJSON := []byte(fmt.Sprintf(`{"conversation_id":%q,"state":"human","taken_over_by":%q}`, conversationID, userID))
                _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                        ShopID:     &shopID,
                        ActorID:    &userID,
                        ActorRole:  "owner",
                        Action:     "ai.conversation.takeover",
                        ObjectType: "conversation",
                        ObjectID:   &conversationID,
                        After:      rawJSON{afterJSON},
                })
        }
        return nil
}

// HandBack transitions a conversation back to 'ai' state ("Rendre à NOVA").
// After this, the AI resumes processing incoming messages.
func (s *AIService) HandBack(ctx context.Context, shopID, conversationID, userID uuid.UUID) error {
        if err := s.conversationRepo.UpdateState(ctx, shopID, conversationID, "ai", nil); err != nil {
                return fmt.Errorf("ai service: handback: %w", err)
        }
        if s.auditRepo != nil {
                afterJSON := []byte(fmt.Sprintf(`{"conversation_id":%q,"state":"ai","returned_by":%q}`, conversationID, userID))
                _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                        ShopID:     &shopID,
                        ActorID:    &userID,
                        ActorRole:  "owner",
                        Action:     "ai.conversation.return_to_ai",
                        ObjectType: "conversation",
                        ObjectID:   &conversationID,
                        After:      rawJSON{afterJSON},
                })
        }
        return nil
}

// Close transitions a conversation to 'closed' state. The AI will not process
// further messages from this conversation; the customer must start a new one.
func (s *AIService) Close(ctx context.Context, shopID, conversationID, userID uuid.UUID) error {
        if err := s.conversationRepo.UpdateState(ctx, shopID, conversationID, "closed", nil); err != nil {
                return fmt.Errorf("ai service: close: %w", err)
        }
        if s.auditRepo != nil {
                afterJSON := []byte(fmt.Sprintf(`{"conversation_id":%q,"state":"closed","closed_by":%q}`, conversationID, userID))
                _ = s.auditRepo.Log(ctx, repository.AuditEntry{
                        ShopID:     &shopID,
                        ActorID:    &userID,
                        ActorRole:  "owner",
                        Action:     "ai.conversation.close",
                        ObjectType: "conversation",
                        ObjectID:   &conversationID,
                        After:      rawJSON{afterJSON},
                })
        }
        return nil
}

// rawJSON is a tiny json.Marshaler wrapper so we can pass pre-built JSON bytes
// to repository.AuditEntry (which expects json.Marshaler for Before/After).
type rawJSON struct{ b []byte }

func (r rawJSON) MarshalJSON() ([]byte, error) { return r.b, nil }

// ProcessStoredInboundMessage runs the AI engine on an inbound message that
// has ALREADY been persisted (typically by the WhatsApp webhook processor,
// which needs to control the wamid deduplication, the inbound message row
// creation with the wamid, and the 24h window update itself).
//
// Inputs are the resolved shopID + conversationID + customerID (already
// created by the caller) plus the message body and message type. The function
// does NOT persist the inbound message again — it only calls the engine and
// returns the reply text + metadata. The caller is responsible for sending
// the reply via WhatsApp and persisting the outbound row.
//
// If the conversation is in 'human' state, the function returns
// ProcessResult{Processed:false} — the caller should NOT send an AI reply.
// If the conversation is 'closed', it returns ErrConversationClosed.
func (s *AIService) ProcessStoredInboundMessage(ctx context.Context, shopID, conversationID, customerID uuid.UUID, customerName, customerPhone, message, msgType string, userID uuid.UUID, userRole string) (*ProcessResult, error) {
        if s.engine == nil {
                return nil, ErrAIEngineNotInitialized
        }
        if msgType == "" {
                msgType = "text"
        }

        // Load the conversation to check its state.
        conv, err := s.conversationRepo.GetByID(ctx, shopID, conversationID)
        if err != nil {
                return nil, fmt.Errorf("ai service: process stored: get conversation: %w", err)
        }
        if conv.State == "human" {
                return &ProcessResult{
                        ConversationID: conv.ID.String(),
                        CustomerID:     customerID.String(),
                        Processed:      false,
                }, nil
        }
        if conv.State == "closed" {
                return nil, ErrConversationClosed
        }

        // Call the engine.
        resp, err := s.engine.ProcessMessage(ctx, ai.ProcessMessageRequest{
                ShopID:          shopID,
                ConversationID:  conv.ID,
                CustomerID:      customerID,
                CustomerMessage: message,
                CustomerName:    customerName,
                CustomerPhone:   customerPhone,
                MessageType:     msgType,
                UserID:          userID,
                UserRole:        userRole,
        })
        if err != nil {
                return nil, fmt.Errorf("ai service: process stored: engine: %w", err)
        }

        return &ProcessResult{
                Reply:               resp.ReplyText,
                ConversationID:      conv.ID.String(),
                CustomerID:          customerID.String(),
                ToolCallsMade:       resp.ToolCallsMade,
                TokensIn:            resp.TokensIn,
                TokensOut:           resp.TokensOut,
                Model:               resp.Model,
                LatencyMs:           resp.LatencyMs,
                CostEstimate:        resp.CostEstimate,
                GuardrailPassed:     resp.GuardrailPassed,
                GuardrailViolations: resp.GuardrailViolations,
                Escalated:           resp.Escalated,
                Regenerated:         resp.Regenerated,
                Processed:           true,
        }, nil
}

// ListConversations returns a paginated list of conversations for the shop.
func (s *AIService) ListConversations(ctx context.Context, shopID uuid.UUID, params repository.ListConversationsParams) ([]repository.ConversationListItem, int64, error) {
        return s.conversationRepo.ListByShop(ctx, shopID, params)
}

// GetConversation returns a conversation with its messages.
type ConversationWithMessages struct {
        Conversation repository.Conversation `json:"conversation"`
        Customer     *repository.Customer    `json:"customer,omitempty"`
        Messages     []repository.Message    `json:"messages"`
}

// GetConversation returns the conversation + its messages + the customer.
func (s *AIService) GetConversation(ctx context.Context, shopID, conversationID uuid.UUID) (*ConversationWithMessages, error) {
        conv, err := s.conversationRepo.GetByID(ctx, shopID, conversationID)
        if err != nil {
                return nil, err
        }
        msgs, err := s.conversationRepo.ListMessages(ctx, shopID, conversationID, 100)
        if err != nil {
                return nil, fmt.Errorf("ai service: list messages: %w", err)
        }
        var cust *repository.Customer
        if c, err := s.customerRepo.GetByID(ctx, shopID, conv.CustomerID); err == nil {
                cust = c
        }
        return &ConversationWithMessages{
                Conversation: *conv,
                Customer:     cust,
                Messages:     msgs,
        }, nil
}

// AIUsageStats re-exports the repository stats shape so handlers don't need
// to import the repository package directly.
type AIUsageStats = repository.AIUsageStats

// GetAIUsage returns the AI usage stats for the shop this month.
func (s *AIService) GetAIUsage(ctx context.Context, shopID uuid.UUID) (*AIUsageStats, error) {
        return s.aiUsageRepo.StatsForShop(ctx, shopID)
}

// ListToTakeOver returns conversations in 'human' state for the dashboard.
func (s *AIService) ListToTakeOver(ctx context.Context, shopID uuid.UUID) ([]repository.ConversationListItem, error) {
        return s.conversationRepo.ListToTakeOver(ctx, shopID)
}

// SendMessage allows a merchant (in 'human' mode) to send a message directly
// to the customer. Used by the chat dashboard.
func (s *AIService) SendMessage(ctx context.Context, shopID, conversationID uuid.UUID, content string) (*repository.Message, error) {
        // Persist the outbound message. The actual WhatsApp send happens in
        // Task 9 (webhook module).
        msg, err := s.conversationRepo.AddMessage(ctx, shopID, conversationID, repository.MsgOutbound, "text", content, "")
        if err != nil {
                return nil, fmt.Errorf("ai service: send message: %w", err)
        }
        return msg, nil
}

// GetOrCreateCustomerByPhone is a thin pass-through to the customer repo. It
// lets the WhatsApp admin handler resolve-or-create a customer without
// pulling in the repository package directly.
func (s *AIService) GetOrCreateCustomerByPhone(ctx context.Context, shopID uuid.UUID, phone, name string) (*repository.Customer, error) {
        return s.customerRepo.GetOrCreateByPhone(ctx, shopID, phone, name)
}

// GetOrCreateConversation is a thin pass-through to the conversation repo.
func (s *AIService) GetOrCreateConversation(ctx context.Context, shopID, customerID uuid.UUID, channel string) (*repository.Conversation, error) {
        return s.conversationRepo.GetOrCreate(ctx, shopID, customerID, channel)
}

// PersistOutboundWithWAMID stores an outbound message with the given wamid.
// Used by the WhatsApp admin handler when the merchant sends a message
// directly — the wamid comes from the WhatsApp Cloud API response.
func (s *AIService) PersistOutboundWithWAMID(ctx context.Context, shopID, conversationID uuid.UUID, msgType, content, wamid string) (*repository.Message, error) {
        return s.conversationRepo.AddMessage(ctx, shopID, conversationID, repository.MsgOutbound, msgType, content, wamid)
}

// UpdateMessageWAMID fills in the wamid on a previously-stored outbound
// message (after the WhatsApp send returned a wamid). Passes through to the
// conversation repo.
func (s *AIService) UpdateMessageWAMID(ctx context.Context, shopID, messageID uuid.UUID, wamid string) error {
        return s.conversationRepo.UpdateMessageWAMID(ctx, shopID, messageID, wamid)
}

// Ensure the unused import is referenced (models may be used in future DTOs).
var _ = models.ConvAI

// ============================================================================
// Adapters — wrap the concrete services to implement the ai package interfaces.
// This breaks the import cycle ai ↔ services by keeping the wrappers in the
// services package (which imports ai).
// ============================================================================

// catalogListerAdapter wraps services.CatalogService to implement ai.CatalogLister.
type catalogListerAdapter struct {
        inner *CatalogService
}

func (a *catalogListerAdapter) GetProduct(ctx context.Context, userID uuid.UUID, userRole string, shopID, productID uuid.UUID) (*repository.ProductWithRelations, error) {
        return a.inner.GetProduct(ctx, userID, userRole, shopID, productID)
}

func (a *catalogListerAdapter) ListProductsTyped(ctx context.Context, userID uuid.UUID, userRole string, shopID uuid.UUID, params models.ListProductsParams) ([]ai.ProductListItem, int64, error) {
        items, total, err := a.inner.ListProducts(ctx, userID, userRole, shopID, params)
        if err != nil {
                return nil, 0, err
        }
        out := make([]ai.ProductListItem, 0, len(items))
        for _, it := range items {
                out = append(out, ai.ProductListItem{
                        Product:      it.Product,
                        VariantCount: it.VariantCount,
                        FirstImage:   it.FirstImage,
                })
        }
        return out, total, nil
}

// orderServiceAdapter wraps services.OrderService to implement ai.OrderServiceIface.
// The only difference is ConfirmOrder, which takes ai.ConfirmOrderInput instead
// of services.ConfirmOrderRequest. The adapter converts.
type orderServiceAdapter struct {
        inner *OrderService
}

func (a *orderServiceAdapter) GetOrCreateCart(ctx context.Context, userID uuid.UUID, userRole string, shopID uuid.UUID, conversationID, customerID *uuid.UUID) (*models.Cart, error) {
        return a.inner.GetOrCreateCart(ctx, userID, userRole, shopID, conversationID, customerID)
}

func (a *orderServiceAdapter) AddToCart(ctx context.Context, userID uuid.UUID, userRole string, shopID, cartID, variantID uuid.UUID, quantity int) (*models.Cart, error) {
        return a.inner.AddToCart(ctx, userID, userRole, shopID, cartID, variantID, quantity)
}

func (a *orderServiceAdapter) UpdateCartItem(ctx context.Context, userID uuid.UUID, userRole string, shopID, cartID, itemID uuid.UUID, quantity int) (*models.Cart, error) {
        return a.inner.UpdateCartItem(ctx, userID, userRole, shopID, cartID, itemID, quantity)
}

func (a *orderServiceAdapter) GenerateRecap(ctx context.Context, userID uuid.UUID, userRole string, shopID, cartID, zoneID uuid.UUID, paymentMode string) (*models.OrderRecap, error) {
        return a.inner.GenerateRecap(ctx, userID, userRole, shopID, cartID, zoneID, paymentMode)
}

func (a *orderServiceAdapter) ConfirmOrder(ctx context.Context, userID uuid.UUID, userRole string, shopID uuid.UUID, req ai.ConfirmOrderInput, ip, userAgent string) (*models.Order, error) {
        // Convert ai.ConfirmOrderInput → services.ConfirmOrderRequest.
        svcReq := ConfirmOrderRequest{
                CartID:          req.CartID,
                ZoneID:          req.ZoneID,
                DeliveryAddress: req.DeliveryAddress,
                PaymentMode:     req.PaymentMode,
                PaymentStatus:   req.PaymentStatus,
                IdempotencyKey:  req.IdempotencyKey,
                CustomerID:      req.CustomerID,
                Note:            req.Note,
                AutoConfirm:     req.AutoConfirm,
        }
        return a.inner.ConfirmOrder(ctx, userID, userRole, shopID, svcReq, ip, userAgent)
}

// NewAIEngineDeps is a convenience constructor that builds the ai.EngineDeps
// from the concrete services + repositories. It applies the adapters above.
// This is the function main.go calls to wire the AI engine.
func NewAIEngineDeps(
        provider ai.Provider,
        cfg ai.EngineConfig,
        shopRepo *repository.ShopRepository,
        customerRepo *repository.CustomerRepository,
        conversationRepo *repository.ConversationRepository,
        aiUsageRepo *repository.AIUsageRepository,
        auditRepo *repository.AuditRepository,
        catalogSvc *CatalogService,
        stockSvc *StockService,
        deliverySvc *DeliveryService,
        orderSvc *OrderService,
        notifRepo *repository.NotificationsRepository,
        productOptionRepo *repository.ProductOptionRepository,
        paymentCfgRepo *repository.PaymentConfigRepository,
        pool *pgxpool.Pool,
        log *slog.Logger,
) ai.EngineDeps {
        return ai.EngineDeps{
                Provider:         provider,
                Config:           cfg,
                ShopRepo:         shopRepo,
                CustomerRepo:     customerRepo,
                ConversationRepo: conversationRepo,
                AIUsageRepo:      aiUsageRepo,
                AuditRepo:        &auditRepoAdapter{inner: auditRepo},
                CatalogSvc:       &catalogListerAdapter{inner: catalogSvc},
                StockSvc:         stockSvc,
                DeliverySvc:      deliverySvc,
                OrderSvc:         &orderServiceAdapter{inner: orderSvc},
                NotifRepo:        notifRepo,
                ProductOptionRepo: productOptionRepo,
                PaymentCfgRepo:    paymentCfgRepo,
                Pool:             pool,
                Log:              log,
        }
}

// auditRepoAdapter wraps repository.AuditRepository to implement
// ai.AuditRepoIface. The engine uses this narrow interface to log AI
// interactions without depending on the repository package directly.
type auditRepoAdapter struct {
        inner *repository.AuditRepository
}

// LogAIInteraction writes an AI interaction event to audit_logs.
func (a *auditRepoAdapter) LogAIInteraction(ctx context.Context, shopID uuid.UUID, actorID *uuid.UUID, action, objectType string, objectID *uuid.UUID, afterJSON []byte) error {
        if a == nil || a.inner == nil {
                return nil
        }
        return a.inner.Log(ctx, repository.AuditEntry{
                ShopID:     &shopID,
                ActorID:    actorID,
                ActorRole:  "owner",
                Action:     action,
                ObjectType: objectType,
                ObjectID:   objectID,
                After:      rawJSON{afterJSON},
        })
}
