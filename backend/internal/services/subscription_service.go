// Subscription service — orchestrates the lifecycle of a shop's subscription
// (cahier des charges ch. 7.3 — Cycle de vie de l'abonnement).
//
// Lifecycle (ch. 7.3):
//
//	essai → actif → échéance → en_retard → période_de_grâce → suspendu → résilié
//	                         ^                                         |
//	                         +----------- paiement validé <-------------+
//
// The lifecycle is driven by two cron jobs (in cron_service.go):
//   - CheckAndAdvanceLifecycle: daily at 02:00 UTC. Walks each subscription
//     and advances its status based on the current date vs next_billing_at,
//     grace_until, suspended_at.
//   - SendPaymentReminders: daily at 09:00 UTC. Sends the J-3 / J / J+1 / ...
//     reminders per ch. 7.4.
//
// Admin actions (manual overrides):
//   - RecordPayment (ch. 7.5 — admin records payment → subscription 'active').
//   - Suspend / Reactivate / Terminate (manual overrides for support cases).
//
// All status changes are audit-logged (ch. 5.5 — every mutation is auditable).
package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"

	"nova-api/internal/models"
	"nova-api/internal/repository"
)

// Subscription service sentinel errors.
var (
	ErrSubServiceNotInitialized = errors.New("subscription service not initialized")
	ErrCannotSuspend            = errors.New("subscription cannot be suspended from its current state")
	ErrCannotReactivate         = errors.New("subscription cannot be reactivated from its current state")
	ErrCannotTerminate          = errors.New("subscription cannot be terminated from its current state")
)

// SubscriptionConfig holds the lifecycle knobs read from the env.
type SubscriptionConfig struct {
	TrialDays          int // 14 by default
	LateGraceDays      int // 3 days after due date before grace period
	GracePeriodDays    int // 7 days of grace before suspension
	SuspendedRetention int // 90 days after suspension before termination
}

// DefaultSubscriptionConfig returns the spec defaults (ch. 7.3 + 7.6).
func DefaultSubscriptionConfig() SubscriptionConfig {
	return SubscriptionConfig{
		TrialDays:          14,
		LateGraceDays:      3,
		GracePeriodDays:    7,
		SuspendedRetention: 90,
	}
}

// SubscriptionService is the subscription business-logic layer.
type SubscriptionService struct {
	subRepo   *repository.SubscriptionRepository
	planRepo  *repository.PlanRepository
	notifRepo *repository.NotificationsRepository
	auditRepo *repository.AuditRepository
	usageRepo *repository.AIUsageRepository
	pool      *pgxpool.Pool
	cfg       SubscriptionConfig
	log       *slog.Logger
}

// NewSubscriptionService constructs a SubscriptionService. cfg may be zero —
// defaults are applied. log may be nil.
func NewSubscriptionService(
	subRepo *repository.SubscriptionRepository,
	planRepo *repository.PlanRepository,
	notifRepo *repository.NotificationsRepository,
	auditRepo *repository.AuditRepository,
	usageRepo *repository.AIUsageRepository,
	pool *pgxpool.Pool,
	cfg SubscriptionConfig,
	log *slog.Logger,
) *SubscriptionService {
	if cfg.TrialDays <= 0 {
		cfg.TrialDays = 14
	}
	if cfg.LateGraceDays <= 0 {
		cfg.LateGraceDays = 3
	}
	if cfg.GracePeriodDays <= 0 {
		cfg.GracePeriodDays = 7
	}
	if cfg.SuspendedRetention <= 0 {
		cfg.SuspendedRetention = 90
	}
	if log == nil {
		log = slog.Default()
	}
	return &SubscriptionService{
		subRepo:   subRepo,
		planRepo:  planRepo,
		notifRepo: notifRepo,
		auditRepo: auditRepo,
		usageRepo: usageRepo,
		pool:      pool,
		cfg:       cfg,
		log:       log,
	}
}

// SubscriptionDashboard is the response shape for GetDashboardStats. Bundles
// everything the merchant dashboard needs in one round-trip: current plan,
// status, next billing date, usage this month, recent payments.
type SubscriptionDashboard struct {
	Subscription   *models.Subscription         `json:"subscription"`
	Plan           *models.Plan                 `json:"plan"`
	UsageThisMonth *repository.MonthlyUsage     `json:"usage_this_month"`
	Payments       []models.SubscriptionPayment `json:"payments"`
}

