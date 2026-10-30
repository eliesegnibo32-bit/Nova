// Package repository provides direct pgx access to NOVA's database tables
// without an ORM. Each repository is a thin wrapper around a `*pgxpool.Pool`
// that knows the SQL for one logical entity group.
//
// Concurrency: every method takes a `context.Context` so the caller can
// cancel in-flight DB work. Methods do NOT begin their own transactions
// unless they need atomicity across multiple statements; reads use
// pool.QueryRow / pool.Query directly.
//
// RLS: the `users` table is platform-level (not shop-scoped), but it still
// has RLS policies (migration 011) that allow:
//   - is_platform_admin()
//   - id = current_user_id()
//   - is_shop_member_of(current_shop_id(), id)
//
// The auth flow needs to look up users by email BEFORE authentication (no
// current_user_id), so the methods below run inside a transaction that sets
// `app.user_role = 'super_admin'` to bypass users-RLS for these privileged
// lookups. This is safe because the auth service is the only caller.
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

// ErrNotFound is returned by Get* methods when no row matched the query.
// Callers should map this to a 404 in the HTTP layer.
var ErrNotFound = errors.New("record not found")

// UserRepository handles the `users` table (platform-level accounts).
type UserRepository struct {
	pool *pgxpool.Pool
}

// NewUserRepository returns a UserRepository bound to the given pool.
func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{pool: pool}
}

// Create inserts a new user and returns the populated record. Email
// uniqueness is enforced by the DB (users.email UNIQUE); a duplicate will
// return a pgx error wrapping a Postgres unique_violation (code 23505) —
// callers can use pgconn.PgError.Code to detect it.
func (r *UserRepository) Create(
	ctx context.Context,
	email, passwordHash, fullName, phone string,
	role models.UserRole,
) (*models.User, error) {
	var u models.User
	err := db.WithTenantTx(ctx, r.pool, nil, uuid.Nil, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
		const q = `
			INSERT INTO users (email, password_hash, full_name, phone, role)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING
				id, email, phone, password_hash, full_name, role,
				two_factor_secret, two_factor_enabled,
				failed_login_count, locked_until, last_login_at,
				created_at, updated_at, deleted_at
		`
		row := tx.QueryRow(ctx, q, email, passwordHash, fullName, nullableString(phone), string(role))
		return scanUser(row, &u)
	})
	if err != nil {
		return nil, fmt.Errorf("user repo: create: %w", err)
	}
	return &u, nil
}

// GetByEmail returns the user with the given email (case-sensitive — callers
// should normalize email to lowercase before calling). Returns ErrNotFound
// if no user has that email.
func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*models.User, error) {
	var u models.User
	err := db.WithTenantTx(ctx, r.pool, nil, uuid.Nil, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
		const q = `
			SELECT
				id, email, phone, password_hash, full_name, role,
				two_factor_secret, two_factor_enabled,
				failed_login_count, locked_until, last_login_at,
				created_at, updated_at, deleted_at
			FROM users
			WHERE email = $1 AND deleted_at IS NULL
		`
		row := tx.QueryRow(ctx, q, email)
		return scanUser(row, &u)
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("user repo: get by email: %w", err)
	}
	return &u, nil
}

// GetByID returns the user with the given UUID. Returns ErrNotFound if no
// such user exists (or it has been soft-deleted).
func (r *UserRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.User, error) {
	var u models.User
	err := db.WithTenantTx(ctx, r.pool, nil, id, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
		const q = `
			SELECT
				id, email, phone, password_hash, full_name, role,
				two_factor_secret, two_factor_enabled,
				failed_login_count, locked_until, last_login_at,
				created_at, updated_at, deleted_at
			FROM users
			WHERE id = $1 AND deleted_at IS NULL
		`
		row := tx.QueryRow(ctx, q, id)
		return scanUser(row, &u)
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("user repo: get by id: %w", err)
	}
	return &u, nil
}

