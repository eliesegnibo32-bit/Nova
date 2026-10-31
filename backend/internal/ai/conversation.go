// Conversation manager — thin facade over ConversationRepository that exposes
// the operations used by the AI engine, the simulation console, and the
// merchant takeover flow (ch. 4.8 — Historique des conversations et
// intervention humaine).
//
// The ConversationRepository already implements every operation we need; this
// manager is a higher-level convenience that bundles a few related calls
// (e.g. TakeOver = SetState('human', takenOverBy)) and provides a stable
// interface for the AI engine to depend on (without pulling in the entire
// repository package).
package ai

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"nova-api/internal/repository"
)

// ConversationManager bundles the conversation-related operations the engine
// and the HTTP handlers need. It is a thin wrapper over the
// ConversationRepository — the repository is the source of truth for the SQL.
type ConversationManager struct {
	repo *repository.ConversationRepository
	pool *pgxpool.Pool
}

// NewConversationManager constructs a ConversationManager.
func NewConversationManager(repo *repository.ConversationRepository, pool *pgxpool.Pool) *ConversationManager {
	return &ConversationManager{repo: repo, pool: pool}
}

// GetOrCreate returns the conversation for (shopID, customerID, channel), or
// creates a new one in state='ai'. Delegates to the repository.
func (m *ConversationManager) GetOrCreate(ctx context.Context, shopID, customerID uuid.UUID, channel string) (*repository.Conversation, error) {
	if m == nil || m.repo == nil {
		return nil, fmt.Errorf("conversation manager: not initialized")
	}
	return m.repo.GetOrCreate(ctx, shopID, customerID, channel)
}

// GetByID returns the conversation for the given shop+id.
func (m *ConversationManager) GetByID(ctx context.Context, shopID, conversationID uuid.UUID) (*repository.Conversation, error) {
	return m.repo.GetByID(ctx, shopID, conversationID)
}

// ListByShop returns a paginated list of conversations for the shop.
func (m *ConversationManager) ListByShop(ctx context.Context, shopID uuid.UUID, params repository.ListConversationsParams) ([]repository.ConversationListItem, int64, error) {
	return m.repo.ListByShop(ctx, shopID, params)
}

// ListToTakeOver returns conversations in 'human' state for the dashboard
// "à reprendre" widget.
func (m *ConversationManager) ListToTakeOver(ctx context.Context, shopID uuid.UUID) ([]repository.ConversationListItem, error) {
	return m.repo.ListToTakeOver(ctx, shopID)
}

// SetState changes the conversation state. state is "ai", "human", or "closed".
// takenOverBy is required when state="human".
func (m *ConversationManager) SetState(ctx context.Context, shopID, conversationID uuid.UUID, state string, takenOverBy *uuid.UUID) error {
	return m.repo.UpdateState(ctx, shopID, conversationID, state, takenOverBy)
}

// TakeOver transitions a conversation to 'human' state, taken over by userID.
// After this, the AI will NOT process incoming messages from this conversation
// (they're buffered for the merchant). Ch. 4.8 — reprise humaine.
func (m *ConversationManager) TakeOver(ctx context.Context, shopID, conversationID, userID uuid.UUID) error {
	return m.repo.UpdateState(ctx, shopID, conversationID, "human", &userID)
}

// ReturnToAI transitions a conversation back to 'ai' state ("Rendre à NOVA").
// After this, the AI resumes processing incoming messages with full context.
// Ch. 4.8 — the AI picks up where the merchant left off, using the persisted
// messages as context.
func (m *ConversationManager) ReturnToAI(ctx context.Context, shopID, conversationID, userID uuid.UUID) error {
	// userID is unused in the SQL update (taken_over_by = NULL) but we accept
	// it for symmetry with TakeOver and for audit logging at a higher level.
	_ = userID
	return m.repo.UpdateState(ctx, shopID, conversationID, "ai", nil)
}

// Close transitions a conversation to 'closed' state. The AI will not process
// further messages; the customer must start a new conversation if they want
// to order again.
func (m *ConversationManager) Close(ctx context.Context, shopID, conversationID uuid.UUID) error {
	return m.repo.UpdateState(ctx, shopID, conversationID, "closed", nil)
}

// AddMessage inserts one message (inbound or outbound). Delegates to the repo.
func (m *ConversationManager) AddMessage(ctx context.Context, shopID, conversationID uuid.UUID, direction, msgType, content, wamid string) (*repository.Message, error) {
	return m.repo.AddMessage(ctx, shopID, conversationID, direction, msgType, content, wamid)
}

// GetHistory returns the most recent messages for a conversation, oldest first.
func (m *ConversationManager) GetHistory(ctx context.Context, shopID, conversationID uuid.UUID, limit int) ([]repository.Message, error) {
	return m.repo.ListMessages(ctx, shopID, conversationID, limit)
}

// UpdateSummary saves the AI-generated conversation summary (used for context
// bounding — ch. 5.6 — and for handover to the merchant). Aliases
// ConversationRepository.SetSummary for naming consistency with the spec.
func (m *ConversationManager) UpdateSummary(ctx context.Context, shopID, conversationID uuid.UUID, summary string) error {
	return m.repo.SetSummary(ctx, shopID, conversationID, summary)
}

// UpdateWindow24h extends the 24h WhatsApp response window.
func (m *ConversationManager) UpdateWindow24h(ctx context.Context, shopID, conversationID uuid.UUID, expiresAt time.Time) error {
	return m.repo.UpdateWindow24h(ctx, shopID, conversationID, expiresAt)
}