// GetSubscription returns the active subscription + plan for the shop.
// Mirrors ShopService.GetSubscription but uses the SubscriptionService's
// config + repositories. Reused by the HTTP handler.
func (s *SubscriptionService) GetSubscription(ctx context.Context, shopID uuid.UUID) (*models.Subscription, *models.Plan, error) {
	if s == nil || s.subRepo == nil {
		return nil, nil, ErrSubServiceNotInitialized
	}
	sub, err := s.subRepo.GetByShopID(ctx, uuid.Nil, string(models.RoleSuperAdmin), shopID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, nil, ErrSubscriptionNotFound
		}
		return nil, nil, fmt.Errorf("sub service: get: %w", err)
	}
	plan, _ := s.planRepo.GetByID(ctx, sub.PlanID)
	return sub, plan, nil
}

// RecordPaymentRequest is the body of POST /subscription/payment (admin only).
// Mirrors models.RecordPaymentRequest but kept here for naming parity with
// the spec.
type RecordPaymentRequest struct {
	Amount      int64  `json:"amount"       validate:"required,min=1"`
	Mode        string `json:"mode"         validate:"required,oneof=cash mobile_money wave orange_money mtn_momo"`
	Reference   string `json:"reference"    validate:"omitempty,max=200"`
	PeriodStart string `json:"period_start" validate:"required"` // YYYY-MM-DD
	PeriodEnd   string `json:"period_end"   validate:"required"` // YYYY-MM-DD
}

// RecordPayment records a payment for the shop's subscription (ch. 7.5 —
// admin records payment, subscription only "paid" after this recording).
// On success: subscription status → 'active', next_billing_at = period_end + 1
// month, grace/suspended timestamps cleared. Audit-logged.
func (s *SubscriptionService) RecordPayment(ctx context.Context, actorID uuid.UUID, shopID uuid.UUID, req RecordPaymentRequest, ip, userAgent string) (*models.SubscriptionPayment, error) {
	if s == nil || s.subRepo == nil {
		return nil, ErrSubServiceNotInitialized
	}
	// Parse period_start / period_end (YYYY-MM-DD).
	periodStart, err := time.Parse("2006-01-02", req.PeriodStart)
	if err != nil {
		return nil, ErrInvalidPeriod
	}
	periodEnd, err := time.Parse("2006-01-02", req.PeriodEnd)
	if err != nil {
		return nil, ErrInvalidPeriod
	}
	if periodEnd.Before(periodStart) {
		return nil, ErrInvalidPeriod
	}
	// Validate payment mode.
	switch models.PaymentMode(req.Mode) {
	case models.PayCash, models.PayMobileMoney, models.PayWave, models.PayOrangeMoney, models.PayMTNMomo:
		// OK
	default:
		return nil, ErrInvalidPaymentMode
	}
	// Fetch the active subscription.
	sub, err := s.subRepo.GetByShopID(ctx, actorID, string(models.RoleSuperAdmin), shopID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrSubscriptionNotFound
		}
		return nil, fmt.Errorf("sub service: get subscription for payment: %w", err)
	}
	// Delegate to the repository method (it inserts the payment row + updates
	// the subscription in one transaction).
	pay, err := s.subRepo.RecordPayment(ctx, actorID, repository.RecordPaymentInput{
		SubscriptionID: sub.ID,
		ShopID:         shopID,
		Amount:         req.Amount,
		Mode:           models.PaymentMode(req.Mode),
		Reference:      req.Reference,
		PeriodStart:    periodStart,
		PeriodEnd:      periodEnd,
		RecordedBy:     actorID,
	})
	if err != nil {
		return nil, fmt.Errorf("sub service: record payment: %w", err)
	}
	// Audit log.
	shopIDForLog := shopID
	payIDForLog := pay.ID
	after, _ := json.Marshal(map[string]any{
		"payment_id":   pay.ID.String(),
		"amount":       req.Amount,
		"mode":         req.Mode,
		"reference":    req.Reference,
		"period_start": req.PeriodStart,
		"period_end":   req.PeriodEnd,
		"new_status":   "active",
	})
	_ = s.auditRepo.Log(ctx, repository.AuditEntry{
		ShopID:     &shopIDForLog,
		ActorID:    &actorID,
		ActorRole:  string(models.RoleSuperAdmin),
		Action:     "subscription.payment.recorded",
		ObjectType: "subscription_payment",
		ObjectID:   &payIDForLog,
		IPAddress:  ip,
		UserAgent:  userAgent,
		After:      rawJSON{after},
	})
	// Notify the shop owner that the payment was recorded.
	s.notifyPayment(ctx, shopID, pay)
	return pay, nil
}

