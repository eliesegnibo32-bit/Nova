// ShopMember repository — handles the `shop_members` table.
//
// A user can belong to several shops (n-aire relationship). The auth flow
// needs to list ALL shops for a user (regardless of which shop is currently
// active in the session), so most methods here run with
// `app.user_role = 'super_admin'` to bypass shop-scoped RLS.
//
// For the team-management endpoint (ListByShop), the caller is already a
// member of the shop they're querying — that flow uses the session's
// current_shop_id and stays RLS-protected.
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"nova-api/internal/db"
	"nova-api/internal/models"
)

// ShopMemberRepository handles the shop_members table.
type ShopMemberRepository struct {
	pool *pgxpool.Pool
}

// NewShopMemberRepository returns a ShopMemberRepository bound to the pool.
func NewShopMemberRepository(pool *pgxpool.Pool) *ShopMemberRepository {
	return &ShopMemberRepository{pool: pool}
}

// GetByUserID returns every shop membership for the given user (across all
// shops). Used by the auth flow to populate the "shops" list in the login
// and /me responses.
func (r *ShopMemberRepository) GetByUserID(ctx context.Context, userID uuid.UUID) ([]models.ShopMember, error) {
	var out []models.ShopMember
	err := db.WithTenantTx(ctx, r.pool, nil, userID, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
		const q = `
			SELECT id, shop_id, user_id, role, permissions, created_at, updated_at
			  FROM shop_members
			 WHERE user_id = $1
			 ORDER BY created_at ASC
		`
		rows, err := tx.Query(ctx, q, userID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var sm models.ShopMember
			if err := scanShopMember(rows, &sm); err != nil {
				return err
			}
			out = append(out, sm)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("shop_members repo: get by user: %w", err)
	}
	return out, nil
}

// GetByShopAndUser returns the membership row linking shopID and userID, or
// ErrNotFound if the user is not a member of that shop.
func (r *ShopMemberRepository) GetByShopAndUser(ctx context.Context, shopID, userID uuid.UUID) (*models.ShopMember, error) {
	var sm models.ShopMember
	err := db.WithTenantTx(ctx, r.pool, &shopID, userID, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
		const q = `
			SELECT id, shop_id, user_id, role, permissions, created_at, updated_at
			  FROM shop_members
			 WHERE shop_id = $1 AND user_id = $2
		`
		row := tx.QueryRow(ctx, q, shopID, userID)
		return scanShopMember(row, &sm)
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("shop_members repo: get by shop+user: %w", err)
	}
	return &sm, nil
}

// Create inserts a new membership. Used during shop creation (adds the
// creator as 'owner') and team invitation (adds an employee).
func (r *ShopMemberRepository) Create(
	ctx context.Context,
	shopID, userID uuid.UUID,
	role string,
	permissions json.RawMessage,
) error {
	if permissions == nil {
		permissions = json.RawMessage("{}")
	}
	return db.WithTenantTx(ctx, r.pool, &shopID, userID, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
		const q = `
			INSERT INTO shop_members (shop_id, user_id, role, permissions)
			VALUES ($1, $2, $3, $4::jsonb)
		`
		_, err := tx.Exec(ctx, q, shopID, userID, role, []byte(permissions))
		if err != nil {
			return fmt.Errorf("create shop_member: %w", err)
		}
		return nil
	})
}

// ListByShop returns every member of the given shop. The caller must have an
// active session scoped to that shop (RLS-protected).
func (r *ShopMemberRepository) ListByShop(ctx context.Context, shopID, requesterID uuid.UUID) ([]models.ShopMember, error) {
	var out []models.ShopMember
	err := db.WithTenantTx(ctx, r.pool, &shopID, requesterID, string(models.RoleOwner), func(tx pgx.Tx) error {
		const q = `
			SELECT id, shop_id, user_id, role, permissions, created_at, updated_at
			  FROM shop_members
			 WHERE shop_id = $1
			 ORDER BY created_at ASC
		`
		rows, err := tx.Query(ctx, q, shopID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var sm models.ShopMember
			if err := scanShopMember(rows, &sm); err != nil {
				return err
			}
			out = append(out, sm)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("shop_members repo: list by shop: %w", err)
	}
	return out, nil
}

// Update changes the role and permissions of a membership row.
func (r *ShopMemberRepository) Update(ctx context.Context, id uuid.UUID, role string, permissions json.RawMessage) error {
	if permissions == nil {
		permissions = json.RawMessage("{}")
	}
	// We don't have shop context here — use platform-admin to bypass RLS.
	// This is safe because the caller (team management handler) has already
	// verified the requester is a member of the shop.
	return db.WithTenantTx(ctx, r.pool, nil, uuid.Nil, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
		const q = `
			UPDATE shop_members
			   SET role = $2,
			       permissions = $3::jsonb,
			       updated_at = now()
			 WHERE id = $1
		`
		ct, err := tx.Exec(ctx, q, id, role, []byte(permissions))
		if err != nil {
			return fmt.Errorf("update shop_member: %w", err)
		}
		if ct.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// Delete removes a membership row (the user is no longer part of the shop).
func (r *ShopMemberRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return db.WithTenantTx(ctx, r.pool, nil, uuid.Nil, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
		const q = `DELETE FROM shop_members WHERE id = $1`
		ct, err := tx.Exec(ctx, q, id)
		if err != nil {
			return fmt.Errorf("delete shop_member: %w", err)
		}
		if ct.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// --- helpers ----------------------------------------------------------------

func scanShopMember(s scanner, sm *models.ShopMember) error {
	var role string
	err := s.Scan(
		&sm.ID,
		&sm.ShopID,
		&sm.UserID,
		&role,
		&sm.Permissions,
		&sm.CreatedAt,
		&sm.UpdatedAt,
	)
	if err != nil {
		return err
	}
	sm.Role = models.UserRole(role)
	return nil
}
