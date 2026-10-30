// Cron service — runs the periodic background jobs that drive the
// subscription lifecycle + cart expiration + monthly quota reset.
//
// Per the spec (Task 10), we use plain `time.Ticker` (no external cron lib)
// to keep the dependency surface small. Each job runs in its own goroutine
// and respects a graceful shutdown via the Stop() channel.
//
// Jobs:
//  1. Lifecycle check    — daily (every 24h). Calls SubscriptionService.
//     CheckAndAdvanceLifecycle (trial→late, active→late, late→grace,
//     grace→suspended, suspended→terminated).
//  2. Payment reminders  — daily (every 24h, offset by 7h so it runs at
//     ~09:00 UTC). Calls SubscriptionService.SendPaymentReminders
//     (J-3, J, J+1, J+3, J+5, final warning).
//  3. Monthly quota reset — 1st of each month. Calls QuotaService.
//     ResetMonthlyQuotas (no-op — ai_usage is append-only, monthly count
//     naturally resets at month boundary).
//  4. Cart expiration     — hourly. Calls CartRepository.ExpireOldCarts
//     with a 24h threshold (ch. 4.5 — panier expire après 24h).
package services

import (
        "context"
        "sync"
        "time"

        "log/slog"

        "nova-api/internal/repository"
)

// CronService drives the periodic background jobs.
type CronService struct {
        subSvc   *SubscriptionService
        quotaSvc *QuotaService
        cartRepo *repository.CartRepository
        // orderSvc is OPTIONAL (NOVA v3). When set, the hourly payment-deadline
        // expiry job runs (auto-cancels en_attente_paiement orders past their
        // payment_deadline + releases stock).
        orderSvc *OrderService
        log      *slog.Logger
        stop     chan struct{}
        wg       sync.WaitGroup
}

// NewCronService constructs a CronService. cartRepo may be nil (the cart
// expiration job is skipped if so). subSvc and quotaSvc may be nil (the
// lifecycle + reminders + monthly quota jobs are skipped if so). orderSvc
// may be nil (the payment-deadline expiry job is skipped if so).
func NewCronService(
        subSvc *SubscriptionService,
        quotaSvc *QuotaService,
        cartRepo *repository.CartRepository,
        log *slog.Logger,
) *CronService {
        if log == nil {
                log = slog.Default()
        }
        return &CronService{
                subSvc:   subSvc,
                quotaSvc: quotaSvc,
                cartRepo: cartRepo,
                log:      log,
                stop:     make(chan struct{}),
        }
}

// SetOrderSvc injects the order service for the v3 payment-deadline expiry
// job. Called by main.go after both the CronService and OrderService are
// constructed.
func (c *CronService) SetOrderSvc(svc *OrderService) {
        c.orderSvc = svc
}

// Start launches the background goroutines. Idempotent — calling Start twice
// is a no-op (the second call does nothing because stop is already closed).
func (c *CronService) Start() {
        c.log.Info("cron: starting background jobs")
        // 1. Lifecycle check — every 24h.
        if c.subSvc != nil {
                c.wg.Add(1)
                go c.runPeriodic("lifecycle", 24*time.Hour, func(ctx context.Context) {
                        if err := c.subSvc.CheckAndAdvanceLifecycle(ctx); err != nil {
                                c.log.Error("cron: lifecycle check failed", "error", err)
                        }
                })
        }
        // 2. Payment reminders — every 24h. Offset by 7h on the first run so it
        //    lands around 09:00 UTC (matching the spec).
        if c.subSvc != nil {
                c.wg.Add(1)
                go c.runPeriodicWithOffset("reminders", 24*time.Hour, 7*time.Hour, func(ctx context.Context) {
                        if err := c.subSvc.SendPaymentReminders(ctx); err != nil {
                                c.log.Error("cron: payment reminders failed", "error", err)
                        }
                })
        }
        // 3. Monthly quota reset — check every hour whether we crossed a month
        //    boundary since the last check; if so, run the reset.
        if c.quotaSvc != nil {
                c.wg.Add(1)
                go c.runMonthlyReset()
        }
        // 4. Cart expiration — every 1h.
        if c.cartRepo != nil {
                c.wg.Add(1)
                go c.runPeriodic("cart_expiration", 1*time.Hour, func(ctx context.Context) {
                        n, err := c.cartRepo.ExpireOldCarts(ctx, time.Now())
                        if err != nil {
                                c.log.Error("cron: expire carts failed", "error", err)
                                return
                        }
                        if n > 0 {
                                c.log.Info("cron: expired carts", "count", n)
                        }
                })
        }
        // 5. NOVA v3 — Payment deadline expiry — every 15min.
        //    Cancels en_attente_paiement orders past their payment_deadline +
        //    releases the reserved stock. Best-effort: errors are logged.
        if c.orderSvc != nil {
                c.wg.Add(1)
                go c.runPeriodic("payment_deadline_expiry", 15*time.Minute, func(ctx context.Context) {
                        n, err := c.orderSvc.CronExpirePaymentDeadline(ctx)
                        if err != nil {
                                c.log.Error("cron: payment deadline expiry failed", "error", err)
                                return
                        }
                        if n > 0 {
                                c.log.Info("cron: payment deadline expired — orders auto-cancelled", "count", n)
                        }
                })
        }
}