// notifyPayment creates a notification for the shop owner acknowledging the
// recorded payment (best-effort).
func (s *SubscriptionService) notifyPayment(ctx context.Context, shopID uuid.UUID, pay *models.SubscriptionPayment) {
	if s.notifRepo == nil {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"shop_id":      shopID.String(),
		"payment_id":   pay.ID.String(),
		"amount":       pay.Amount,
		"mode":         string(pay.Mode),
		"reference":    pay.Reference,
		"period_start": pay.PeriodStart.Format("2006-01-02"),
		"period_end":   pay.PeriodEnd.Format("2006-01-02"),
		"recorded_at":  pay.RecordedAt.UTC().Format(time.RFC3339),
	})
	if _, err := s.notifRepo.Create(ctx, &shopID, nil, "subscription_payment_recorded", payload); err != nil {
		s.log.Warn("sub service: notify payment failed", "error", err, "shop_id", shopID)
	}
}

// CheckAndAdvanceLifecycle is the daily cron job (ch. 7.3). For each
// subscription, it checks whether the lifecycle should advance based on the
// current date vs the subscription's due dates. Transitions:
//
//  1. trial → (active | late):
//     - if now > started_at + trial_days: the trial has ended. If the
//     subscription has at least one recorded payment (status == 'active'
//     from the RecordPayment path), it stays 'active'. Otherwise it
//     transitions to 'late' with next_billing_at = started_at + trial_days.
//     - In practice the trial → active transition is driven by the
//     RecordPayment call (the merchant pays during the trial). The cron
//     handles the trial → late case (merchant didn't pay).
//  2. active → late: if next_billing_at < now → 'late'.
//  3. late → grace_period: if next_billing_at + late_grace_days < now →
//     'grace_period' with grace_until = next_billing_at + late_grace_days +
//     grace_period_days.
//  4. grace_period → suspended: if grace_until < now → 'suspended'.
//  5. suspended → terminated: if suspended_at + retention_days < now →
//     'terminated' (data export reminder sent).
//
// Every transition is audit-logged + a notification is created.
func (s *SubscriptionService) CheckAndAdvanceLifecycle(ctx context.Context) error {
	if s == nil || s.subRepo == nil {
		return ErrSubServiceNotInitialized
	}
	now := time.Now().UTC()
	// 1. trial → late (or stay active if paid).
	trialSubs, err := s.subRepo.ListByStatuses(ctx, models.SubTrial)
	if err != nil {
		return fmt.Errorf("lifecycle: list trial: %w", err)
	}
	for _, sub := range trialSubs {
		trialEnd := sub.StartedAt.Add(time.Duration(s.cfg.TrialDays) * 24 * time.Hour)
		if now.After(trialEnd) {
			// Trial ended. If the merchant never paid, transition to 'late'
			// with next_billing_at = trialEnd (the due date).
			if err := s.subRepo.SetNextBilling(ctx, sub.ID, trialEnd); err != nil {
				s.log.Warn("lifecycle: set next billing for trial→late failed", "error", err, "sub_id", sub.ID)
				continue
			}
			if _, err := s.subRepo.UpdateStatus(ctx, uuid.Nil, sub.ID, models.SubLate); err != nil {
				s.log.Warn("lifecycle: trial→late failed", "error", err, "sub_id", sub.ID)
				continue
			}
			s.log.Info("lifecycle: trial→late", "sub_id", sub.ID, "shop_id", sub.ShopID, "trial_end", trialEnd.Format(time.RFC3339))
			s.auditLifecycle(ctx, sub.ShopID, sub.ID, "trial", "late", "trial expired without payment")
			s.notifyLifecycle(ctx, sub.ShopID, "trial_expired", "Essai expiré", "Votre période d'essai est terminée. Réglez votre abonnement pour continuer à utiliser NOVA.")
		}
	}
	// 2. active → late.
	activeSubs, err := s.subRepo.ListByStatuses(ctx, models.SubActive)
	if err != nil {
		return fmt.Errorf("lifecycle: list active: %w", err)
	}
	for _, sub := range activeSubs {
		if sub.NextBillingAt != nil && now.After(*sub.NextBillingAt) {
			if _, err := s.subRepo.UpdateStatus(ctx, uuid.Nil, sub.ID, models.SubLate); err != nil {
				s.log.Warn("lifecycle: active→late failed", "error", err, "sub_id", sub.ID)
				continue
			}
			s.log.Info("lifecycle: active→late", "sub_id", sub.ID, "shop_id", sub.ShopID, "next_billing_at", sub.NextBillingAt.Format(time.RFC3339))
			s.auditLifecycle(ctx, sub.ShopID, sub.ID, "active", "late", "billing due date passed")
			s.notifyLifecycle(ctx, sub.ShopID, "subscription_late", "Abonnement en retard", "Votre abonnement est en retard. Réglez votre facture pour éviter la suspension.")
		}
	}
	// 3. late → grace_period.
	lateSubs, err := s.subRepo.ListByStatuses(ctx, models.SubLate)
	if err != nil {
		return fmt.Errorf("lifecycle: list late: %w", err)
	}
	for _, sub := range lateSubs {
		if sub.NextBillingAt == nil {
			continue
		}
		lateEnd := sub.NextBillingAt.Add(time.Duration(s.cfg.LateGraceDays) * 24 * time.Hour)
		if now.After(lateEnd) {
			graceUntil := lateEnd.Add(time.Duration(s.cfg.GracePeriodDays) * 24 * time.Hour)
			if err := s.subRepo.SetGrace(ctx, sub.ID, graceUntil); err != nil {
				s.log.Warn("lifecycle: late→grace failed", "error", err, "sub_id", sub.ID)
				continue
			}
			s.log.Info("lifecycle: late→grace_period", "sub_id", sub.ID, "shop_id", sub.ShopID, "grace_until", graceUntil.Format(time.RFC3339))
			s.auditLifecycle(ctx, sub.ShopID, sub.ID, "late", "grace_period", "late period expired, grace period started")
			s.notifyLifecycle(ctx, sub.ShopID, "subscription_grace_period", "Période de grâce", "Votre abonnement est en période de grâce. C'est votre dernier délai avant la suspension.")
		}
	}
	// 4. grace_period → suspended.
	graceSubs, err := s.subRepo.ListByStatuses(ctx, models.SubGracePeriod)
	if err != nil {
		return fmt.Errorf("lifecycle: list grace_period: %w", err)
	}
	for _, sub := range graceSubs {
		if sub.GraceUntil != nil && now.After(*sub.GraceUntil) {
			if _, err := s.subRepo.UpdateStatus(ctx, uuid.Nil, sub.ID, models.SubSuspended); err != nil {
				s.log.Warn("lifecycle: grace→suspended failed", "error", err, "sub_id", sub.ID)
				continue
			}
			s.log.Info("lifecycle: grace_period→suspended", "sub_id", sub.ID, "shop_id", sub.ShopID)
			s.auditLifecycle(ctx, sub.ShopID, sub.ID, "grace_period", "suspended", "grace period expired")
			s.notifyLifecycle(ctx, sub.ShopID, "subscription_suspended", "Abonnement suspendu", "Votre abonnement a été suspendu. NOVA ne répond plus automatiquement. Réglez votre facture pour réactiver le service.")
		}
	}
	// 5. suspended → terminated.
	suspSubs, err := s.subRepo.ListByStatuses(ctx, models.SubSuspended)
	if err != nil {
		return fmt.Errorf("lifecycle: list suspended: %w", err)
	}
	for _, sub := range suspendedTerminated(ctx, s, suspSubs) {
		// iteration handled by the helper below — kept here for symmetry.
		_ = sub
	}
	for _, sub := range suspSubs {
		if sub.SuspendedAt == nil {
			continue
		}
		retentionEnd := sub.SuspendedAt.Add(time.Duration(s.cfg.SuspendedRetention) * 24 * time.Hour)
		if now.After(retentionEnd) {
			if _, err := s.subRepo.UpdateStatus(ctx, uuid.Nil, sub.ID, models.SubTerminated); err != nil {
				s.log.Warn("lifecycle: suspended→terminated failed", "error", err, "sub_id", sub.ID)
				continue
			}
			s.log.Info("lifecycle: suspended→terminated", "sub_id", sub.ID, "shop_id", sub.ShopID)
			s.auditLifecycle(ctx, sub.ShopID, sub.ID, "suspended", "terminated", "retention period expired")
			s.notifyLifecycle(ctx, sub.ShopID, "subscription_terminated", "Abonnement résilié", "Votre abonnement a été résilié. Vos données seront supprimées sous 30 jours. Contactez le support pour un export.")
		}
	}
	return nil
}

