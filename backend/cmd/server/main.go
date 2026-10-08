// Command nova-api starts the NOVA backend HTTP server.
//
// Startup sequence:
//  1. Load configuration from environment variables.
//  2. Initialize the slog JSON logger with the configured level.
//  3. Connect to PostgreSQL via pgxpool.
//  4. Run goose migrations (embedded).
//  5. Build the auth-related dependencies (rate limiter, repositories,
//     auth service) — skipped if the pool is nil (degraded mode).
//  6. Build the HTTP router.
//  7. Start the HTTP server on cfg.Port.
//  8. Wait for SIGINT/SIGTERM and shut down gracefully (30s timeout).
//
// Failure mode: if the database is unreachable at startup in production,
// the server logs a fatal error and exits non-zero. In development we keep
// going so the process boots and /health responds (useful when bringing up
// the stack with `docker compose up`).
package main

import (
        "context"
        "errors"
        "fmt"
        "net/http"
        "os"
        "os/signal"
        "syscall"
        "time"

        "log/slog"

        "github.com/jackc/pgx/v5/pgxpool"

        "nova-api/internal/ai"
        "nova-api/internal/api"
        "nova-api/internal/api/handlers"
        "nova-api/internal/auth"
        "nova-api/internal/config"
        "nova-api/internal/db"
        "nova-api/internal/repository"
        "nova-api/internal/services"
	"nova-api/internal/storage"
        "nova-api/internal/whatsapp"
)

func main() {
        if err := run(); err != nil {
                fmt.Fprintf(os.Stderr, "nova-api: fatal: %v\n", err)
                os.Exit(1)
        }
}

