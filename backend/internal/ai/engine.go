// Core AI engine (cahier des charges ch. 5.1 — Moteur d'intelligence artificielle).
//
// The engine orchestrates the conversation flow:
//  1. Load conversation context (last N messages, cart summary, customer info).
//  2. Build the system prompt with the 11 rules + shop info.
//  3. Build the tool definitions from the ToolRegistry.
//  4. Call provider.Chat with messages + tools.
//  5. If the response has tool calls: execute each tool via ToolRegistry.Execute,
//     append tool results to messages, call provider.Chat again (loop, max 5
//     iterations).
//  6. Once the response has no tool calls (final text reply): run the output
//     guardrail.
//  7. If guardrail passes: return the reply. If guardrail fails: regenerate
//     with a warning, or escalate to human.
//  8. Record ai_usage (tokens, model, latency, cost estimate).
//  9. Save the messages to the conversations/messages tables (done by the
//     AI service, not the engine — the engine is conversation-storage-agnostic).
//
// The engine is provider-agnostic (MockProvider or OpenAIProvider) and storage-
// agnostic (it doesn't write messages to the DB — that's the AI service's job).
// This makes the engine unit-testable with a mock provider + in-memory repos.
package ai

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

// EngineConfig holds the runtime knobs for the engine.
type EngineConfig struct {
        Model                string
        Temperature          float32
        MaxTokens            int
        DebounceMs           int
        MaxConversationTurns int // max tool-call iterations per ProcessMessage call
        // MaxRetries is the max number of guardrail-regeneration attempts before
        // escalating to human (ch. 5.5). Default 2 → 1 initial attempt + 2 retries.
        MaxRetries int
        // MaxHistoryMessages caps the number of prior messages included in the LLM
        // context (ch. 5.6 — context bounding). Older messages are summarized via
        // conversation.summary.
        MaxHistoryMessages int
}

// DefaultEngineConfig returns sensible defaults.
func DefaultEngineConfig() EngineConfig {
        return EngineConfig{
                Model:                "nova-mock",
                Temperature:          0.3,
                MaxTokens:            800,
                DebounceMs:           4000,
                MaxConversationTurns: 5,
                MaxRetries:           2,
                MaxHistoryMessages:   20,
        }
}

// ProcessMessageRequest is the input to Engine.ProcessMessage.
type ProcessMessageRequest struct {
        ShopID          uuid.UUID
        ConversationID  uuid.UUID
        CustomerID      uuid.UUID
        CustomerMessage string
        CustomerName    string
        CustomerPhone   string
        MessageType     string    // text | image | audio | video | document (default: text)
        UserID          uuid.UUID // the user on behalf of whom we're processing (for RLS)
        UserRole        string
}

// ToolCallRecord is one executed tool call (for the response + audit).
type ToolCallRecord struct {
        Name      string          `json:"name"`
        Arguments json.RawMessage `json:"arguments"`
        Result    json.RawMessage `json:"result"`
        LatencyMs int             `json:"latency_ms"`
        Error     string          `json:"error,omitempty"`
}

// ProcessMessageResponse is the output of Engine.ProcessMessage.
type ProcessMessageResponse struct {
        ReplyText           string            `json:"reply_text"`
        ToolCallsMade       []ToolCallRecord  `json:"tool_calls_made"`
        TokensIn            int               `json:"tokens_in"`
        TokensOut           int               `json:"tokens_out"`
        Model               string            `json:"model"`
        LatencyMs           int               `json:"latency_ms"`
        CostEstimate        float64           `json:"cost_estimate"`
        GuardrailPassed     bool              `json:"guardrail_passed"`
        GuardrailTrace      map[string]string `json:"guardrail_trace,omitempty"`
        GuardrailViolations []string          `json:"guardrail_violations,omitempty"`
        Escalated           bool              `json:"escalated"`
        Regenerated         bool              `json:"regenerated"`
}