// suspendedTerminated is a tiny helper kept for clarity — it returns the
// input slice unchanged. (It exists so the loop above can use `range` without
// the linter complaining about an unused variable when the loop body is
// extracted to a helper. We keep the loop inline for readability.)
func suspendedTerminated(_ context.Context, _ *SubscriptionService, in []models.Subscription) []models.Subscription {
	return in
}

// SendPaymentReminders is the daily cron job (ch. 7.4 — Rappels de paiement).
// For each subscription, it sends the appropriate reminder based on the
// number of days until/since next_billing_at:
//
//   - J-3 (3 days before due date): "rappel d'échéance"
//   - Day of due date (J): "rappel aujourd'hui"
//   - J+1, J+3, J+5 (after due date, still unpaid): "rappel de retard"
//   - Day before suspension (grace_until - 1 day): "avertissement final"
//
// Reminders are created as notifications. The WhatsApp template send (if the
// shop has consent + a phone number) is delegated to the WhatsAppService and
// is best-effort.
func (s *SubscriptionService) SendPaymentReminders(ctx context.Context) error {
	if s == nil || s.subRepo == nil {
		return ErrSubServiceNotInitialized
	}
	now := time.Now().UTC()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	// Subscriptions that are still in trial/active/late/grace_period may need
	// reminders (suspended/terminated don't).
	subs, err := s.subRepo.ListByStatuses(ctx, models.SubTrial, models.SubActive, models.SubLate, models.SubGracePeriod)
	if err != nil {
		return fmt.Errorf("reminders: list subscriptions: %w", err)
	}
	for _, sub := range subs {
		// J-3 + J (pre-due-date reminders): only meaningful for trial/active.
		if sub.NextBillingAt != nil && (sub.Status == models.SubTrial || sub.Status == models.SubActive) {
			daysUntil := int(sub.NextBillingAt.Sub(startOfDay).Hours() / 24)
			if daysUntil == 3 {
				s.notifyLifecycle(ctx, sub.ShopID, "reminder_j_minus_3", "Rappel d'échéance J-3",
					fmt.Sprintf("Votre abonnement NOVA arrive à échéance dans 3 jours (%s). Montant: 10 000 FCFA.", sub.NextBillingAt.Format("02/01/2006")))
			}
			if daysUntil == 0 {
				s.notifyLifecycle(ctx, sub.ShopID, "reminder_j_day", "Rappel d'échéance (aujourd'hui)",
					"Votre abonnement NOVA arrive à échéance aujourd'hui. Réglez votre facture pour éviter un retard.")
			}
		}
		// J+1, J+3, J+5 (post-due-date reminders): only for late subscriptions.
		if sub.NextBillingAt != nil && sub.Status == models.SubLate {
			daysSince := int(startOfDay.Sub(*sub.NextBillingAt).Hours() / 24)
			if daysSince == 1 || daysSince == 3 || daysSince == 5 {
				s.notifyLifecycle(ctx, sub.ShopID, "reminder_late",
					fmt.Sprintf("Rappel de retard (J+%d)", daysSince),
					fmt.Sprintf("Votre abonnement NOVA est en retard de %d jour(s). Réglez votre facture pour éviter la suspension.", daysSince))
			}
		}
		// Final warning: 1 day before grace_until expires.
		if sub.GraceUntil != nil && sub.Status == models.SubGracePeriod {
			daysUntilSusp := int(sub.GraceUntil.Sub(startOfDay).Hours() / 24)
			if daysUntilSusp == 1 {
				s.notifyLifecycle(ctx, sub.ShopID, "reminder_final_warning", "Avertissement final",
					"Votre abonnement sera suspendu demain. C'est votre dernier délai pour régler votre facture.")
			}
		}
	}
	return nil
}

