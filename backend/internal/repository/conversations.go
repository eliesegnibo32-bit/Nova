// Conversation + message repository (ch. 4.5 — migration 005).
//
// Tables:
//   - conversations: fil de discussion (state: ai | human | closed, taken_over_by,
//     window_24h_expires_at, summary).
//   - messages: messages individuels (direction inbound/outbound, type text/image/...
//     wamid unique, status queued/sent/delivered/read/failed).
//
// All queries go through db.WithTenantTx so the RLS policies from migration 011
// are enforced. shopID comes from the authenticated session.
package repository

import (
        "context"
        "errors"
        "fmt"
        "time"

        "github.com/google/uuid"
        "github.com/jackc/pgx/v5"
        "github.com/jackc/pgx/v5/pgxpool"

        "nova-api/internal/db"
        "nova-api/internal/models"
)

// Conversation repository sentinels.
var (
        ErrConversationNotFound = errors.New("conversation not found")
        ErrMessageNotFound      = errors.New("message not found")
)

// MessageDirection values.
const (
        MsgInbound  = "inbound"
        MsgOutbound = "outbound"
)

// ConversationRepository wraps the conversations + messages tables.
type ConversationRepository struct {
        pool *pgxpool.Pool
}

// NewConversationRepository returns a ConversationRepository bound to the pool.
func NewConversationRepository(pool *pgxpool.Pool) *ConversationRepository {
        return &ConversationRepository{pool: pool}
}

// Conversation is the persisted conversation row.
type Conversation struct {
        ID                 uuid.UUID  `json:"id"`
        ShopID             uuid.UUID  `json:"shop_id"`
        CustomerID         uuid.UUID  `json:"customer_id"`
        Channel            string     `json:"channel"`
        State              string     `json:"state"` // ai | human | closed
        Window24hExpiresAt *time.Time `json:"window_24h_expires_at,omitempty"`
        TakenOverBy        *uuid.UUID `json:"taken_over_by,omitempty"`
        Summary            *string    `json:"summary,omitempty"`
        CreatedAt          time.Time  `json:"created_at"`
        UpdatedAt          time.Time  `json:"updated_at"`
}

// Message is a persisted message row.
type Message struct {
        ID             uuid.UUID  `json:"id"`
        ConversationID uuid.UUID  `json:"conversation_id"`
        ShopID         uuid.UUID  `json:"shop_id"`
        Direction      string     `json:"direction"`
        Type           string     `json:"type"`
        Content        string     `json:"content"`
        WAMID          *string    `json:"wamid,omitempty"`
        Status         string     `json:"status"`
        CreatedAt      time.Time  `json:"created_at"`
        DeliveredAt    *time.Time `json:"delivered_at,omitempty"`
        ReadAt         *time.Time `json:"read_at,omitempty"`
}

// ConversationListItem is the list-view shape joined with the customer.
type ConversationListItem struct {
        Conversation
        CustomerName       *string    `json:"customer_name,omitempty"`
        CustomerPhone      string     `json:"customer_phone"`
        LastMessageAt      *time.Time `json:"last_message_at,omitempty"`
        LastMessagePreview *string    `json:"last_message_preview,omitempty"`
        UnreadCount        int        `json:"unread_count"`
}

// ListConversationsParams holds the query parameters for ListByShop.
type ListConversationsParams struct {
        Page   int
        Limit  int
        State  string // optional filter
        Search string // optional: search customer name or phone
}

// Normalize fills sane defaults.
func (p *ListConversationsParams) Normalize() {
        if p.Page < 1 {
                p.Page = 1
        }
        if p.Limit < 1 || p.Limit > 200 {
                p.Limit = 20
        }
}

// Offset returns the SQL OFFSET.
func (p *ListConversationsParams) Offset() int { return (p.Page - 1) * p.Limit }

