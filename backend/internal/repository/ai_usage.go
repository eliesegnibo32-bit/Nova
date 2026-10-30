// ai_usage repository (ch. 5.6 — migration 009).
//
// ai_usage: journal of LLM consumption per shop + per conversation. Used for
// cost tracking, quota enforcement, and the dashboard "AI usage" widget.
package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"nova-api/internal/db"
)

// AIUsageRepository wraps the ai_usage table.
type AIUsageRepository struct {
	pool *pgxpool.Pool
}

// NewAIUsageRepository returns an AIUsageRepository bound to the pool.
func NewAIUsageRepository(pool *pgxpool.Pool) *AIUsageRepository {
	return &AIUsageRepository{pool: pool}
}

// AIUsageEntry is one row in ai_usage.
type AIUsageEntry struct {
	ID             uuid.UUID  `json:"id"`
	ShopID         uuid.UUID  `json:"shop_id"`
	ConversationID *uuid.UUID `json:"conversation_id,omitempty"`
	Model          string     `json:"model"`
	TokensIn       int        `json:"tokens_in"`
	TokensOut      int        `json:"tokens_out"`
	LatencyMs      int        `json:"latency_ms"`
	EstimatedCost  float64    `json:"estimated_cost"`
	CreatedAt      time.Time  `json:"created_at"`
}

// LogAIUsage inserts one ai_usage row. conversation_id may be nil (e.g. for
// standalone completions outside a conversation). estimated_cost is in USD.
func (r *AIUsageRepository) LogAIUsage(ctx context.Context, shopID uuid.UUID, conversationID *uuid.UUID, model string, tokensIn, tokensOut, latencyMs int, estimatedCost float64) error {
	return db.WithTenantTx(ctx, r.pool, &shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
		var convArg any
		if conversationID != nil {
			convArg = *conversationID
		}
		const q = `
                        INSERT INTO ai_usage (shop_id, conversation_id, model, tokens_in, tokens_out, latency_ms, estimated_cost)
                        VALUES ($1, $2, $3, $4, $5, $6, $7)
                `
		_, err := tx.Exec(ctx, q, shopID, convArg, model, tokensIn, tokensOut, latencyMs, estimatedCost)
		if err != nil {
			return fmt.Errorf("ai_usage repo: log: %w", err)
		}
		return nil
	})
}

// AIUsageStats is the aggregate shape returned by StatsForShop.
type AIUsageStats struct {
	TotalTokensIn  int64                        `json:"total_tokens_in"`
	TotalTokensOut int64                        `json:"total_tokens_out"`
	TotalCost      float64                      `json:"total_cost"`
	CallsCount     int64                        `json:"calls_count"`
	ByModel        map[string]AIUsageModelStats `json:"by_model"`
}

// AIUsageModelStats is the per-model breakdown.
type AIUsageModelStats struct {
	TokensIn  int64   `json:"tokens_in"`
	TokensOut int64   `json:"tokens_out"`
	Cost      float64 `json:"cost"`
	Calls     int64   `json:"calls"`
}

// StatsForShop returns the aggregate AI usage for the shop in the current month.
func (r *AIUsageRepository) StatsForShop(ctx context.Context, shopID uuid.UUID) (*AIUsageStats, error) {
	stats := &AIUsageStats{ByModel: map[string]AIUsageModelStats{}}
	err := db.WithTenantTx(ctx, r.pool, &shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
		// Aggregate totals for the current month.
		const totQ = `
                        SELECT
                                COALESCE(SUM(tokens_in), 0),
                                COALESCE(SUM(tokens_out), 0),
                                COALESCE(SUM(estimated_cost), 0)::float8,
                                COUNT(*)
                          FROM ai_usage
                         WHERE shop_id = $1
                           AND created_at >= date_trunc('month', now())
                `
		if err := tx.QueryRow(ctx, totQ, shopID).Scan(
			&stats.TotalTokensIn, &stats.TotalTokensOut, &stats.TotalCost, &stats.CallsCount,
		); err != nil {
			return fmt.Errorf("ai_usage repo: stats totals: %w", err)
		}
		// Per-model breakdown.
		const modelQ = `
                        SELECT model,
                                COALESCE(SUM(tokens_in), 0),
                                COALESCE(SUM(tokens_out), 0),
                                COALESCE(SUM(estimated_cost), 0)::float8,
                                COUNT(*)
                          FROM ai_usage
                         WHERE shop_id = $1
                           AND created_at >= date_trunc('month', now())
                         GROUP BY model
                `
		rows, err := tx.Query(ctx, modelQ, shopID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				model     string
				tokensIn  int64
				tokensOut int64
				cost      float64
				calls     int64
			)
			if err := rows.Scan(&model, &tokensIn, &tokensOut, &cost, &calls); err != nil {
				return err
			}
			stats.ByModel[model] = AIUsageModelStats{
				TokensIn:  tokensIn,
				TokensOut: tokensOut,
				Cost:      cost,
				Calls:     calls,
			}
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("ai_usage repo: stats for shop: %w", err)
	}
	return stats, nil
}