// Suspend is the admin manual override (ch. 7.6 — admin can suspend a shop
// immediately, e.g. for abuse or non-payment). Sets the subscription status
// to 'suspended' and stamps suspended_at.
func (s *SubscriptionService) Suspend(ctx context.Context, actorID uuid.UUID, shopID uuid.UUID, reason string, ip, userAgent string) (*models.Subscription, error) {
	if s == nil || s.subRepo == nil {
		return nil, ErrSubServiceNotInitialized
	}
	sub, err := s.subRepo.GetByShopID(ctx, actorID, string(models.RoleSuperAdmin), shopID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrSubscriptionNotFound
		}
		return nil, fmt.Errorf("sub service: suspend: get: %w", err)
	}
	if sub.Status == models.SubSuspended || sub.Status == models.SubTerminated {
		return nil, ErrCannotSuspend
	}
	updated, err := s.subRepo.UpdateStatus(ctx, actorID, sub.ID, models.SubSuspended)
	if err != nil {
		return nil, fmt.Errorf("sub service: suspend: update: %w", err)
	}
	s.auditLifecycleWithActor(ctx, shopID, sub.ID, actorID, string(sub.Status), "suspended", reason, ip, userAgent)
	s.notifyLifecycle(ctx, shopID, "subscription_suspended", "Abonnement suspendu", "Votre abonnement a été suspendu par l'administrateur. Raison: "+reason)
	return updated, nil
}