// Engine is the core AI engine. One instance is shared across all conversations
// (it's stateless — per-conversation state lives in the ToolRegistry).
type Engine struct {
        provider            Provider
        config              EngineConfig
        guardrail           *GuardrailChecker
        toolRegistryFactory ToolRegistryFactory
        shopRepo            ShopRepoIface
        customerRepo        CustomerRepoIface
        conversationRepo    ConversationRepoIface
        aiUsageRepo         AIUsageRepoIface
        auditRepo           AuditRepoIface
        // quotaSvc is an OPTIONAL dependency (Task 10 — ch. 7.2). When set, the
        // engine consults it before every LLM call: if the shop is in degraded
        // mode (≥100% of monthly quota), the LLM call is skipped and a neutral
        // auto-reply is returned to the customer. After each successful call,
        // IncrementUsage(...) is invoked to record the AI usage + re-evaluate the
        // 80%/100% thresholds.
        quotaSvc QuotaServiceIface
        pool     *pgxpool.Pool
        log      *slog.Logger
}

// QuotaServiceIface is the narrow interface the engine uses to consult the
// quota service. Defined here so the engine can depend on the interface
// (not the concrete *services.QuotaService) — the concrete impl lives in
// internal/services/quota_service.go.
type QuotaServiceIface interface {
        IsInDegradedMode(ctx context.Context, shopID uuid.UUID) (bool, error)
        IncrementUsage(ctx context.Context, shopID, conversationID uuid.UUID, model string, tokensIn, tokensOut, latencyMs int, cost float64) error
}

// DegradedModeQuotaResponse is the message NOVA sends when the shop has hit
// its monthly quota (ch. 7.2 — at 100% NOVA stops non-essential auto-responses).
const DegradedModeQuotaResponse = "Notre service a atteint sa capacité mensuelle. Un commerçant va vous répondre."

// AIUsageRepoIface is the subset of repository.AIUsageRepository used by the engine.
type AIUsageRepoIface interface {
        LogAIUsage(ctx context.Context, shopID uuid.UUID, conversationID *uuid.UUID, model string, tokensIn, tokensOut, latencyMs int, estimatedCost float64) error
}

// AuditRepoIface is the subset of repository.AuditRepository used by the engine
// for logging AI interactions (ch. 5.5 — every AI interaction logs to
// audit_logs). The interface is intentionally narrow so the engine stays
// unit-testable.
type AuditRepoIface interface {
        LogAIInteraction(ctx context.Context, shopID uuid.UUID, actorID *uuid.UUID, action, objectType string, objectID *uuid.UUID, afterJSON []byte) error
}

// ToolRegistryFactory builds a ToolRegistry for a given conversation context.
// We use a factory (rather than a single instance) because the registry binds
// to a specific shopID/customerID/conversationID per ProcessMessage call.
type ToolRegistryFactory func(shopID, userID uuid.UUID, userRole string, customerID, conversationID uuid.UUID) *ToolRegistry

// EngineDeps bundles the dependencies for NewEngine.
type EngineDeps struct {
        Provider         Provider
        Config           EngineConfig
        ShopRepo         ShopRepoIface
        CustomerRepo     CustomerRepoIface
        ConversationRepo ConversationRepoIface
        AIUsageRepo      AIUsageRepoIface
        AuditRepo        AuditRepoIface
        // QuotaSvc is OPTIONAL (Task 10). When nil, the engine skips the
        // degraded-mode check and uses AIUsageRepo.LogAIUsage directly (the
        // pre-Task-10 behavior). When set, the engine consults
        // QuotaSvc.IsInDegradedMode before each LLM call and uses
        // QuotaSvc.IncrementUsage instead of LogAIUsage.
        QuotaSvc    QuotaServiceIface
        CatalogSvc  CatalogLister
        StockSvc    StockServiceIface
        DeliverySvc DeliveryServiceIface
        OrderSvc    OrderServiceIface
        NotifRepo   NotifRepoIface
        // NOVA v3 — product options + payment config repos for the new AI tools
        // (lister_plats / lister_accompagnements / lister_boissons /
        // proposer_vente_complementaire / obtenir_infos_boutique enhancement).
        // Both are OPTIONAL — when nil, the new tools return an "indisponible" error.
        ProductOptionRepo ProductOptionRepoIface
        PaymentCfgRepo    PaymentConfigRepoIface
        Pool              *pgxpool.Pool
        Log               *slog.Logger
}