// ListByShop returns the most recent ai_usage entries for a shop.
func (r *AIUsageRepository) ListByShop(ctx context.Context, shopID uuid.UUID, limit int) ([]AIUsageEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	var out []AIUsageEntry
	err := db.WithTenantTx(ctx, r.pool, &shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
		const q = `
                        SELECT id, shop_id, conversation_id, model, tokens_in, tokens_out, latency_ms, estimated_cost, created_at
                          FROM ai_usage
                         WHERE shop_id = $1
                         ORDER BY created_at DESC
                         LIMIT $2
                `
		rows, err := tx.Query(ctx, q, shopID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e AIUsageEntry
			var convID *uuid.UUID
			if err := rows.Scan(&e.ID, &e.ShopID, &convID, &e.Model, &e.TokensIn, &e.TokensOut, &e.LatencyMs, &e.EstimatedCost, &e.CreatedAt); err != nil {
				return err
			}
			e.ConversationID = convID
			out = append(out, e)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("ai_usage repo: list by shop: %w", err)
	}
	return out, nil
}

// ============================================================================
// Spec Task 10 — Quota tracking + dashboard helpers
// ============================================================================

// Record inserts one ai_usage row. This is the spec Task 10 entry point used
// by the QuotaService.IncrementUsage flow. conversation_id may be nil. The
// estimated_cost is in USD (matches the table's numeric column).
//
// Note: this method is functionally identical to LogAIUsage — we expose both
// names because the spec uses `Record` for the quota-flow path while the AI
// engine already uses `LogAIUsage`. They share the same SQL INSERT.
func (r *AIUsageRepository) Record(ctx context.Context, shopID, conversationID uuid.UUID, model string, tokensIn, tokensOut, latencyMs int, estimatedCost float64) error {
	conv := conversationID
	return r.LogAIUsage(ctx, shopID, &conv, model, tokensIn, tokensOut, latencyMs, estimatedCost)
}

// MonthlyUsage is the aggregate shape returned by GetMonthlyUsage — totals for
// one (shop, year, month) tuple.
type MonthlyUsage struct {
	Year              int     `json:"year"`
	Month             int     `json:"month"`
	TokensIn          int64   `json:"tokens_in"`
	TokensOut         int64   `json:"tokens_out"`
	EstimatedCost     float64 `json:"estimated_cost"`
	MessageCount      int64   `json:"message_count"`
	ConversationCount int64   `json:"conversation_count"`
}

// GetMonthlyUsage returns the aggregate AI usage for the given shop in the
// given (year, month). `month` is 1-12. The aggregate counts every ai_usage
// row (one per LLM call) and the distinct conversation_id values.
func (r *AIUsageRepository) GetMonthlyUsage(ctx context.Context, shopID uuid.UUID, year, month int) (*MonthlyUsage, error) {
	if month < 1 || month > 12 {
		return nil, fmt.Errorf("ai_usage repo: invalid month %d", month)
	}
	out := &MonthlyUsage{Year: year, Month: month}
	err := db.WithTenantTx(ctx, r.pool, &shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
		// Build the month bounds in UTC.
		start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
		end := start.AddDate(0, 1, 0)
		const q = `
                        SELECT
                                COALESCE(SUM(tokens_in), 0),
                                COALESCE(SUM(tokens_out), 0),
                                COALESCE(SUM(estimated_cost), 0)::float8,
                                COUNT(*),
                                COUNT(DISTINCT conversation_id)
                          FROM ai_usage
                         WHERE shop_id = $1
                           AND created_at >= $2 AND created_at < $3
                `
		return tx.QueryRow(ctx, q, shopID, start, end).Scan(
			&out.TokensIn, &out.TokensOut, &out.EstimatedCost,
			&out.MessageCount, &out.ConversationCount,
		)
	})
	if err != nil {
		return nil, fmt.Errorf("ai_usage repo: get monthly usage: %w", err)
	}
	return out, nil
}