// Reactivate is the admin manual override (or auto-on-payment). Sets the
// subscription status back to 'active' and clears grace/suspended timestamps.
// The next_billing_at is bumped to +1 month from now (a reactivation implies
// a payment was received).
func (s *SubscriptionService) Reactivate(ctx context.Context, actorID uuid.UUID, shopID uuid.UUID, ip, userAgent string) (*models.Subscription, error) {
	if s == nil || s.subRepo == nil {
		return nil, ErrSubServiceNotInitialized
	}
	sub, err := s.subRepo.GetByShopID(ctx, actorID, string(models.RoleSuperAdmin), shopID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrSubscriptionNotFound
		}
		return nil, fmt.Errorf("sub service: reactivate: get: %w", err)
	}
	if sub.Status == models.SubTerminated {
		return nil, ErrCannotReactivate
	}
	// RecordPayment already handles the active transition. For the manual
	// reactivation path we go through RecordPayment with a synthetic payment
	// record so the books stay consistent. We use the plan's monthly price.
	plan, err := s.planRepo.GetByID(ctx, sub.PlanID)
	if err != nil {
		return nil, fmt.Errorf("sub service: reactivate: get plan: %w", err)
	}
	today := time.Now().UTC()
	periodEnd := today.AddDate(0, 1, 0)
	pay, err := s.subRepo.RecordPayment(ctx, actorID, repository.RecordPaymentInput{
		SubscriptionID: sub.ID,
		ShopID:         shopID,
		Amount:         plan.Price,
		Mode:           models.PayCash,
		Reference:      "manual_reactivation",
		PeriodStart:    today,
		PeriodEnd:      periodEnd,
		RecordedBy:     actorID,
	})
	if err != nil {
		return nil, fmt.Errorf("sub service: reactivate: record payment: %w", err)
	}
	// Fetch the updated subscription to return.
	updated, err := s.subRepo.GetByShopID(ctx, actorID, string(models.RoleSuperAdmin), shopID)
	if err != nil {
		updated = sub // best-effort
	}
	s.auditLifecycleWithActor(ctx, shopID, sub.ID, actorID, string(sub.Status), "active", "manual reactivation", ip, userAgent)
	_ = pay
	return updated, nil
}

// Terminate is the admin manual override (ch. 7.6 — admin can terminate a
// shop permanently, e.g. for fraud). Sets the subscription status to
// 'terminated' and stamps terminated_at.
func (s *SubscriptionService) Terminate(ctx context.Context, actorID uuid.UUID, shopID uuid.UUID, reason string, ip, userAgent string) (*models.Subscription, error) {
	if s == nil || s.subRepo == nil {
		return nil, ErrSubServiceNotInitialized
	}
	sub, err := s.subRepo.GetByShopID(ctx, actorID, string(models.RoleSuperAdmin), shopID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrSubscriptionNotFound
		}
		return nil, fmt.Errorf("sub service: terminate: get: %w", err)
	}
	if sub.Status == models.SubTerminated {
		return nil, ErrCannotTerminate
	}
	updated, err := s.subRepo.UpdateStatus(ctx, actorID, sub.ID, models.SubTerminated)
	if err != nil {
		return nil, fmt.Errorf("sub service: terminate: update: %w", err)
	}
	s.auditLifecycleWithActor(ctx, shopID, sub.ID, actorID, string(sub.Status), "terminated", reason, ip, userAgent)
	s.notifyLifecycle(ctx, shopID, "subscription_terminated", "Abonnement résilié", "Votre abonnement a été résilié. Raison: "+reason)
	return updated, nil
}