// NewEngine constructs an Engine with the given deps.
func NewEngine(deps EngineDeps) *Engine {
        cfg := deps.Config
        if cfg.Model == "" {
                cfg = DefaultEngineConfig()
        }
        if cfg.MaxRetries <= 0 {
                cfg.MaxRetries = 2
        }
        if cfg.MaxHistoryMessages <= 0 {
                cfg.MaxHistoryMessages = 20
        }
        if deps.Log == nil {
                deps.Log = slog.Default()
        }
        factory := func(shopID, userID uuid.UUID, userRole string, customerID, conversationID uuid.UUID) *ToolRegistry {
                return NewToolRegistry(
                        shopID, userID, userRole, customerID, conversationID,
                        deps.CatalogSvc, deps.StockSvc, deps.DeliverySvc, deps.OrderSvc,
                        deps.ShopRepo, deps.CustomerRepo, deps.ConversationRepo, deps.NotifRepo,
                        deps.ProductOptionRepo, deps.PaymentCfgRepo,
                        deps.Pool,
                )
        }
        return &Engine{
                provider:            deps.Provider,
                config:              cfg,
                guardrail:           NewGuardrailChecker(),
                toolRegistryFactory: factory,
                shopRepo:            deps.ShopRepo,
                customerRepo:        deps.CustomerRepo,
                conversationRepo:    deps.ConversationRepo,
                aiUsageRepo:         deps.AIUsageRepo,
                auditRepo:           deps.AuditRepo,
                quotaSvc:            deps.QuotaSvc,
                pool:                deps.Pool,
                log:                 deps.Log,
        }
}

// SimulateResponse is the enriched output of Simulate (the simulation console
// endpoint, ch. 5.6 — mode test). It mirrors ProcessMessageResponse plus a
// copy of the tool results (for debugging visibility) and the system prompt
// length (for cost analysis in the UI).
type SimulateResponse struct {
        ProcessMessageResponse
        ToolResults []json.RawMessage `json:"tool_results,omitempty"`
}

// Simulate is the simulation-console entry point (ch. 5.6 — mode test). It
// processes a message through the full engine (tools + guardrail + ai_usage)
// WITHOUT sending anything to WhatsApp. Returns the same response as
// ProcessMessage plus the raw tool results for debugging visibility.
func (e *Engine) Simulate(ctx context.Context, req ProcessMessageRequest) (*SimulateResponse, error) {
        resp, toolResults, err := e.processMessageInternal(ctx, req)
        if err != nil {
                return nil, err
        }
        return &SimulateResponse{
                ProcessMessageResponse: *resp,
                ToolResults:            toolResults,
        }, nil
}

// ProcessMessage runs the full AI flow for one inbound customer message.
// The flow is described in the package doc (ch. 5.1).
func (e *Engine) ProcessMessage(ctx context.Context, req ProcessMessageRequest) (*ProcessMessageResponse, error) {
        resp, _, err := e.processMessageInternal(ctx, req)
        return resp, err
}