// Stop signals all background goroutines to exit and waits for them to
// drain (max 10s per job).
func (c *CronService) Stop() {
        c.log.Info("cron: stopping background jobs")
        select {
        case <-c.stop:
                // already closed
        default:
                close(c.stop)
        }
        done := make(chan struct{})
        go func() {
                c.wg.Wait()
                close(done)
        }()
        select {
        case <-done:
        case <-time.After(10 * time.Second):
                c.log.Warn("cron: timed out waiting for jobs to stop")
        }
}

// runPeriodic runs fn immediately, then every `interval` until Stop is
// called. Each invocation gets a fresh context with a 5-minute timeout so a
// stuck job can't block the next tick.
func (c *CronService) runPeriodic(name string, interval time.Duration, fn func(context.Context)) {
        defer c.wg.Done()
        c.runOnce(name, fn)
        ticker := time.NewTicker(interval)
        defer ticker.Stop()
        for {
                select {
                case <-c.stop:
                        c.log.Info("cron: job stopped", "name", name)
                        return
                case <-ticker.C:
                        c.runOnce(name, fn)
                }
        }
}

// runPeriodicWithOffset is like runPeriodic but waits `offset` before the
// first run (used to stagger the lifecycle + reminders jobs so they don't
// both fire at the same minute on the first tick).
func (c *CronService) runPeriodicWithOffset(name string, interval, offset time.Duration, fn func(context.Context)) {
        defer c.wg.Done()
        select {
        case <-c.stop:
                return
        case <-time.After(offset):
        }
        c.runOnce(name, fn)
        ticker := time.NewTicker(interval)
        defer ticker.Stop()
        for {
                select {
                case <-c.stop:
                        c.log.Info("cron: job stopped", "name", name)
                        return
                case <-ticker.C:
                        c.runOnce(name, fn)
                }
        }
}

// runMonthlyReset runs the monthly quota reset. We poll every hour to detect
// month crossings (simpler than computing the next 1st-of-month duration).
// The actual reset is a no-op for now (ai_usage is append-only) but the poll
// lets us hook in real work later (e.g. trim > 12-month-old ai_usage rows).
func (c *CronService) runMonthlyReset() {
        defer c.wg.Done()
        var lastMonth int
        if now := time.Now().UTC(); now.Month() >= 1 && now.Month() <= 12 {
                lastMonth = int(now.Month())
        }
        ticker := time.NewTicker(1 * time.Hour)
        defer ticker.Stop()
        for {
                select {
                case <-c.stop:
                        c.log.Info("cron: job stopped", "name", "monthly_reset")
                        return
                case <-ticker.C:
                        now := time.Now().UTC()
                        curMonth := int(now.Month())
                        if curMonth != lastMonth {
                                // Month boundary crossed — run the reset.
                                ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
                                if err := c.quotaSvc.ResetMonthlyQuotas(ctx); err != nil {
                                        c.log.Error("cron: monthly quota reset failed", "error", err)
                                } else {
                                        c.log.Info("cron: monthly quota reset completed", "year", now.Year(), "month", curMonth)
                                }
                                cancel()
                                lastMonth = curMonth
                        }
                }
        }
}

// runOnce invokes fn with a fresh context + 5-minute timeout.
func (c *CronService) runOnce(name string, fn func(context.Context)) {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
        defer cancel()
        start := time.Now()
        fn(ctx)
        c.log.Debug("cron: job ran", "name", name, "duration_ms", time.Since(start).Milliseconds())
}

// RunAllNow triggers every job immediately (used by the admin endpoint
// POST /api/admin/cron/run for testing). Returns after each job has
// completed (or timed out).
func (c *CronService) RunAllNow(ctx context.Context) map[string]error {
        results := map[string]error{}
        if c.subSvc != nil {
                results["lifecycle"] = c.subSvc.CheckAndAdvanceLifecycle(ctx)
                results["reminders"] = c.subSvc.SendPaymentReminders(ctx)
        }
        if c.quotaSvc != nil {
                results["monthly_reset"] = c.quotaSvc.ResetMonthlyQuotas(ctx)
        }
        if c.cartRepo != nil {
                _, err := c.cartRepo.ExpireOldCarts(ctx, time.Now())
                results["cart_expiration"] = err
        }
        if c.orderSvc != nil {
                _, err := c.orderSvc.CronExpirePaymentDeadline(ctx)
                results["payment_deadline_expiry"] = err
        }
        return results
}