// UpdateLastLogin sets last_login_at = now() and resets failed_login_count
// and locked_until to 0/NULL (called after a successful login).
func (r *UserRepository) UpdateLastLogin(ctx context.Context, id uuid.UUID) error {
	return db.WithTenantTx(ctx, r.pool, nil, id, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
		const q = `
			UPDATE users
			   SET last_login_at = now(),
			       failed_login_count = 0,
			       locked_until = NULL,
			       updated_at = now()
			 WHERE id = $1
		`
		ct, err := tx.Exec(ctx, q, id)
		if err != nil {
			return fmt.Errorf("update last_login: %w", err)
		}
		if ct.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// SetTwoFactor stores the TOTP secret and toggles the enabled flag.
// `enabled=false` is used during the setup flow (we store the secret
// provisionally so the user can confirm with a code before we activate it).
// `enabled=true` is set by ConfirmTwoFactor. To fully disable, pass an
// empty secret and enabled=false.
func (r *UserRepository) SetTwoFactor(ctx context.Context, id uuid.UUID, secret string, enabled bool) error {
	return db.WithTenantTx(ctx, r.pool, nil, id, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
		var secretArg any
		if secret != "" {
			secretArg = secret
		}
		const q = `
			UPDATE users
			   SET two_factor_secret = $2,
			       two_factor_enabled = $3,
			       updated_at = now()
			 WHERE id = $1
		`
		ct, err := tx.Exec(ctx, q, id, secretArg, enabled)
		if err != nil {
			return fmt.Errorf("set two_factor: %w", err)
		}
		if ct.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// UpdatePassword replaces the password_hash and clears any locked_until /
// failed_login_count so the user can immediately log in with the new
// password. Used by ChangePassword and ResetPassword.
func (r *UserRepository) UpdatePassword(ctx context.Context, id uuid.UUID, passwordHash string) error {
	return db.WithTenantTx(ctx, r.pool, nil, id, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
		const q = `
			UPDATE users
			   SET password_hash = $2,
			       failed_login_count = 0,
			       locked_until = NULL,
			       updated_at = now()
			 WHERE id = $1
		`
		ct, err := tx.Exec(ctx, q, id, passwordHash)
		if err != nil {
			return fmt.Errorf("update password: %w", err)
		}
		if ct.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// IncrementFailedLogin bumps failed_login_count by 1 and, if the threshold
// is reached, sets locked_until = now() + lockDuration. The threshold check
// is intentionally simple (== N) — the rate limiter in auth.LoginRateLimiter
// handles the same logic in-process for fast rejection; this DB-side lock is
// a durable second line of defense in case the process restarts mid-attack.
func (r *UserRepository) IncrementFailedLogin(ctx context.Context, id uuid.UUID, threshold int, lockDuration time.Duration) error {
	return db.WithTenantTx(ctx, r.pool, nil, id, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
		const q = `
			UPDATE users
			   SET failed_login_count = failed_login_count + 1,
			       locked_until = CASE
			           WHEN failed_login_count + 1 >= $2 THEN now() + $3::interval
			           ELSE locked_until
			       END,
			       updated_at = now()
			 WHERE id = $1
		`
		// $2 is the threshold; $3 is the lock interval in seconds.
		_, err := tx.Exec(ctx, q, id, threshold, lockDuration.Seconds())
		if err != nil {
			return fmt.Errorf("increment failed_login: %w", err)
		}
		return nil
	})
}

// ResetFailedLogin clears failed_login_count and locked_until. Called on a
// successful login (along with UpdateLastLogin).
func (r *UserRepository) ResetFailedLogin(ctx context.Context, id uuid.UUID) error {
	return db.WithTenantTx(ctx, r.pool, nil, id, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
		const q = `
			UPDATE users
			   SET failed_login_count = 0,
			       locked_until = NULL,
			       updated_at = now()
			 WHERE id = $1
		`
		_, err := tx.Exec(ctx, q, id)
		if err != nil {
			return fmt.Errorf("reset failed_login: %w", err)
		}
		return nil
	})
}

// LockUntil sets locked_until to the given time. Used by admin-tooling or
// the rate limiter to explicitly lock an account.
func (r *UserRepository) LockUntil(ctx context.Context, id uuid.UUID, until time.Time) error {
	return db.WithTenantTx(ctx, r.pool, nil, id, string(models.RoleSuperAdmin), func(tx pgx.Tx) error {
		var untilArg any
		if !until.IsZero() {
			untilArg = until
		}
		const q = `
			UPDATE users
			   SET locked_until = $2,
			       updated_at = now()
			 WHERE id = $1
		`
		ct, err := tx.Exec(ctx, q, id, untilArg)
		if err != nil {
			return fmt.Errorf("lock_until: %w", err)
		}
		if ct.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// --- helpers ----------------------------------------------------------------

// scanner is the subset of pgx.Row / pgx.Rows used by scanUser.
type scanner interface {
	Scan(dest ...any) error
}

// scanUser maps a users row into a models.User. Phone, two_factor_secret,
// locked_until, last_login_at, deleted_at are NULLABLE.
func scanUser(s scanner, u *models.User) error {
	var (
		phone           *string
		passwordHash    *string
		twoFactorSecret *string
		lockedUntil     *time.Time
		lastLoginAt     *time.Time
		deletedAt       *time.Time
		role            string
	)
	err := s.Scan(
		&u.ID,
		&u.Email,
		&phone,
		&passwordHash,
		&u.FullName,
		&role,
		&twoFactorSecret,
		&u.TwoFactorEnabled,
		&u.FailedLoginCount,
		&lockedUntil,
		&lastLoginAt,
		&u.CreatedAt,
		&u.UpdatedAt,
		&deletedAt,
	)
	if err != nil {
		return err
	}
	u.Role = models.UserRole(role)
	u.Phone = phone
	u.PasswordHash = passwordHash
	u.TwoFactorSecret = twoFactorSecret
	u.LockedUntil = lockedUntil
	u.LastLoginAt = lastLoginAt
	u.DeletedAt = deletedAt
	return nil
}

// nullableString returns nil for an empty string so the column receives
// NULL instead of "". Useful for optional text fields.
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