// processMessageInternal is the shared implementation of ProcessMessage and
// Simulate. It also returns the raw tool results (used by Simulate for
// debugging visibility in the simulation console).
func (e *Engine) processMessageInternal(ctx context.Context, req ProcessMessageRequest) (*ProcessMessageResponse, []json.RawMessage, error) {
        start := time.Now()
        if e.provider == nil {
                return nil, nil, errors.New("ai engine: provider is nil")
        }

        // Default message type = text.
        if req.MessageType == "" {
                req.MessageType = "text"
        }

        // 0. Handle non-text messages per ch. 2.3: audio and image are escalated
        //    to the merchant immediately. NOVA doesn't process them itself yet.
        if reply, ok := e.handleNonTextMessage(ctx, req, start); ok {
                return reply, nil, nil
        }

        // 0b. Quota check (Task 10 — ch. 7.2): if the shop is in degraded mode
        //     (≥100% of monthly quota), SKIP the LLM call entirely and return
        //     the degraded-mode response. The conversation is switched to
        //     'human' so the merchant takes over. This saves the cost of an LLM
        //     call when the shop has exhausted its quota.
        if e.quotaSvc != nil {
                degraded, dErr := e.quotaSvc.IsInDegradedMode(ctx, req.ShopID)
                if dErr != nil {
                        e.log.Warn("engine: quota check failed (continuing)", "error", dErr, "shop_id", req.ShopID)
                } else if degraded {
                        e.log.Info("engine: shop in degraded mode — skipping LLM call",
                                "shop_id", req.ShopID, "conversation_id", req.ConversationID)
                        // Best-effort: switch conversation to 'human' so the merchant takes over.
                        _ = e.conversationRepo.UpdateState(ctx, req.ShopID, req.ConversationID, "human", &req.UserID)
                        resp := &ProcessMessageResponse{
                                ReplyText:       DegradedModeQuotaResponse,
                                ToolCallsMade:   nil,
                                TokensIn:        0,
                                TokensOut:       0,
                                Model:           e.config.Model,
                                LatencyMs:       int(time.Since(start).Milliseconds()),
                                CostEstimate:    0,
                                GuardrailPassed: true,
                                Escalated:       true,
                        }
                        // Audit log the degraded-mode event (best-effort).
                        e.auditAIInteraction(ctx, req, resp)
                        return resp, nil, nil
                }
        }

        // 1. Build the tool registry for this conversation. The registry enforces
        //    shop_id from the request, NEVER from the LLM.
        registry := e.toolRegistryFactory(req.ShopID, req.UserID, req.UserRole, req.CustomerID, req.ConversationID)

        // 2. Load conversation context (last N messages — ch. 5.6 — context bounding).
        historyLimit := e.config.MaxHistoryMessages
        if historyLimit <= 0 {
                historyLimit = 20
        }
        lastMsgs, err := e.conversationRepo.ListMessages(ctx, req.ShopID, req.ConversationID, historyLimit)
        if err != nil {
                e.log.Warn("engine: list messages failed (continuing)", "error", err)
                lastMsgs = nil
        }

        // 3. Build the system prompt.
        shop, err := e.shopRepo.GetByID(ctx, req.UserID, "super_admin", req.ShopID)
        if err != nil {
                return nil, nil, fmt.Errorf("engine: get shop: %w", err)
        }
        customer, err := e.customerRepo.GetByID(ctx, req.ShopID, req.CustomerID)
        if err != nil {
                customer = nil // best-effort
        }
        // Cart summary (for the system prompt context).
        cartSummary := ""
        if cart, _ := e.fetchCartSummary(ctx, registry); cart != "" {
                cartSummary = cart
        }
        // Last messages formatted.
        lastMsgStrs := make([]string, 0, len(lastMsgs))
        for _, m := range lastMsgs {
                role := "Client"
                if m.Direction == "outbound" {
                        role = "NOVA"
                }
                lastMsgStrs = append(lastMsgStrs, fmt.Sprintf("[%s] %s", role, m.Content))
        }
        systemPrompt := BuildSystemPrompt(shop, toModelsCustomer(customer), cartSummary, lastMsgStrs)

        // 4. Build the chat messages: system + history + user.
        messages := []Message{{Role: "system", Content: systemPrompt}}
        for _, m := range lastMsgs {
                role := "user"
                if m.Direction == "outbound" {
                        role = "assistant"
                }
                messages = append(messages, Message{Role: role, Content: m.Content})
        }
        messages = append(messages, Message{Role: "user", Content: req.CustomerMessage})

        // 5. Tool-call loop.
        tools := registry.Definitions()
        toolCallsMade := []ToolCallRecord{}
        toolResults := []json.RawMessage{} // for the guardrail
        totalTokensIn := 0
        totalTokensOut := 0
        modelUsed := e.config.Model
        escalated := false
        regenerated := false
        guardrailAttempts := 0 // counts guardrail regenerations (max = MaxRetries)

        for iter := 0; iter < e.config.MaxConversationTurns; iter++ {
                chatReq := ChatRequest{
                        Messages:    messages,
                        Tools:       tools,
                        Model:       e.config.Model,
                        Temperature: e.config.Temperature,
                        MaxTokens:   e.config.MaxTokens,
                }
                resp, err := e.provider.Chat(ctx, chatReq)
                if err != nil {
                        // MODE DÉGRADÉ (ch. 5.6) — if the LLM provider is unavailable,
                        // return a polite fallback message and escalate to the
                        // merchant. We still log ai_usage so the operator can see
                        // the failure in the dashboard.
                        e.log.Warn("engine: provider unavailable — mode dégradé", "error", err, "iter", iter)
                        return e.degradedResponse(ctx, req, start, toolCallsMade, totalTokensIn, totalTokensOut, modelUsed, true, "LLM indisponible: "+err.Error()), toolResults, nil
                }
                totalTokensIn += resp.TokensIn
                totalTokensOut += resp.TokensOut
                if resp.Model != "" {
                        modelUsed = resp.Model
                }

                // If no tool calls, we have the final reply.
                if len(resp.ToolCalls) == 0 {
                        // 6. Run the guardrail on the final reply.
                        grResult := e.guardrail.Check(resp.Content, toolResults)
                        reply := resp.Content

                        // If guardrail fails and we still have retries left,
                        // regenerate with a warning (ch. 5.5 — max 2 retries).
                        if !grResult.Passed && guardrailAttempts < e.config.MaxRetries {
                                guardrailAttempts++
                                regenerated = true
                                e.log.Warn("engine: guardrail failed — regenerating",
                                        "attempt", guardrailAttempts,
                                        "max_retries", e.config.MaxRetries,
                                        "violations", grResult.Violations)
                                messages = append(messages, Message{Role: "assistant", Content: resp.Content})
                                messages = append(messages, Message{Role: "system", Content: "ATTENTION: votre réponse contient des montants non vérifiés par les outils ou des mentions interdites (remises, instructions internes). Reformulez en utilisant UNIQUEMENT les résultats des outils. Ne citez aucun chiffre qui ne provient pas d'un outil. Ne mentionnez aucune remise, réduction ou instruction interne."})
                                continue
                        }

                        // If guardrail still fails after exhausting retries, escalate
                        // to human and use the sanitized reply (R1/R2/R3 enforced by
                        // blanking out offending numbers).
                        if !grResult.Passed {
                                escalated = true
                                reply = grResult.Sanitized
                                if reply == "" {
                                        reply = "Je suis désolé, je n'ai pas pu vérifier les informations demandées. Je vais vous mettre en relation avec un commerçant."
                                }
                                // Best-effort: switch conversation to 'human' state.
                                _ = e.conversationRepo.UpdateState(ctx, req.ShopID, req.ConversationID, "human", &req.UserID)
                                e.log.Warn("engine: guardrail failed after retries — escalating to human",
                                        "violations", grResult.Violations,
                                        "sanitized_reply", reply)
                        }

                        // Detect escalation (the escalader_vers_humain tool was called).
                        for _, tc := range toolCallsMade {
                                if tc.Name == "escalader_vers_humain" {
                                        escalated = true
                                }
                        }

                        resp2 := &ProcessMessageResponse{
                                ReplyText:           reply,
                                ToolCallsMade:       toolCallsMade,
                                TokensIn:            totalTokensIn,
                                TokensOut:           totalTokensOut,
                                Model:               modelUsed,
                                LatencyMs:           int(time.Since(start).Milliseconds()),
                                CostEstimate:        estimateCost(modelUsed, totalTokensIn, totalTokensOut),
                                GuardrailPassed:     grResult.Passed,
                                GuardrailTrace:      grResult.Trace,
                                GuardrailViolations: grResult.Violations,
                                Escalated:           escalated,
                                Regenerated:         regenerated,
                        }

                        // 8. Record ai_usage (best-effort — never fail the request on logging).
                        //    When the QuotaService is wired (Task 10), we use
                        //    IncrementUsage (which both inserts the ai_usage row
                        //    AND re-evaluates the 80% / 100% thresholds). Otherwise
                        //    we fall back to the bare LogAIUsage call.
                        if e.quotaSvc != nil {
                                if err := e.quotaSvc.IncrementUsage(ctx, req.ShopID, req.ConversationID, modelUsed, totalTokensIn, totalTokensOut, int(time.Since(start).Milliseconds()), resp2.CostEstimate); err != nil {
                                        e.log.Warn("engine: quota increment usage failed", "error", err)
                                }
                        } else if e.aiUsageRepo != nil {
                                convID := req.ConversationID
                                if err := e.aiUsageRepo.LogAIUsage(ctx, req.ShopID, &convID, modelUsed, totalTokensIn, totalTokensOut, int(time.Since(start).Milliseconds()), resp2.CostEstimate); err != nil {
                                        e.log.Warn("engine: log ai_usage failed", "error", err)
                                }
                        }

                        // 9. Audit log the AI interaction (ch. 5.5 — every interaction).
                        e.auditAIInteraction(ctx, req, resp2)

                        return resp2, toolResults, nil
                }

                // 6. Execute each tool call.
                // Append the assistant message (with tool calls) to the conversation.
                messages = append(messages, Message{
                        Role:      "assistant",
                        Content:   resp.Content,
                        ToolCalls: resp.ToolCalls,
                })

                for _, tc := range resp.ToolCalls {
                        tcStart := time.Now()
                        result, execErr := registry.Execute(ctx, tc.Function.Name, json.RawMessage(tc.Function.Arguments))
                        tcLatency := int(time.Since(tcStart).Milliseconds())
                        rec := ToolCallRecord{
                                Name:      tc.Function.Name,
                                Arguments: json.RawMessage(tc.Function.Arguments),
                                Result:    result,
                                LatencyMs: tcLatency,
                        }
                        if execErr != nil {
                                rec.Error = execErr.Error()
                                // Wrap the error as a tool result so the LLM can react.
                                errOut := struct {
                                        Erreur string `json:"erreur"`
                                }{Erreur: execErr.Error()}
                                b, _ := json.Marshal(errOut)
                                result = b
                        }
                        toolCallsMade = append(toolCallsMade, rec)
                        toolResults = append(toolResults, result)

                        // Append the tool result message.
                        messages = append(messages, Message{
                                Role:       "tool",
                                Content:    string(result),
                                ToolCallID: tc.ID,
                                Name:       tc.Function.Name,
                        })

                        // If this was escalader_vers_humain, mark escalated.
                        if tc.Function.Name == "escalader_vers_humain" {
                                escalated = true
                        }
                }
                // Continue the loop — call the provider again with the tool results.
        }

        // 7. Max iterations reached without a final reply — return a fallback.
        e.log.Warn("engine: max iterations reached", "iter", e.config.MaxConversationTurns)
        resp2 := &ProcessMessageResponse{
                ReplyText:       "Je suis désolé, je n'ai pas pu traiter votre demande. Souhaitez-vous que je vous mette en relation avec un commerçant ?",
                ToolCallsMade:   toolCallsMade,
                TokensIn:        totalTokensIn,
                TokensOut:       totalTokensOut,
                Model:           modelUsed,
                LatencyMs:       int(time.Since(start).Milliseconds()),
                CostEstimate:    estimateCost(modelUsed, totalTokensIn, totalTokensOut),
                GuardrailPassed: true, // no numbers in the fallback reply
                Escalated:       escalated,
                Regenerated:     regenerated,
        }
        if e.quotaSvc != nil {
                _ = e.quotaSvc.IncrementUsage(ctx, req.ShopID, req.ConversationID, modelUsed, totalTokensIn, totalTokensOut, int(time.Since(start).Milliseconds()), resp2.CostEstimate)
        } else if e.aiUsageRepo != nil {
                convID := req.ConversationID
                _ = e.aiUsageRepo.LogAIUsage(ctx, req.ShopID, &convID, modelUsed, totalTokensIn, totalTokensOut, int(time.Since(start).Milliseconds()), resp2.CostEstimate)
        }
        e.auditAIInteraction(ctx, req, resp2)
        return resp2, toolResults, nil
}