// DailyUsage is one day's aggregate for the charts.
type DailyUsage struct {
	Day           time.Time `json:"day"`
	TokensIn      int64     `json:"tokens_in"`
	TokensOut     int64     `json:"tokens_out"`
	EstimatedCost float64   `json:"estimated_cost"`
	MessageCount  int64     `json:"message_count"`
}

// GetDailyUsage returns the daily aggregate for the given shop between `from`
// and `to` (inclusive of `from`, exclusive of `to` to match the standard
// [from, to) SQL pattern). Used by the dashboard "AI usage" chart.
func (r *AIUsageRepository) GetDailyUsage(ctx context.Context, shopID uuid.UUID, from, to time.Time) ([]DailyUsage, error) {
	var out []DailyUsage
	err := db.WithTenantTx(ctx, r.pool, &shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
		const q = `
                        SELECT date_trunc('day', created_at) AS day,
                               COALESCE(SUM(tokens_in), 0),
                               COALESCE(SUM(tokens_out), 0),
                               COALESCE(SUM(estimated_cost), 0)::float8,
                               COUNT(*)
                          FROM ai_usage
                         WHERE shop_id = $1
                           AND created_at >= $2 AND created_at < $3
                         GROUP BY day
                         ORDER BY day ASC
                `
		rows, err := tx.Query(ctx, q, shopID, from, to)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d DailyUsage
			if err := rows.Scan(&d.Day, &d.TokensIn, &d.TokensOut, &d.EstimatedCost, &d.MessageCount); err != nil {
				return err
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("ai_usage repo: get daily usage: %w", err)
	}
	return out, nil
}

// CountMessagesThisMonth returns the number of ai_usage rows for the shop in
// the current month (calendar month, server-local time). This is the value
// the QuotaService compares against plan.message_quota.
//
// We count ai_usage rows rather than conversations.messages because the
// cahier des charges (ch. 7.2) defines the quota as "messages IA" — every
// LLM call counts as one. A conversation with 5 customer messages + 5 AI
// replies = 5 ai_usage rows = 5 quota units.
func (r *AIUsageRepository) CountMessagesThisMonth(ctx context.Context, shopID uuid.UUID) (int64, error) {
	var n int64
	err := db.WithTenantTx(ctx, r.pool, &shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
		const q = `
                        SELECT COUNT(*)
                          FROM ai_usage
                         WHERE shop_id = $1
                           AND created_at >= date_trunc('month', now())
                `
		return tx.QueryRow(ctx, q, shopID).Scan(&n)
	})
	if err != nil {
		return 0, fmt.Errorf("ai_usage repo: count messages this month: %w", err)
	}
	return n, nil
}

// UsageSummary is the aggregate shape for a custom date range.
type UsageSummary struct {
	From              time.Time `json:"from"`
	To                time.Time `json:"to"`
	TokensIn          int64     `json:"tokens_in"`
	TokensOut         int64     `json:"tokens_out"`
	EstimatedCost     float64   `json:"estimated_cost"`
	MessageCount      int64     `json:"message_count"`
	ConversationCount int64     `json:"conversation_count"`
	AvgLatencyMs      int64     `json:"avg_latency_ms"`
}

