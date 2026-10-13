// Payment configs repository — handles the `payment_configs` table
// (NOVA v3 — spec section 4).
//
// All queries go through db.WithTenantTx so the RLS policies from migration
// 016 are enforced:
//
//      shop_id = current_shop_id() OR is_platform_admin()
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"nova-api/internal/db"
	"nova-api/internal/models"
)

// --- Sentinels --------------------------------------------------------------

// ErrPaymentConfigNotFound is returned when a payment config doesn't exist for a shop.
var ErrPaymentConfigNotFound = errors.New("payment config not found")

// --- Inputs -----------------------------------------------------------------

// UpsertPaymentConfigInput is the data needed to create or update a payment config.
type UpsertPaymentConfigInput struct {
	Mode          string // paiement_livraison | paiement_avance | paiement_integral
	AdvanceAmount *int64
	DelayMinutes  int
	WaveLink      *string
	WaveNumber    *string
	MoovNumber    *string
	OrangeNumber  *string
	MTNNumber     *string
	ActiveMethods []string
	Active        bool
}

// --- Repository -------------------------------------------------------------

// PaymentConfigRepository wraps the payment_configs table.
type PaymentConfigRepository struct {
	pool *pgxpool.Pool
}

// NewPaymentConfigRepository returns a PaymentConfigRepository bound to the pool.
func NewPaymentConfigRepository(pool *pgxpool.Pool) *PaymentConfigRepository {
	return &PaymentConfigRepository{pool: pool}
}

// GetByShop returns the payment config for a shop. If no config exists, a
// default one (paiement_livraison, 120 min) is created on the fly (best-effort)
// — but the caller should preferably call GetOrCreateByShop.
func (r *PaymentConfigRepository) GetByShop(ctx context.Context, shopID, userID uuid.UUID, role string) (*models.PaymentConfig, error) {
	var cfg models.PaymentConfig
	err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
		return scanPaymentConfig(tx.QueryRow(ctx, `
			SELECT shop_id, mode, advance_amount, delay_minutes, wave_link, wave_number,
			       moov_number, orange_number, mtn_number, active_methods, active, created_at, updated_at
			  FROM payment_configs WHERE shop_id = $1
		`, shopID), &cfg)
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPaymentConfigNotFound
		}
		return nil, fmt.Errorf("payment config repo: get by shop: %w", err)
	}
	return &cfg, nil
}

// GetOrCreateByShop returns the payment config for a shop, creating a default
// one (paiement_livraison, 120 min) if it doesn't exist yet.
func (r *PaymentConfigRepository) GetOrCreateByShop(ctx context.Context, shopID, userID uuid.UUID, role string) (*models.PaymentConfig, error) {
	cfg, err := r.GetByShop(ctx, shopID, userID, role)
	if err == nil {
		return cfg, nil
	}
	if !errors.Is(err, ErrPaymentConfigNotFound) {
		return nil, err
	}
	// Create a default config.
	return r.Upsert(ctx, shopID, userID, role, UpsertPaymentConfigInput{
		Mode:          string(models.PaymentModeLivraison),
		DelayMinutes:  120,
		ActiveMethods: []string{"wave", "moov", "orange", "mtn"},
		Active:        true,
	})
}

// Upsert creates or updates the payment config for a shop.
func (r *PaymentConfigRepository) Upsert(ctx context.Context, shopID, userID uuid.UUID, role string, in UpsertPaymentConfigInput) (*models.PaymentConfig, error) {
	if in.DelayMinutes <= 0 {
		in.DelayMinutes = 120
	}
	if in.Mode == "" {
		in.Mode = string(models.PaymentModeLivraison)
	}
	if in.ActiveMethods == nil {
		in.ActiveMethods = []string{}
	}
	var cfg models.PaymentConfig
	err := db.WithTenantTx(ctx, r.pool, &shopID, userID, role, func(tx pgx.Tx) error {
		var advanceArg any
		if in.AdvanceAmount != nil {
			advanceArg = *in.AdvanceAmount
		}
		var waveLinkArg, waveNumberArg, moovNumberArg, orangeNumberArg, mtnNumberArg any
		if in.WaveLink != nil {
			waveLinkArg = *in.WaveLink
		}
		if in.WaveNumber != nil {
			waveNumberArg = *in.WaveNumber
		}
		if in.MoovNumber != nil {
			moovNumberArg = *in.MoovNumber
		}
		if in.OrangeNumber != nil {
			orangeNumberArg = *in.OrangeNumber
		}
		if in.MTNNumber != nil {
			mtnNumberArg = *in.MTNNumber
		}
		const q = `
			INSERT INTO payment_configs (shop_id, mode, advance_amount, delay_minutes,
			                             wave_link, wave_number, moov_number, orange_number, mtn_number,
			                             active_methods, active)
			VALUES ($1, $2::payment_config_mode, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			ON CONFLICT (shop_id) DO UPDATE SET
				mode = EXCLUDED.mode,
				advance_amount = EXCLUDED.advance_amount,
				delay_minutes = EXCLUDED.delay_minutes,
				wave_link = EXCLUDED.wave_link,
				wave_number = EXCLUDED.wave_number,
				moov_number = EXCLUDED.moov_number,
				orange_number = EXCLUDED.orange_number,
				mtn_number = EXCLUDED.mtn_number,
				active_methods = EXCLUDED.active_methods,
				active = EXCLUDED.active,
				updated_at = now()
			RETURNING shop_id, mode, advance_amount, delay_minutes, wave_link, wave_number,
			          moov_number, orange_number, mtn_number, active_methods, active, created_at, updated_at
		`
		return scanPaymentConfig(tx.QueryRow(ctx, q,
			shopID, in.Mode, advanceArg, in.DelayMinutes,
			waveLinkArg, waveNumberArg, moovNumberArg, orangeNumberArg, mtnNumberArg,
			in.ActiveMethods, in.Active,
		), &cfg)
	})
	if err != nil {
		return nil, fmt.Errorf("payment config repo: upsert: %w", err)
	}
	return &cfg, nil
}

// GetDelayMinutes returns the configured payment delay in minutes for a shop
// (default 120 if not configured). Used by the AI prompt + cron.
func (r *PaymentConfigRepository) GetDelayMinutes(ctx context.Context, shopID uuid.UUID) int {
	cfg, err := r.GetByShop(ctx, shopID, uuid.Nil, "super_admin")
	if err != nil || cfg == nil || cfg.DelayMinutes <= 0 {
		return 120
	}
	return cfg.DelayMinutes
}

// --- helpers ----------------------------------------------------------------

// scanPaymentConfig maps a payment_configs row into a models.PaymentConfig.
func scanPaymentConfig(s scanner, c *models.PaymentConfig) error {
	var (
		mode          string
		advanceAmount *int64
		waveLink      *string
		waveNumber    *string
		moovNumber    *string
		orangeNumber  *string
		mtnNumber     *string
	)
	err := s.Scan(
		&c.ShopID, &mode, &advanceAmount, &c.DelayMinutes,
		&waveLink, &waveNumber, &moovNumber, &orangeNumber, &mtnNumber,
		&c.ActiveMethods, &c.Active, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return err
	}
	c.Mode = models.PaymentConfigMode(mode)
	c.AdvanceAmount = advanceAmount
	c.WaveLink = waveLink
	c.WaveNumber = waveNumber
	c.MoovNumber = moovNumber
	c.OrangeNumber = orangeNumber
	c.MTNNumber = mtnNumber
	if c.ActiveMethods == nil {
		c.ActiveMethods = []string{}
	}
	return nil
}