// GetOrCreate returns the conversation for (shopID, customerID, channel), or
// creates a new one in state='ai'. The 24h window is set to now+24h on creation.
// This is the only place we INSERT into conversations; all other methods are
// read/update.
func (r *ConversationRepository) GetOrCreate(ctx context.Context, shopID, customerID uuid.UUID, channel string) (*Conversation, error) {
        if channel == "" {
                channel = "whatsapp"
        }
        var conv Conversation
        err := db.WithTenantTx(ctx, r.pool, &shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
                // Try to fetch an existing conversation for this shop+customer+channel.
                const selQ = `
                        SELECT id, shop_id, customer_id, channel, state, window_24h_expires_at, taken_over_by, summary, created_at, updated_at
                          FROM conversations
                         WHERE shop_id = $1 AND customer_id = $2 AND channel = $3
                         ORDER BY updated_at DESC
                         LIMIT 1
                `
                err := scanConversation(tx.QueryRow(ctx, selQ, shopID, customerID, channel), &conv)
                if err == nil {
                        return nil
                }
                if !errors.Is(err, pgx.ErrNoRows) {
                        return fmt.Errorf("conv repo: get or create: select: %w", err)
                }
                // Insert a new conversation in state='ai'.
                const insQ = `
                        INSERT INTO conversations (shop_id, customer_id, channel, state, window_24h_expires_at)
                        VALUES ($1, $2, $3, 'ai', now() + interval '24 hours')
                        RETURNING id, shop_id, customer_id, channel, state, window_24h_expires_at, taken_over_by, summary, created_at, updated_at
                `
                if err := scanConversation(tx.QueryRow(ctx, insQ, shopID, customerID, channel), &conv); err != nil {
                        return fmt.Errorf("conv repo: get or create: insert: %w", err)
                }
                return nil
        })
        if err != nil {
                return nil, err
        }
        return &conv, nil
}

// GetByID returns the conversation for the given shop+id.
func (r *ConversationRepository) GetByID(ctx context.Context, shopID, id uuid.UUID) (*Conversation, error) {
        var conv Conversation
        err := db.WithTenantTx(ctx, r.pool, &shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
                const q = `
                        SELECT id, shop_id, customer_id, channel, state, window_24h_expires_at, taken_over_by, summary, created_at, updated_at
                          FROM conversations
                         WHERE shop_id = $1 AND id = $2
                `
                return scanConversation(tx.QueryRow(ctx, q, shopID, id), &conv)
        })
        if err != nil {
                if errors.Is(err, pgx.ErrNoRows) {
                        return nil, ErrConversationNotFound
                }
                return nil, fmt.Errorf("conv repo: get by id: %w", err)
        }
        return &conv, nil
}

// UpdateState changes the conversation state. state is "ai", "human", or "closed".
// takenOverBy is required when state="human" (the user who takes over).
func (r *ConversationRepository) UpdateState(ctx context.Context, shopID, id uuid.UUID, state string, takenOverBy *uuid.UUID) error {
        return db.WithTenantTx(ctx, r.pool, &shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
                var takenByArg any
                if takenOverBy != nil {
                        takenByArg = *takenOverBy
                }
                const q = `
                        UPDATE conversations
                           SET state = $3, taken_over_by = $4, updated_at = now()
                         WHERE shop_id = $1 AND id = $2
                `
                ct, err := tx.Exec(ctx, q, shopID, id, state, takenByArg)
                if err != nil {
                        return fmt.Errorf("conv repo: update state: %w", err)
                }
                if ct.RowsAffected() == 0 {
                        return ErrConversationNotFound
                }
                return nil
        })
}

// UpdateWindow24h extends the 24h response window to expiresAt (used on each
// inbound customer message).
func (r *ConversationRepository) UpdateWindow24h(ctx context.Context, shopID, id uuid.UUID, expiresAt time.Time) error {
        return db.WithTenantTx(ctx, r.pool, &shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
                const q = `UPDATE conversations SET window_24h_expires_at = $3, updated_at = now() WHERE shop_id = $1 AND id = $2`
                ct, err := tx.Exec(ctx, q, shopID, id, expiresAt)
                if err != nil {
                        return fmt.Errorf("conv repo: update window: %w", err)
                }
                if ct.RowsAffected() == 0 {
                        return ErrConversationNotFound
                }
                return nil
        })
}