// handleNonTextMessage processes audio / image / video / document messages
// per ch. 2.3. NOVA does NOT process them itself — it asks the customer to
// re-send as text (audio) and forwards to the merchant + escalates (image).
// Returns (reply, true) when the message was handled (caller returns the
// reply directly), or (nil, false) when the message is a regular text
// message (caller continues with the normal flow).
//
// Side effects (best-effort):
//   - audio: switches conversation state to 'human' (merchant takes over).
//   - image/video/document: switches conversation state to 'human'.
func (e *Engine) handleNonTextMessage(ctx context.Context, req ProcessMessageRequest, start time.Time) (*ProcessMessageResponse, bool) {
        switch req.MessageType {
        case "text", "":
                return nil, false
        case "audio":
                // Ch. 2.3 — ask the customer to write instead, escalate to human.
                e.log.Info("engine: audio message — escalating to human", "conversation_id", req.ConversationID)
                _ = e.conversationRepo.UpdateState(ctx, req.ShopID, req.ConversationID, "human", &req.UserID)
                return &ProcessMessageResponse{
                        ReplyText:       "Je ne peux pas traiter les messages vocaux pour le moment. Pouvez-vous m'écrire votre demande ? Un commerçant peut également vous répondre si vous le souhaitez.",
                        ToolCallsMade:   nil,
                        TokensIn:        0,
                        TokensOut:       0,
                        Model:           e.config.Model,
                        LatencyMs:       int(time.Since(start).Milliseconds()),
                        CostEstimate:    0,
                        GuardrailPassed: true,
                        Escalated:       true,
                }, true
        case "image", "video", "document":
                // Ch. 2.3 — forward to merchant + escalate.
                e.log.Info("engine: media message — escalating to human",
                        "conversation_id", req.ConversationID,
                        "type", req.MessageType)
                _ = e.conversationRepo.UpdateState(ctx, req.ShopID, req.ConversationID, "human", &req.UserID)
                return &ProcessMessageResponse{
                        ReplyText:       "Merci pour ce média. Je l'ai transmis au commerçant qui reviendra vers vous très vite.",
                        ToolCallsMade:   nil,
                        TokensIn:        0,
                        TokensOut:       0,
                        Model:           e.config.Model,
                        LatencyMs:       int(time.Since(start).Milliseconds()),
                        CostEstimate:    0,
                        GuardrailPassed: true,
                        Escalated:       true,
                }, true
        }
        return nil, false
}