func run() error {
        // 1. Config ----------------------------------------------------------
        cfg, err := config.Load()
        if err != nil {
                return fmt.Errorf("load config: %w", err)
        }

        // 2. Logger ----------------------------------------------------------
        log := newLogger(cfg.LogLevel, cfg.IsProd())
        log.Info("nova-api starting",
                "environment", cfg.Environment,
                "port", cfg.Port,
                "cors_origins", cfg.CORSAllowedOrigins,
        )

        // 3. DB pool ---------------------------------------------------------
        // We tolerate an empty DATABASE_URL in dev (degraded mode); the migrator
        // and handlers will simply report the missing DB.
        var pool *pgxpool.Pool
        if cfg.DatabaseURL != "" {
                p, err := db.NewPool(context.Background(), cfg.DatabaseURL)
                if err != nil {
                        if cfg.IsProd() {
                                return fmt.Errorf("connect database: %w", err)
                        }
                        log.Error("database connection failed (dev mode, continuing)", "error", err)
                } else {
                        pool = p
                        log.Info("database connected")
                }
        } else {
                log.Warn("DATABASE_URL is empty — running in degraded mode (no DB)")
        }

        // 4. Migrations ------------------------------------------------------
        if cfg.DatabaseURL != "" && pool != nil {
                migCtx, migCancel := context.WithTimeout(context.Background(), 60*time.Second)
                if err := db.RunMigrations(migCtx, cfg.DatabaseURL); err != nil {
                        migCancel()
                        if cfg.IsProd() {
                                return fmt.Errorf("run migrations: %w", err)
                        }
                        log.Error("migrations failed (dev mode, continuing)", "error", err)
                } else {
                        log.Info("migrations applied")
                }
                migCancel()
        }

        // 5. Auth dependencies ----------------------------------------------
        // Build the rate limiter, repositories, and auth service. In degraded
        // mode (no pool), authSvc stays nil and the auth handlers return 503.
        var authSvc *services.AuthService
        var shopSvc *services.ShopService
        var catalogSvc *services.CatalogService
        var stockSvc *services.StockService
        var deliverySvc *services.DeliveryService
        var orderSvc *services.OrderService
        var aiSvc *services.AIService
        var subSvc *services.SubscriptionService
        var quotaSvc *services.QuotaService
        var cronSvc *services.CronService
        var waClient *whatsapp.Client
        var waProcessor *whatsapp.Processor
        var waTemplateMgr *whatsapp.TemplateManager
        var waSvc *services.WhatsAppService
        if pool != nil {
                // Rate limiter: 5 attempts / 15 min lock (overridable via env).
                // Defaults are also enforced inside NewLoginRateLimiter.
                maxAttempts := parseIntEnv("LOGIN_MAX_ATTEMPTS", 5)
                lockDuration := parseDurationEnv("LOGIN_LOCK_DURATION", 15*time.Minute)
                sessionDuration := parseDurationEnv("SESSION_DURATION", 7*24*time.Hour)
                adminSessionDuration := parseDurationEnv("ADMIN_SESSION_DURATION", 24*time.Hour)

                rateLimiter := auth.NewLoginRateLimiter(maxAttempts, lockDuration)
                rateLimiter.StartCleanup(5 * time.Minute)
                defer rateLimiter.Stop()

                userRepo := repository.NewUserRepository(pool)
                memberRepo := repository.NewShopMemberRepository(pool)
                auditRepo := repository.NewAuditRepository(pool)
                shopRepo := repository.NewShopRepository(pool)
                planRepo := repository.NewPlanRepository(pool)
                subRepo := repository.NewSubscriptionRepository(pool)
                productRepo := repository.NewProductRepository(pool)
                inventoryRepo := repository.NewInventoryRepository(pool)
                zoneRepo := repository.NewDeliveryZoneRepository(pool)
                cartRepo := repository.NewCartRepository(pool)
                orderRepo := repository.NewOrderRepository(pool)
                customerRepo := repository.NewCustomerRepository(pool)
                conversationRepo := repository.NewConversationRepository(pool)
                aiUsageRepo := repository.NewAIUsageRepository(pool)
                notifRepo := repository.NewNotificationsRepository(pool)

                authSvc = services.NewAuthService(
                        userRepo, memberRepo, auditRepo, rateLimiter,
                        []byte(cfg.SessionSecret),
                        sessionDuration, adminSessionDuration,
                        maxAttempts, lockDuration,
                )
                log.Info("auth service initialized",
                        "login_max_attempts", maxAttempts,
                        "login_lock_duration", lockDuration.String(),
                        "session_duration", sessionDuration.String(),
                        "admin_session_duration", adminSessionDuration.String(),
                )

                shopSvc = services.NewShopService(
                        shopRepo, planRepo, subRepo, memberRepo, userRepo, auditRepo,
                        pool,
                        []byte(cfg.SessionSecret),
                        sessionDuration, adminSessionDuration,
                )
                log.Info("shop service initialized")

                catalogSvc = services.NewCatalogService(productRepo, inventoryRepo, auditRepo, pool)
                stockSvc = services.NewStockService(inventoryRepo, auditRepo, pool)
                deliverySvc = services.NewDeliveryService(zoneRepo, auditRepo, pool)
                orderSvc = services.NewOrderService(orderRepo, cartRepo, inventoryRepo, productRepo, zoneRepo, auditRepo, pool)
                log.Info("catalog + stock + delivery + order services initialized")

                // AI engine + service. The provider is selected by the
                // AI_PROVIDER env var ("mock" by default; "openai" for the real
                // provider). The mock provider is deterministic and simulates
                // tool calls so the full flow (tools + guardrail + ai_usage)
                // can be tested without a real LLM API key.
                //
                // When AI_API_KEY is empty, we force MockProvider (ch. 5.6 — mode
                // test) regardless of AI_PROVIDER, so the server boots cleanly
                // in development without an OpenAI account.
                aiConfig := ai.LoadAIConfig()
                providerName := getenv("AI_PROVIDER", "mock")
                if aiConfig.IsMock() {
                        providerName = "mock"
                }
                var aiProvider ai.Provider
                switch providerName {
                case "openai":
                        aiProvider = ai.NewOpenAIProvider(aiConfig.APIKey, aiConfig.BaseURL, aiConfig.Model, nil)
                default:
                        aiProvider = ai.NewMockProvider(aiConfig.Model)
                }
                aiCfg := aiConfig.ToEngineConfig()
                // Override MaxConversationTurns with the AI_MAX_TURNS env var (kept
                // for backwards compatibility — LoadAIConfig doesn't surface it).
                if n := parseIntEnv("AI_MAX_TURNS", 0); n > 0 {
                        aiCfg.MaxConversationTurns = n
                }
                engineDeps := services.NewAIEngineDeps(
                        aiProvider, aiCfg,
                        shopRepo, customerRepo, conversationRepo, aiUsageRepo, auditRepo,
                        catalogSvc, stockSvc, deliverySvc, orderSvc, notifRepo,
                        repository.NewProductOptionRepository(pool),
                        repository.NewPaymentConfigRepository(pool),
                        pool, log,
                )
                // Task 10 — wire the QuotaService into the engine so the
                // degraded-mode check runs before every LLM call.
                // We construct the QuotaService + SubscriptionService first
                // (they don't depend on the engine) and inject QuotaSvc here.
                subCfg := services.SubscriptionConfig{
                        TrialDays:          parseIntEnv("SUBSCRIPTION_TRIAL_DAYS", 14),
                        LateGraceDays:      parseIntEnv("SUBSCRIPTION_LATE_GRACE_DAYS", 3),
                        GracePeriodDays:    parseIntEnv("SUBSCRIPTION_GRACE_PERIOD_DAYS", 7),
                        SuspendedRetention: parseIntEnv("SUBSCRIPTION_SUSPENDED_RETENTION_DAYS", 90),
                }
                quotaCfg := services.QuotaConfig{
                        Threshold80:  parseIntEnv("QUOTA_ALERT_THRESHOLD_80", 80),
                        Threshold100: parseIntEnv("QUOTA_ALERT_THRESHOLD_100", 100),
                }
                subSvc = services.NewSubscriptionService(subRepo, planRepo, notifRepo, auditRepo, aiUsageRepo, pool, subCfg, log)
                quotaSvc = services.NewQuotaService(planRepo, subRepo, aiUsageRepo, notifRepo, auditRepo, pool, quotaCfg, log)
                engineDeps.QuotaSvc = quotaSvc
                engine := ai.NewEngine(engineDeps)
                debouncer := ai.NewDebouncer(time.Duration(aiCfg.DebounceMs) * time.Millisecond)
                aiSvc = services.NewAIService(engine, conversationRepo, customerRepo, aiUsageRepo, notifRepo, auditRepo, debouncer, pool, log)
                log.Info("ai engine initialized",
                        "provider", aiProvider.Name(),
                        "model", cfg.AIModel,
                        "debounce_ms", aiCfg.DebounceMs,
                        "max_turns", aiCfg.MaxConversationTurns,
                        "max_retries", aiCfg.MaxRetries,
                        "max_history", aiCfg.MaxHistoryMessages,
                )
                log.Info("subscription + quota services initialized",
                        "trial_days", subCfg.TrialDays,
                        "late_grace_days", subCfg.LateGraceDays,
                        "grace_period_days", subCfg.GracePeriodDays,
                        "suspended_retention_days", subCfg.SuspendedRetention,
                        "quota_threshold_80", quotaCfg.Threshold80,
                        "quota_threshold_100", quotaCfg.Threshold100,
                )

                // WhatsApp Cloud API client + processor + template manager
                // (Task 9 — Canal WhatsApp). The client runs in mock mode
                // when WHATSAPP_ACCESS_TOKEN is empty — useful for local dev
                // and for the e2e tests in the worklog.
                waClient = whatsapp.NewClient(whatsapp.WhatsAppConfig{
                        VerifyToken:   cfg.WhatsAppVerifyToken,
                        AccessToken:   cfg.WhatsAppAccessToken,
                        PhoneNumberID: cfg.WhatsAppPhoneNumberID,
                        AppSecret:     cfg.WhatsAppAppSecret,
                        BaseURL:       cfg.WhatsAppBaseURL,
                        Version:       cfg.WhatsAppVersion,
                }, log)
                // The processor's subChecker is the SubscriptionService (Task 10).
                waProcessor = whatsapp.NewProcessor(waClient, shopRepo, customerRepo, conversationRepo, services.NewAIProcessorAdapter(aiSvc), subSvc, pool, log)
                tplRepo := repository.NewMessageTemplateRepository(pool)
                waTemplateMgr = whatsapp.NewTemplateManager(waClient, tplRepo, pool)
                // Best-effort: seed the NOVA pre-defined templates (idempotent).
                // Errors are logged but don't fail the boot.
                if err := waTemplateMgr.EnsureNovaTemplates(context.Background()); err != nil {
                        log.Warn("whatsapp: ensure nova templates failed (best-effort)", "error", err)
                } else {
                        log.Info("whatsapp: nova templates ensured")
                }
                // WhatsAppService facade (spec: services.WhatsAppService). Wraps the
                // client + processor + sender + template manager for the HTTP layer.
                // subChecker is the SubscriptionService (Task 10 — ch. 7.3).
                waSender := whatsapp.NewSender(waClient, conversationRepo, log)
                waMessageRepo := repository.NewMessageRepository(pool)
                waSvc = services.NewWhatsAppService(
                        waClient, waTemplateMgr, waProcessor, waSender,
                        shopRepo, customerRepo, conversationRepo, waMessageRepo,
                        aiSvc, subSvc, pool, log,
                )
                log.Info("whatsapp integration initialized",
                        "mock_mode", waClient.IsMock(),
                        "phone_number_id", cfg.WhatsAppPhoneNumberID,
                        "verify_token_set", cfg.WhatsAppVerifyToken != "",
                        "app_secret_set", cfg.WhatsAppAppSecret != "",
                )

                // NOVA v3 — payment config + product option repositories.
                paymentCfgRepo := repository.NewPaymentConfigRepository(pool)
                // Wire the payment config repo into the order service for the v3 flow
                // (merchant confirm → en_attente_paiement OR en_cours depending on mode).
                orderSvc.SetPaymentConfigRepo(paymentCfgRepo)
                log.Info("nova v3: payment config + product option repos initialized")

                // Cron service (Task 10 — ch. 7.3 lifecycle + reminders +
                // ch. 4.5 cart expiration + ch. 7.2 monthly quota reset +
                // NOVA v3 — payment deadline expiry).
                // Pass the cartRepo so the hourly cart-expiration job runs.
                cronSvc = services.NewCronService(subSvc, quotaSvc, cartRepo, log)
                // Wire the order service so the payment-deadline expiry job runs.
                cronSvc.SetOrderSvc(orderSvc)
                cronSvc.Start()
                log.Info("cron service started (lifecycle, reminders, monthly reset, cart expiration, payment deadline expiry)")
        } else {
                log.Warn("auth + shop + catalog + ai services NOT initialized — endpoints will return 503")
        }

        // 6. Cloudflare R2 storage (optional — media upload) ----------------
        var r2Store *storage.R2Store
        r2AccessKey := getenv("R2_ACCESS_KEY", "")
        r2SecretKey := getenv("R2_SECRET_KEY", "")
        r2AccountID := getenv("R2_ACCOUNT_ID", "")
        r2Bucket := getenv("R2_BUCKET", "nova-media")
        r2PublicURL := getenv("R2_PUBLIC_BASE_URL", "")
        if r2AccessKey != "" && r2SecretKey != "" {
                var r2Err error
                r2Store, r2Err = storage.NewR2Store(storage.Config{
                        AccountID:     r2AccountID,
                        AccessKey:     r2AccessKey,
                        SecretKey:     r2SecretKey,
                        Bucket:        r2Bucket,
                        PublicBaseURL: r2PublicURL,
                })
                if r2Err != nil {
                        log.Error("R2 storage init failed", "error", r2Err)
                        r2Store = nil
                } else {
                        log.Info("R2 storage initialized", "bucket", r2Bucket)
                }
        } else {
                log.Info("R2 storage not configured — upload endpoints disabled")
        }

        // 7. Router ----------------------------------------------------------
        var productOptionH *handlers.ProductOptionHandler
        var paymentCfgH *handlers.PaymentConfigHandler
        if pool != nil {
                productOptionH = handlers.NewProductOptionHandler(repository.NewProductOptionRepository(pool))
                paymentCfgH = handlers.NewPaymentConfigHandler(repository.NewPaymentConfigRepository(pool))
        }
        handler := api.New(cfg, pool, log, authSvc, shopSvc, catalogSvc, stockSvc, deliverySvc, orderSvc, aiSvc, waProcessor, waClient, waTemplateMgr, waSvc, subSvc, quotaSvc, cronSvc, productOptionH, paymentCfgH, r2Store)

        // 7. HTTP server -----------------------------------------------------
        srv := &http.Server{
                Addr:              ":" + cfg.Port,
                Handler:           handler,
                ReadHeaderTimeout: 10 * time.Second,
                ReadTimeout:       30 * time.Second,
                WriteTimeout:      30 * time.Second,
                IdleTimeout:       120 * time.Second,
        }

        serverErr := make(chan error, 1)
        go func() {
                log.Info("http server listening", "addr", srv.Addr)
                if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
                        serverErr <- err
                        return
                }
                serverErr <- nil
        }()

        // 8. Graceful shutdown ----------------------------------------------
        quit := make(chan os.Signal, 1)
        signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

        select {
        case err := <-serverErr:
                if err != nil {
                        return fmt.Errorf("http server: %w", err)
                }
        case sig := <-quit:
                log.Info("shutdown signal received", "signal", sig.String())
        }

        shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer shutdownCancel()

        if err := srv.Shutdown(shutdownCtx); err != nil {
                return fmt.Errorf("http shutdown: %w", err)
        }
        log.Info("http server stopped cleanly")

        // Stop the cron service (Task 10) — gracefully drains background jobs.
        if cronSvc != nil {
                cronSvc.Stop()
                log.Info("cron service stopped")
        }

        if pool != nil {
                pool.Close()
                log.Info("database pool closed")
        }
        return nil
}