// GetOrCreateByCustomerPhone looks up the most-recent conversation for the
// given shop + customer phone + channel (default 'whatsapp'), or creates a
// new one in state='ai'. Used by the WhatsApp webhook processor: it first
// resolves the customer by phone, then attaches the conversation to that
// customer.
//
// Equivalent to GetOrCreate(shopID, customerID, channel) but takes a phone
// directly so callers don't have to do the customer lookup themselves. When
// no conversation exists for (shopID, customerID, channel), one is inserted
// with window_24h_expires_at = now() + 24h.
func (r *ConversationRepository) GetOrCreateByCustomerPhone(ctx context.Context, shopID uuid.UUID, customerPhone, channel string) (*Conversation, error) {
        if channel == "" {
                channel = "whatsapp"
        }
        var conv Conversation
        err := db.WithTenantTx(ctx, r.pool, &shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
                // Pick the most-recent conversation for this shop + customer phone +
                // channel. We rely on the customers table for the phone lookup.
                const selQ = `
                        SELECT c.id, c.shop_id, c.customer_id, c.channel, c.state,
                               c.window_24h_expires_at, c.taken_over_by, c.summary,
                               c.created_at, c.updated_at
                          FROM conversations c
                          JOIN customers cu ON cu.id = c.customer_id
                         WHERE c.shop_id = $1 AND cu.phone = $2 AND c.channel = $3
                         ORDER BY c.updated_at DESC
                         LIMIT 1
                `
                err := scanConversation(tx.QueryRow(ctx, selQ, shopID, customerPhone, channel), &conv)
                if err == nil {
                        return nil
                }
                if !errors.Is(err, pgx.ErrNoRows) {
                        return fmt.Errorf("conv repo: get or create by phone: select: %w", err)
                }
                // No conversation yet — find the customer by phone, then insert.
                var customerID uuid.UUID
                if err := tx.QueryRow(ctx, `SELECT id FROM customers WHERE shop_id = $1 AND phone = $2 AND deleted_at IS NULL ORDER BY created_at DESC LIMIT 1`, shopID, customerPhone).Scan(&customerID); err != nil {
                        if errors.Is(err, pgx.ErrNoRows) {
                                return fmt.Errorf("conv repo: customer with phone %q not found in shop %s (call GetOrCreateByPhone first)", customerPhone, shopID)
                        }
                        return fmt.Errorf("conv repo: lookup customer for phone: %w", err)
                }
                const insQ = `
                        INSERT INTO conversations (shop_id, customer_id, channel, state, window_24h_expires_at)
                        VALUES ($1, $2, $3, 'ai', now() + interval '24 hours')
                        RETURNING id, shop_id, customer_id, channel, state, window_24h_expires_at, taken_over_by, summary, created_at, updated_at
                `
                if err := scanConversation(tx.QueryRow(ctx, insQ, shopID, customerID, channel), &conv); err != nil {
                        return fmt.Errorf("conv repo: get or create by phone: insert: %w", err)
                }
                return nil
        })
        if err != nil {
                return nil, err
        }
        return &conv, nil
}

// MessageByWAMID returns the message with the given wamid (WhatsApp Message
// ID). Used by the webhook processor to deduplicate inbound messages (Meta
// retries webhook delivery) and to update the status of outbound messages.
// Returns ErrMessageNotFound when no message matches.
func (r *ConversationRepository) MessageByWAMID(ctx context.Context, shopID uuid.UUID, wamid string) (*Message, error) {
        if wamid == "" {
                return nil, ErrMessageNotFound
        }
        var msg Message
        err := db.WithTenantTx(ctx, r.pool, &shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
                const q = `
                        SELECT id, conversation_id, shop_id, direction, type, content, wamid, status, created_at, delivered_at, read_at
                          FROM messages
                         WHERE shop_id = $1 AND wamid = $2
                         LIMIT 1
                `
                return scanMessage(tx.QueryRow(ctx, q, shopID, wamid), &msg)
        })
        if err != nil {
                if errors.Is(err, pgx.ErrNoRows) {
                        return nil, ErrMessageNotFound
                }
                return nil, fmt.Errorf("conv repo: message by wamid: %w", err)
        }
        return &msg, nil
}