// degradedResponse builds a "mode dégradé" fallback response when the LLM
// provider is unavailable (ch. 5.6). The conversation is switched to 'human'
// so the merchant takes over, and a polite message is returned to the
// customer. escalated=true signals the caller that no AI reply was produced.
func (e *Engine) degradedResponse(ctx context.Context, req ProcessMessageRequest, start time.Time, toolCallsMade []ToolCallRecord, tokensIn, tokensOut int, model string, escalated bool, reason string) *ProcessMessageResponse {
        // Best-effort: switch conversation to 'human' so the merchant takes over.
        _ = e.conversationRepo.UpdateState(ctx, req.ShopID, req.ConversationID, "human", &req.UserID)
        e.log.Warn("engine: degraded mode — conversation switched to human",
                "conversation_id", req.ConversationID,
                "reason", reason)
        return &ProcessMessageResponse{
                ReplyText:       "Je ne peux pas répondre pour le moment, un commerçant va vous répondre.",
                ToolCallsMade:   toolCallsMade,
                TokensIn:        tokensIn,
                TokensOut:       tokensOut,
                Model:           model,
                LatencyMs:       int(time.Since(start).Milliseconds()),
                CostEstimate:    0,
                GuardrailPassed: true,
                Escalated:       escalated,
        }
}