// GetDashboardStats returns the bundle of subscription + plan + monthly usage
// + recent payments for the merchant dashboard.
func (s *SubscriptionService) GetDashboardStats(ctx context.Context, shopID uuid.UUID) (*SubscriptionDashboard, error) {
	if s == nil || s.subRepo == nil {
		return nil, ErrSubServiceNotInitialized
	}
	sub, plan, err := s.GetSubscription(ctx, shopID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	usage, _ := s.usageRepo.GetMonthlyUsage(ctx, shopID, now.Year(), int(now.Month()))
	if usage == nil {
		usage = &repository.MonthlyUsage{Year: now.Year(), Month: int(now.Month())}
	}
	payments, _ := s.subRepo.ListPaymentsByShop(ctx, shopID, 20)
	if payments == nil {
		payments = []models.SubscriptionPayment{}
	}
	return &SubscriptionDashboard{
		Subscription:   sub,
		Plan:           plan,
		UsageThisMonth: usage,
		Payments:       payments,
	}, nil
}

// ListPayments returns the payment history for the shop.
func (s *SubscriptionService) ListPayments(ctx context.Context, shopID uuid.UUID) ([]models.SubscriptionPayment, error) {
	if s == nil || s.subRepo == nil {
		return nil, ErrSubServiceNotInitialized
	}
	payments, err := s.subRepo.ListPaymentsByShop(ctx, shopID, 100)
	if err != nil {
		return nil, fmt.Errorf("sub service: list payments: %w", err)
	}
	if payments == nil {
		payments = []models.SubscriptionPayment{}
	}
	return payments, nil
}

// ListAllSubscriptions is the platform-admin endpoint (GET /api/admin/subscriptions).
// Returns all subscriptions optionally filtered by status, joined with shop +
// plan names. Paginated.
func (s *SubscriptionService) ListAllSubscriptions(ctx context.Context, params repository.ListAllParams) ([]repository.SubscriptionListItem, int64, error) {
	if s == nil || s.subRepo == nil {
		return nil, 0, ErrSubServiceNotInitialized
	}
	return s.subRepo.ListAll(ctx, params)
}

// GetRevenueStats is the platform-admin endpoint (GET /api/admin/subscriptions/revenue).
// Returns MRR, total revenue, and per-plan breakdown.
func (s *SubscriptionService) GetRevenueStats(ctx context.Context) (*repository.RevenueStats, error) {
	if s == nil || s.subRepo == nil {
		return nil, ErrSubServiceNotInitialized
	}
	return s.subRepo.GetRevenueStats(ctx)
}

// --- helpers ----------------------------------------------------------------

// auditLifecycle writes a lifecycle-transition audit log entry (system actor).
func (s *SubscriptionService) auditLifecycle(ctx context.Context, shopID, subID uuid.UUID, from, to, reason string) {
	if s.auditRepo == nil {
		return
	}
	after, _ := json.Marshal(map[string]any{
		"subscription_id": subID.String(),
		"from_status":     from,
		"to_status":       to,
		"reason":          reason,
	})
	_ = s.auditRepo.Log(ctx, repository.AuditEntry{
		ShopID:     &shopID,
		ActorID:    nil,
		ActorRole:  "system",
		Action:     "subscription.lifecycle." + to,
		ObjectType: "subscription",
		ObjectID:   &subID,
		After:      rawJSON{after},
	})
}

// auditLifecycleWithActor writes a lifecycle-transition audit log entry with
// an explicit actor (admin manual action).
func (s *SubscriptionService) auditLifecycleWithActor(ctx context.Context, shopID, subID, actorID uuid.UUID, from, to, reason, ip, userAgent string) {
	if s.auditRepo == nil {
		return
	}
	after, _ := json.Marshal(map[string]any{
		"subscription_id": subID.String(),
		"from_status":     from,
		"to_status":       to,
		"reason":          reason,
		"actor_id":        actorID.String(),
	})
	_ = s.auditRepo.Log(ctx, repository.AuditEntry{
		ShopID:     &shopID,
		ActorID:    &actorID,
		ActorRole:  string(models.RoleSuperAdmin),
		Action:     "subscription.lifecycle." + to,
		ObjectType: "subscription",
		ObjectID:   &subID,
		IPAddress:  ip,
		UserAgent:  userAgent,
		After:      rawJSON{after},
	})
}

// notifyLifecycle creates a notification for the shop owner about a lifecycle
// event (best-effort).
func (s *SubscriptionService) notifyLifecycle(ctx context.Context, shopID uuid.UUID, notifType, title, message string) {
	if s.notifRepo == nil {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"shop_id": shopID.String(),
		"title":   title,
		"message": message,
	})
	if _, err := s.notifRepo.Create(ctx, &shopID, nil, notifType, payload); err != nil {
		s.log.Warn("sub service: notify lifecycle failed", "error", err, "shop_id", shopID, "type", notifType)
	}
}