// WAMIDExists returns true if a message with the given wamid already exists
// for this shop. This is the cheap deduplication check used by the webhook
// processor before doing any work on an inbound message (Meta retries).
func (r *ConversationRepository) WAMIDExists(ctx context.Context, shopID uuid.UUID, wamid string) (bool, error) {
        if wamid == "" {
                return false, nil
        }
        var exists bool
        err := db.WithTenantTx(ctx, r.pool, &shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
                return tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM messages WHERE shop_id = $1 AND wamid = $2)`, shopID, wamid).Scan(&exists)
        })
        if err != nil {
                return false, fmt.Errorf("conv repo: wamid exists: %w", err)
        }
        return exists, nil
}

// UpdateMessageStatus updates the status of an outbound message (sent,
// delivered, read, failed). Updates delivered_at / read_at timestamps when
// relevant. Returns ErrMessageNotFound when no message matches the wamid.
func (r *ConversationRepository) UpdateMessageStatus(ctx context.Context, shopID uuid.UUID, wamid, status string) error {
        if wamid == "" || status == "" {
                return ErrMessageNotFound
        }
        return db.WithTenantTx(ctx, r.pool, &shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
                var q string
                switch status {
                case "delivered":
                        q = `UPDATE messages SET status = $3, delivered_at = COALESCE(delivered_at, now()) WHERE shop_id = $1 AND wamid = $2 AND direction = 'outbound'`
                case "read":
                        q = `UPDATE messages SET status = $3, delivered_at = COALESCE(delivered_at, now()), read_at = COALESCE(read_at, now()) WHERE shop_id = $1 AND wamid = $2 AND direction = 'outbound'`
                case "sent", "failed":
                        q = `UPDATE messages SET status = $3 WHERE shop_id = $1 AND wamid = $2 AND direction = 'outbound'`
                default:
                        // Unknown status — ignore to avoid breaking on Meta
                        // adding new statuses (e.g. "deleted").
                        return nil
                }
                ct, err := tx.Exec(ctx, q, shopID, wamid, status)
                if err != nil {
                        return fmt.Errorf("conv repo: update message status: %w", err)
                }
                if ct.RowsAffected() == 0 {
                        return ErrMessageNotFound
                }
                return nil
        })
}

// UpdateMessageWAMID sets the wamid of a previously-stored outbound message.
// Used by the webhook sender: the message is stored with wamid=NULL first
// (so we have the row even if the HTTP call to Meta fails), then the wamid
// returned by Meta is filled in once the send succeeds.
func (r *ConversationRepository) UpdateMessageWAMID(ctx context.Context, shopID, messageID uuid.UUID, wamid string) error {
        return db.WithTenantTx(ctx, r.pool, &shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
                ct, err := tx.Exec(ctx, `UPDATE messages SET wamid = $3 WHERE shop_id = $1 AND id = $2`, shopID, messageID, wamid)
                if err != nil {
                        return fmt.Errorf("conv repo: update message wamid: %w", err)
                }
                if ct.RowsAffected() == 0 {
                        return ErrMessageNotFound
                }
                return nil
        })
}

// SetSummary saves the AI-generated conversation summary (used for handover).
func (r *ConversationRepository) SetSummary(ctx context.Context, shopID, id uuid.UUID, summary string) error {
        return db.WithTenantTx(ctx, r.pool, &shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
                const q = `UPDATE conversations SET summary = $3, updated_at = now() WHERE shop_id = $1 AND id = $2`
                ct, err := tx.Exec(ctx, q, shopID, id, summary)
                if err != nil {
                        return fmt.Errorf("conv repo: set summary: %w", err)
                }
                if ct.RowsAffected() == 0 {
                        return ErrConversationNotFound
                }
                return nil
        })
}

// AddMessage inserts one message. wamid is optional (NULL for outbound NOVA
// replies that haven't been sent to Meta yet). Returns the inserted row.
func (r *ConversationRepository) AddMessage(ctx context.Context, shopID, conversationID uuid.UUID, direction, msgType, content, wamid string) (*Message, error) {
        if direction == "" {
                direction = MsgInbound
        }
        if msgType == "" {
                msgType = "text"
        }
        var msg Message
        err := db.WithTenantTx(ctx, r.pool, &shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
                var wamidArg any
                if wamid != "" {
                        wamidArg = wamid
                }
                status := "queued"
                if direction == MsgInbound {
                        status = "delivered"
                }
                const q = `
                        INSERT INTO messages (conversation_id, shop_id, direction, type, content, wamid, status)
                        VALUES ($1, $2, $3, $4, $5, $6, $7)
                        RETURNING id, conversation_id, shop_id, direction, type, content, wamid, status, created_at, delivered_at, read_at
                `
                if err := scanMessage(tx.QueryRow(ctx, q, conversationID, shopID, direction, msgType, content, wamidArg, status), &msg); err != nil {
                        return fmt.Errorf("conv repo: add message: %w", err)
                }
                // Touch conversation's updated_at (and reset the 24h window for inbound).
                if direction == MsgInbound {
                        _, _ = tx.Exec(ctx, `UPDATE conversations SET updated_at = now(), window_24h_expires_at = now() + interval '24 hours' WHERE shop_id = $1 AND id = $2`, shopID, conversationID)
                } else {
                        _, _ = tx.Exec(ctx, `UPDATE conversations SET updated_at = now() WHERE shop_id = $1 AND id = $2`, shopID, conversationID)
                }
                return nil
        })
        if err != nil {
                return nil, err
        }
        return &msg, nil
}

// ListMessages returns the most recent messages for a conversation, oldest first
// (so the conversation history reads naturally in the chat UI).
func (r *ConversationRepository) ListMessages(ctx context.Context, shopID, conversationID uuid.UUID, limit int) ([]Message, error) {
        if limit <= 0 || limit > 200 {
                limit = 50
        }
        var out []Message
        err := db.WithTenantTx(ctx, r.pool, &shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
                // Fetch the most recent N messages (DESC), then reverse to ASC for display.
                const q = `
                        SELECT id, conversation_id, shop_id, direction, type, content, wamid, status, created_at, delivered_at, read_at
                          FROM messages
                         WHERE shop_id = $1 AND conversation_id = $2
                         ORDER BY created_at DESC
                         LIMIT $3
                `
                rows, err := tx.Query(ctx, q, shopID, conversationID, limit)
                if err != nil {
                        return err
                }
                defer rows.Close()
                var desc []Message
                for rows.Next() {
                        var m Message
                        if err := scanMessage(rows, &m); err != nil {
                                return err
                        }
                        desc = append(desc, m)
                }
                if err := rows.Err(); err != nil {
                        return err
                }
                // Reverse to ASC.
                out = make([]Message, 0, len(desc))
                for i := len(desc) - 1; i >= 0; i-- {
                        out = append(out, desc[i])
                }
                return nil
        })
        if err != nil {
                return nil, fmt.Errorf("conv repo: list messages: %w", err)
        }
        return out, nil
}

// ListByShop returns a paginated list of conversations for the shop, joined
// with the customer name + phone + the last message preview.
func (r *ConversationRepository) ListByShop(ctx context.Context, shopID uuid.UUID, params ListConversationsParams) ([]ConversationListItem, int64, error) {
        params.Normalize()
        var (
                out   []ConversationListItem
                total int64
        )
        err := db.WithTenantTx(ctx, r.pool, &shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
                where := "WHERE c.shop_id = $1"
                args := []any{shopID}
                idx := 2
                if params.State != "" {
                        where += fmt.Sprintf(" AND c.state = $%d", idx)
                        args = append(args, params.State)
                        idx++
                }
                if params.Search != "" {
                        where += fmt.Sprintf(" AND (coalesce(cu.name, '') ILIKE $%d OR cu.phone ILIKE $%d)", idx, idx)
                        args = append(args, "%"+params.Search+"%")
                        idx++
                }

                // Count.
                countQ := `SELECT COUNT(*) FROM conversations c LEFT JOIN customers cu ON cu.id = c.customer_id ` + where
                if err := tx.QueryRow(ctx, countQ, args...).Scan(&total); err != nil {
                        return fmt.Errorf("conv repo: list: count: %w", err)
                }

                // List with joined customer + last message preview.
                listQ := `
                        SELECT c.id, c.shop_id, c.customer_id, c.channel, c.state, c.window_24h_expires_at,
                               c.taken_over_by, c.summary, c.created_at, c.updated_at,
                               cu.name, cu.phone,
                               lm.last_at, lm.last_content,
                               (SELECT COUNT(*) FROM messages m WHERE m.conversation_id = c.id AND m.direction = 'inbound' AND m.read_at IS NULL) AS unread
                          FROM conversations c
                          LEFT JOIN customers cu ON cu.id = c.customer_id
                          LEFT JOIN LATERAL (
                               SELECT created_at AS last_at, content AS last_content
                                 FROM messages WHERE conversation_id = c.id
                                 ORDER BY created_at DESC LIMIT 1
                          ) lm ON true
                ` + where + `
                        ORDER BY c.updated_at DESC
                        LIMIT $` + fmt.Sprintf("%d", idx) + ` OFFSET $` + fmt.Sprintf("%d", idx+1)
                args = append(args, params.Limit, params.Offset())
                rows, err := tx.Query(ctx, listQ, args...)
                if err != nil {
                        return err
                }
                defer rows.Close()
                for rows.Next() {
                        var item ConversationListItem
                        if err := scanConversationListItem(rows, &item); err != nil {
                                return err
                        }
                        out = append(out, item)
                }
                return rows.Err()
        })
        if err != nil {
                return nil, 0, fmt.Errorf("conv repo: list by shop: %w", err)
        }
        return out, total, nil
}

// ListToTakeOver returns conversations in 'human' state (escalated or taken
// over) for the shop dashboard's "à reprendre" widget.
func (r *ConversationRepository) ListToTakeOver(ctx context.Context, shopID uuid.UUID) ([]ConversationListItem, error) {
        var out []ConversationListItem
        err := db.WithTenantTx(ctx, r.pool, &shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
                const q = `
                        SELECT c.id, c.shop_id, c.customer_id, c.channel, c.state, c.window_24h_expires_at,
                               c.taken_over_by, c.summary, c.created_at, c.updated_at,
                               cu.name, cu.phone,
                               lm.last_at, lm.last_content, 0 AS unread
                          FROM conversations c
                          LEFT JOIN customers cu ON cu.id = c.customer_id
                          LEFT JOIN LATERAL (
                               SELECT created_at AS last_at, content AS last_content
                                 FROM messages WHERE conversation_id = c.id
                                 ORDER BY created_at DESC LIMIT 1
                          ) lm ON true
                         WHERE c.shop_id = $1 AND c.state = 'human'
                         ORDER BY c.updated_at DESC
                         LIMIT 100
                `
                rows, err := tx.Query(ctx, q, shopID)
                if err != nil {
                        return err
                }
                defer rows.Close()
                for rows.Next() {
                        var item ConversationListItem
                        if err := scanConversationListItem(rows, &item); err != nil {
                                return err
                        }
                        out = append(out, item)
                }
                return rows.Err()
        })
        if err != nil {
                return nil, fmt.Errorf("conv repo: list to take over: %w", err)
        }
        return out, nil
}

// --- scanners ---------------------------------------------------------------

type convScanner interface {
        Scan(dest ...any) error
}

func scanConversation(s convScanner, c *Conversation) error {
        var (
                windowExpires *time.Time
                takenOverBy   *uuid.UUID
                summary       *string
        )
        err := s.Scan(
                &c.ID, &c.ShopID, &c.CustomerID, &c.Channel, &c.State,
                &windowExpires, &takenOverBy, &summary,
                &c.CreatedAt, &c.UpdatedAt,
        )
        if err != nil {
                return err
        }
        c.Window24hExpiresAt = windowExpires
        c.TakenOverBy = takenOverBy
        c.Summary = summary
        return nil
}

func scanMessage(s convScanner, m *Message) error {
        var (
                wamid       *string
                deliveredAt *time.Time
                readAt      *time.Time
        )
        err := s.Scan(
                &m.ID, &m.ConversationID, &m.ShopID, &m.Direction, &m.Type,
                &m.Content, &wamid, &m.Status, &m.CreatedAt, &deliveredAt, &readAt,
        )
        if err != nil {
                return err
        }
        m.WAMID = wamid
        m.DeliveredAt = deliveredAt
        m.ReadAt = readAt
        return nil
}

func scanConversationListItem(s convScanner, item *ConversationListItem) error {
        var (
                windowExpires *time.Time
                takenOverBy   *uuid.UUID
                summary       *string
                customerName  *string
                customerPhone *string
                lastAt        *time.Time
                lastContent   *string
                unread        int
        )
        err := s.Scan(
                &item.ID, &item.ShopID, &item.CustomerID, &item.Channel, &item.State,
                &windowExpires, &takenOverBy, &summary,
                &item.CreatedAt, &item.UpdatedAt,
                &customerName, &customerPhone,
                &lastAt, &lastContent, &unread,
        )
        if err != nil {
                return err
        }
        item.Window24hExpiresAt = windowExpires
        item.TakenOverBy = takenOverBy
        item.Summary = summary
        item.CustomerName = customerName
        if customerPhone != nil {
                item.CustomerPhone = *customerPhone
        }
        item.LastMessageAt = lastAt
        item.LastMessagePreview = lastContent
        item.UnreadCount = unread
        return nil
}

// Ensure models.Conversation is referenced so the import is not flagged unused
// if future code calls into models. (We keep our own Conversation type here to
// avoid coupling with models.)
var _ = models.ConvAI