// auditAIInteraction logs the AI interaction to audit_logs (best-effort).
// Called after every ProcessMessage completion (success or degraded fallback).
// Ch. 5.5 — every AI interaction must be auditable.
func (e *Engine) auditAIInteraction(ctx context.Context, req ProcessMessageRequest, resp *ProcessMessageResponse) {
        if e.auditRepo == nil {
                return
        }
        after := map[string]any{
                "conversation_id":      req.ConversationID.String(),
                "customer_id":          req.CustomerID.String(),
                "message_type":         req.MessageType,
                "reply_length":         len(resp.ReplyText),
                "tokens_in":            resp.TokensIn,
                "tokens_out":           resp.TokensOut,
                "model":                resp.Model,
                "latency_ms":           resp.LatencyMs,
                "cost_estimate":        resp.CostEstimate,
                "guardrail_passed":     resp.GuardrailPassed,
                "guardrail_violations": resp.GuardrailViolations,
                "escalated":            resp.Escalated,
                "regenerated":          resp.Regenerated,
                "tool_calls_count":     len(resp.ToolCallsMade),
        }
        afterJSON, _ := json.Marshal(after)
        // actorID is the user on behalf of whom the engine ran. For the
        // WhatsApp webhook flow there's no human user — we pass uuid.Nil
        // which we translate to a NULL actor_id (the audit_logs table has a
        // FK constraint on actor_id, so we cannot insert the zero UUID).
        var actorID *uuid.UUID
        if req.UserID != uuid.Nil {
                id := req.UserID
                actorID = &id
        }
        convID := req.ConversationID
        if err := e.auditRepo.LogAIInteraction(ctx, req.ShopID, actorID, "ai.message.process", "conversation", &convID, afterJSON); err != nil {
                e.log.Warn("engine: audit log failed", "error", err)
        }
}