// GetUsageByShop returns the aggregate AI usage for the shop between `from`
// and `to` (inclusive of `from`, exclusive of `to`).
func (r *AIUsageRepository) GetUsageByShop(ctx context.Context, shopID uuid.UUID, from, to time.Time) (*UsageSummary, error) {
	out := &UsageSummary{From: from, To: to}
	err := db.WithTenantTx(ctx, r.pool, &shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
		const q = `
                        SELECT
                                COALESCE(SUM(tokens_in), 0),
                                COALESCE(SUM(tokens_out), 0),
                                COALESCE(SUM(estimated_cost), 0)::float8,
                                COUNT(*),
                                COUNT(DISTINCT conversation_id),
                                COALESCE(AVG(latency_ms), 0)::bigint
                          FROM ai_usage
                         WHERE shop_id = $1
                           AND created_at >= $2 AND created_at < $3
                `
		return tx.QueryRow(ctx, q, shopID, from, to).Scan(
			&out.TokensIn, &out.TokensOut, &out.EstimatedCost,
			&out.MessageCount, &out.ConversationCount, &out.AvgLatencyMs,
		)
	})
	if err != nil {
		return nil, fmt.Errorf("ai_usage repo: get usage by shop: %w", err)
	}
	return out, nil
}

// ConversationUsage is the per-conversation aggregate used by
// GetTopConversationsByCost.
type ConversationUsage struct {
	ConversationID  uuid.UUID `json:"conversation_id"`
	ShopID          uuid.UUID `json:"shop_id"`
	MessageCount    int64     `json:"message_count"`
	TokensIn        int64     `json:"tokens_in"`
	TokensOut       int64     `json:"tokens_out"`
	EstimatedCost   float64   `json:"estimated_cost"`
	LastInteraction time.Time `json:"last_interaction"`
}

// GetTopConversationsByCost returns the top `limit` conversations for the
// shop ranked by total estimated_cost (descending). Used by the dashboard to
// surface the most expensive conversations (e.g. for abuse / cost debugging).
func (r *AIUsageRepository) GetTopConversationsByCost(ctx context.Context, shopID uuid.UUID, limit int) ([]ConversationUsage, error) {
	if limit <= 0 || limit > 200 {
		limit = 10
	}
	var out []ConversationUsage
	err := db.WithTenantTx(ctx, r.pool, &shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
		const q = `
                        SELECT conversation_id,
                               shop_id,
                               COUNT(*)            AS msg_count,
                               COALESCE(SUM(tokens_in), 0),
                               COALESCE(SUM(tokens_out), 0),
                               COALESCE(SUM(estimated_cost), 0)::float8 AS cost,
                               MAX(created_at)     AS last_interaction
                          FROM ai_usage
                         WHERE shop_id = $1
                           AND conversation_id IS NOT NULL
                         GROUP BY conversation_id, shop_id
                         ORDER BY cost DESC
                         LIMIT $2
                `
		rows, err := tx.Query(ctx, q, shopID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c ConversationUsage
			if err := rows.Scan(
				&c.ConversationID, &c.ShopID, &c.MessageCount,
				&c.TokensIn, &c.TokensOut, &c.EstimatedCost, &c.LastInteraction,
			); err != nil {
				return err
			}
			out = append(out, c)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("ai_usage repo: top conversations by cost: %w", err)
	}
	return out, nil
}

// GetTotalCostThisMonth returns the sum of estimated_cost for the shop in the
// current month (USD). Used by the dashboard "cost this month" widget and by
// the QuotaService for cost-vs-quota analytics.
func (r *AIUsageRepository) GetTotalCostThisMonth(ctx context.Context, shopID uuid.UUID) (float64, error) {
	var cost float64
	err := db.WithTenantTx(ctx, r.pool, &shopID, uuid.Nil, "owner", func(tx pgx.Tx) error {
		const q = `
                        SELECT COALESCE(SUM(estimated_cost), 0)::float8
                          FROM ai_usage
                         WHERE shop_id = $1
                           AND created_at >= date_trunc('month', now())
                `
		return tx.QueryRow(ctx, q, shopID).Scan(&cost)
	})
	if err != nil {
		return 0, fmt.Errorf("ai_usage repo: total cost this month: %w", err)
	}
	return cost, nil
}