// newLogger builds the slog.Logger from the configured level/environment.
// In production we emit JSON; in dev we emit text for readability in the
// terminal.
func newLogger(level string, prod bool) *slog.Logger {
        var lvl slog.Level
        switch level {
        case "debug":
                lvl = slog.LevelDebug
        case "info":
                lvl = slog.LevelInfo
        case "warn", "warning":
                lvl = slog.LevelWarn
        case "error":
                lvl = slog.LevelError
        default:
                lvl = slog.LevelInfo
        }

        opts := &slog.HandlerOptions{
                Level:     lvl,
                AddSource: !prod, // include source file:line in dev for debugging
        }

        var handler slog.Handler
        if prod {
                handler = slog.NewJSONHandler(os.Stdout, opts)
        } else {
                handler = slog.NewTextHandler(os.Stdout, opts)
        }
        return slog.New(handler)
}

// parseIntEnv reads an integer env var with a fallback default. Used for
// LOGIN_MAX_ATTEMPTS.
func parseIntEnv(key string, def int) int {
        v := os.Getenv(key)
        if v == "" {
                return def
        }
        var n int
        if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
                return def
        }
        if n <= 0 {
                return def
        }
        return n
}

// getenv returns the env var or the provided default when empty/missing.
func getenv(key, def string) string {
        if v := os.Getenv(key); v != "" {
                return v
        }
        return def
}

// parseDurationEnv reads a duration env var (Go duration syntax: "15m",
// "168h", "24h") with a fallback default. Used for SESSION_DURATION,
// ADMIN_SESSION_DURATION, LOGIN_LOCK_DURATION.
func parseDurationEnv(key string, def time.Duration) time.Duration {
        v := os.Getenv(key)
        if v == "" {
                return def
        }
        d, err := time.ParseDuration(v)
        if err != nil || d <= 0 {
                return def
        }
        return d
}