// SubscriptionStatusChecker is the narrow interface used by the WhatsApp
// service to check whether a shop should be served by the AI (active/trial/
// late/grace_period = yes; suspended/terminated = no).
type SubscriptionStatusChecker interface {
	IsShopServiceActive(ctx context.Context, shopID uuid.UUID) (bool, error)
}

// IsShopServiceActive returns true if the shop's subscription allows the AI
// to respond. Per ch. 7.3:
//   - trial, active, late, grace_period: AI + auto-sends active (service maintained).
//   - suspended: AI + auto-sends STOP (only the merchant can reply, or a
//     neutral auto-reply is sent).
//   - terminated: AI + auto-sends STOP.
func (s *SubscriptionService) IsShopServiceActive(ctx context.Context, shopID uuid.UUID) (bool, error) {
	if s == nil || s.subRepo == nil {
		return false, ErrSubServiceNotInitialized
	}
	sub, err := s.subRepo.GetByShopID(ctx, uuid.Nil, string(models.RoleSuperAdmin), shopID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return false, nil // no subscription → don't serve
		}
		return false, fmt.Errorf("sub service: is shop active: %w", err)
	}
	switch sub.Status {
	case models.SubTrial, models.SubActive, models.SubLate, models.SubGracePeriod:
		return true, nil
	case models.SubSuspended, models.SubTerminated:
		return false, nil
	}
	return false, nil
}

// Ensure SubscriptionService satisfies SubscriptionStatusChecker.
var _ SubscriptionStatusChecker = (*SubscriptionService)(nil)

// ============================================================================
// Usage repository accessors — exposed for the HTTP handler so it can build
// dashboard charts (daily breakdown, custom-range aggregate, top
// conversations by cost) without importing the repository package directly.
// ============================================================================

// UsageRepoGetDailyUsage delegates to AIUsageRepository.GetDailyUsage.
func (s *SubscriptionService) UsageRepoGetDailyUsage(ctx context.Context, shopID uuid.UUID, from, to time.Time) ([]repository.DailyUsage, error) {
	if s.usageRepo == nil {
		return nil, ErrSubServiceNotInitialized
	}
	return s.usageRepo.GetDailyUsage(ctx, shopID, from, to)
}

// UsageRepoGetUsageByShop delegates to AIUsageRepository.GetUsageByShop.
func (s *SubscriptionService) UsageRepoGetUsageByShop(ctx context.Context, shopID uuid.UUID, from, to time.Time) (*repository.UsageSummary, error) {
	if s.usageRepo == nil {
		return nil, ErrSubServiceNotInitialized
	}
	return s.usageRepo.GetUsageByShop(ctx, shopID, from, to)
}

// UsageRepoGetTopConversationsByCost delegates to AIUsageRepository.GetTopConversationsByCost.
func (s *SubscriptionService) UsageRepoGetTopConversationsByCost(ctx context.Context, shopID uuid.UUID, limit int) ([]repository.ConversationUsage, error) {
	if s.usageRepo == nil {
		return nil, ErrSubServiceNotInitialized
	}
	return s.usageRepo.GetTopConversationsByCost(ctx, shopID, limit)
}