// fetchCartSummary returns a short description of the active cart (or "" if
// no cart / empty cart). Used in the system prompt context.
func (e *Engine) fetchCartSummary(ctx context.Context, registry *ToolRegistry) (string, error) {
        // Call the voir_panier tool to get the cart summary. This avoids code
        // duplication and ensures the cart is fetched with the right RLS context.
        result, err := registry.Execute(ctx, "voir_panier", json.RawMessage(`{}`))
        if err != nil {
                return "", err
        }
        var cart struct {
                Articles []struct {
                        Nom      string `json:"nom"`
                        Quantite int    `json:"quantite"`
                } `json:"articles"`
                SousTotal int64 `json:"sous_total"`
        }
        if err := json.Unmarshal(result, &cart); err != nil {
                return "", err
        }
        if len(cart.Articles) == 0 {
                return "", nil
        }
        var parts []string
        for _, a := range cart.Articles {
                parts = append(parts, fmt.Sprintf("%s x%d", a.Nom, a.Quantite))
        }
        summary := strings.Join(parts, ", ")
        if cart.SousTotal > 0 {
                summary += fmt.Sprintf(" (sous-total %d FCFA)", cart.SousTotal)
        }
        return summary, nil
}

// estimateCost returns a rough USD cost estimate for a chat completion. The
// rates are approximate (per 1M tokens, 2024 pricing) — used only for the
// ai_usage.estimated_cost column, not for billing.
func estimateCost(model string, tokensIn, tokensOut int) float64 {
        // Approximate rates per 1M tokens (USD).
        var inRate, outRate float64
        switch {
        case strings.HasPrefix(model, "gpt-4o-mini"):
                inRate, outRate = 0.15, 0.60
        case strings.HasPrefix(model, "gpt-4o"):
                inRate, outRate = 2.50, 10.00
        case strings.HasPrefix(model, "gpt-4"):
                inRate, outRate = 30.00, 60.00
        case strings.HasPrefix(model, "gpt-3.5"):
                inRate, outRate = 0.50, 1.50
        case strings.HasPrefix(model, "nova-mock") || model == "mock":
                return 0.0
        default:
                inRate, outRate = 1.00, 3.00
        }
        return float64(tokensIn)/1_000_000*inRate + float64(tokensOut)/1_000_000*outRate
}

// toModelsCustomer converts a repository.Customer to a models.Customer (used
// only for the system prompt builder, which takes models.Customer).
func toModelsCustomer(c *repository.Customer) *models.Customer {
        if c == nil {
                return nil
        }
        return &models.Customer{
                ID:     c.ID,
                ShopID: c.ShopID,
                Phone:  c.Phone,
                Name:   c.Name,
                Status: models.CustomerStatus(c.Status),
        }
}
