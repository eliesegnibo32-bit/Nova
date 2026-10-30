// Message repository (spec: internal/repository/messages.go).
//
// This is the spec-required MessageRepository facade. Messages live in the
// `messages` table (migration 005) and are also managed through
// ConversationRepository (AddMessage / UpdateMessageStatus / MessageByWAMID /
// WAMIDExists / UpdateMessageWAMID / ListMessages). The spec asks for a
// dedicated MessageRepository, so this file provides one — but the
// implementation delegates to the same DB queries so behavior is identical.
//
// Why two facades over the same table? The ConversationRepository groups
// conversation + message operations (a conversation owns its messages). The
// MessageRepository is a message-only view useful for status-tracking
// workflows (e.g. the WhatsApp webhook status updater) that don't need the
// conversation context.
//
// Both repos enforce RLS via db.WithTenantTx — the caller passes the shopID
// so the tenant context is set correctly.
package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"nova-api/internal/db"
)

// MessageRepository wraps the messages table for status-tracking workflows.
// It does NOT own the conversations table — that's ConversationRepository.
// Both repos are safe to use concurrently.
type MessageRepository struct {
	pool *pgxpool.Pool
}

// NewMessageRepository returns a MessageRepository bound to the pool.
func NewMessageRepository(pool *pgxpool.Pool) *MessageRepository {
	return &MessageRepository{pool: pool}
}

// CreateInput is the input to MessageRepository.Create. WAMID is optional
// (NULL for outbound messages that haven't been sent to Meta yet).
type CreateInput struct {
	ConversationID uuid.UUID
	ShopID         uuid.UUID
	Direction      string // inbound | outbound
	Type           string // text | image | audio | ...
	Content        string
	WAMID          string // optional
}

// Create inserts one message row. Wraps ConversationRepository.AddMessage so
// the dedup UNIQUE constraint on wamid is enforced. Returns the inserted row.
//
// The 24h window is updated when direction=inbound (delegates to
// ConversationRepository.AddMessage which does this internally).
func (r *MessageRepository) Create(ctx context.Context, shopID, conversationID uuid.UUID, msg CreateInput) (*Message, error) {
	convRepo := &ConversationRepository{pool: r.pool}
	direction := msg.Direction
	if direction == "" {
		direction = MsgInbound
	}
	msgType := msg.Type
	if msgType == "" {
		msgType = "text"
	}
	return convRepo.AddMessage(ctx, shopID, conversationID, direction, msgType, msg.Content, msg.WAMID)
}

// UpdateStatus updates the delivery status of an outbound message by wamid.
// status is one of: queued | sent | delivered | read | failed. Returns
// ErrMessageNotFound when no message matches the wamid.
//
// Wraps ConversationRepository.UpdateMessageStatus.
func (r *MessageRepository) UpdateStatus(ctx context.Context, shopID uuid.UUID, wamid, status string) error {
	convRepo := &ConversationRepository{pool: r.pool}
	return convRepo.UpdateMessageStatus(ctx, shopID, wamid, status)
}

// GetByWamid returns the message with the given wamid (WhatsApp Message ID).
// Returns ErrMessageNotFound when no message matches.
//
// Wraps ConversationRepository.MessageByWAMID.
func (r *MessageRepository) GetByWamid(ctx context.Context, shopID uuid.UUID, wamid string) (*Message, error) {
	convRepo := &ConversationRepository{pool: r.pool}
	return convRepo.MessageByWAMID(ctx, shopID, wamid)
}

// ListByConversation returns the most recent messages for a conversation,
// oldest first (natural chat reading order). limit is capped at 200.
//
// Wraps ConversationRepository.ListMessages.
func (r *MessageRepository) ListByConversation(ctx context.Context, shopID, conversationID uuid.UUID, limit int) ([]Message, error) {
	convRepo := &ConversationRepository{pool: r.pool}
	return convRepo.ListMessages(ctx, shopID, conversationID, limit)
}

// WAMIDExists returns true if a message with the given wamid already exists
// for this shop. Cheap dedup check (ch. 6 — Meta retries webhook delivery).
func (r *MessageRepository) WAMIDExists(ctx context.Context, shopID uuid.UUID, wamid string) (bool, error) {
	if wamid == "" {
		return false, nil
	}
	var exists bool
	err := db.WithTenantTx(ctx, r.pool, &shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM messages WHERE shop_id = $1 AND wamid = $2)`, shopID, wamid).Scan(&exists)
	})
	if err != nil {
		return false, fmt.Errorf("message repo: wamid exists: %w", err)
	}
	return exists, nil
}

// UpdateWAMID fills in the wamid on a previously-stored outbound message
// (after the WhatsApp send returned a wamid from Meta).
func (r *MessageRepository) UpdateWAMID(ctx context.Context, shopID, messageID uuid.UUID, wamid string) error {
	convRepo := &ConversationRepository{pool: r.pool}
	return convRepo.UpdateMessageWAMID(ctx, shopID, messageID, wamid)
}
