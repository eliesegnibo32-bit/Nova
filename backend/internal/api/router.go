// Package api wires the router. The router is constructed once at startup
// and shared across all requests. It composes the global middleware stack
// (CORS, request ID, logger, recoverer, auth) with the route tree.
package api

import (
        "net/http"

        "github.com/go-chi/chi/v5"
        chiMiddleware "github.com/go-chi/chi/v5/middleware"
        "github.com/jackc/pgx/v5/pgxpool"
        "log/slog"

        "nova-api/internal/api/handlers"
        "nova-api/internal/api/middleware"
        "nova-api/internal/config"
        "nova-api/internal/services"
        "nova-api/internal/storage"
        "nova-api/internal/whatsapp"
)

// New constructs the root http.Handler from the config, DB pool, logger, and
// auth + shop + catalog + stock + delivery + order + AI + subscription + cron
// services. The handler is ready to pass to http.Server.
//
// If authSvc is nil (degraded mode without DB), auth routes are still
// mounted but each handler returns 503 service_unavailable. The same applies
// to shopSvc / catalogSvc / stockSvc / deliverySvc / orderSvc / aiSvc / subSvc
// / quotaSvc.
//
// NOVA v3 — added: productOptionH (product_options CRUD) +
// paymentCfgH (payment_configs GET/PUT).
func New(cfg *config.Config, pool *pgxpool.Pool, log *slog.Logger, authSvc *services.AuthService, shopSvc *services.ShopService, catalogSvc *services.CatalogService, stockSvc *services.StockService, deliverySvc *services.DeliveryService, orderSvc *services.OrderService, aiSvc *services.AIService, waProcessor *whatsapp.Processor, waClient *whatsapp.Client, waTemplateMgr *whatsapp.TemplateManager, waSvc *services.WhatsAppService, subSvc *services.SubscriptionService, quotaSvc *services.QuotaService, cronSvc *services.CronService, productOptionH *handlers.ProductOptionHandler, paymentCfgH *handlers.PaymentConfigHandler, r2Store *storage.R2Store) http.Handler {
        r := chi.NewRouter()

        // --- Global middleware (applied to every request) --------------------
        r.Use(middleware.RequestID)
        r.Use(middleware.CORS(cfg.CORSAllowedOrigins))
        r.Use(chiMiddleware.Recoverer) // convert panics to 500s
        r.Use(middleware.Logger(log))  // structured request logging
        r.Use(middleware.Auth([]byte(cfg.SessionSecret)))

        // --- Health & readiness (no auth required) ---------------------------
        r.Get("/health", handlers.Health())
        r.Get("/health/ready", handlers.HealthReady(pool))

        // --- WhatsApp webhook (NOT shop-scoped; Meta calls it with a phone) --
        // Full implementation (Task 9): verify handshake + signature + async
        // process. If the processor is nil (degraded mode), the handler still
        // verifies the GET handshake and ACKs the POSTs with 200 — Meta won't
        // retry, but no AI processing happens.
        webhookH := handlers.NewWhatsAppWebhookHandler(waProcessor, cfg.WhatsAppVerifyToken, cfg.WhatsAppAppSecret, log)
        webhookH.Register(r)

        // --- API v1 ---------------------------------------------------------
        r.Route("/api", func(r chi.Router) {
                // Auth routes — register / login / logout / me / 2FA / password
                // reset. The AuthHandler owns its own sub-router so the route
                // table lives next to the handler code; we mount it under /auth.
                authH := handlers.NewAuthHandler(
                        authSvc,
                        []byte(cfg.SessionSecret),
                        cfg.CookieSecure(),
                        cfg.IsDev(),
                )
                r.Mount("/auth", authH.Router())

                // Shop routes — creation, onboarding, switch, activation, team
                // management. The ShopHandler owns its own sub-router; we mount
                // it under /shops. All shop routes require an authenticated
                // session (RequireAuth is applied inside the handler's Router).
                // Role checks happen per-route inside the service layer.
                shopH := handlers.NewShopHandler(
                        shopSvc,
                        []byte(cfg.SessionSecret),
                        cfg.CookieSecure(),
                        cfg.IsDev(),
                )
                r.Mount("/shops", shopH.Router())

                // Catalog + stock + delivery + orders + AI + subscriptions + quotas
                // routes — shop-scoped endpoints mounted under
                // /api/shops/{shopId}/.... We use a Route group (rather than three
                // separate Mount() calls) because chi forbids mounting multiple
                // sub-routers at the same path.
                // Each handler exposes a Register() method that adds its
                // routes to the shared group router.
                //
                // The {shopId} parameter is constrained to UUID characters
                // (`[0-9a-f-]+`) so it doesn't shadow the shopH.Router()'s
                // static routes like `/api/shops/switch` (chi would otherwise
                // match "switch" against the parameter route and apply the
                // ShopContext middleware, blocking the request before
                // shopH.Router() gets a chance to handle it).
                //
                // The shop-scoped ShopHandler routes (Get, Update, Activate,
                // Suspend, Reactivate, Validation, Subscription, Payment)
                // are ALSO registered here so they share the same {shopId}
                // parameter — registering them as shopH.Router()'s /{id}/*
                // would conflict with the regex-constrained {shopId} group
                // (chi prefers the more-specific regex route, so the {id}
                // routes would never be reached).
                catalogH := handlers.NewCatalogHandler(catalogSvc)
                stockH := handlers.NewStockHandler(stockSvc)
                deliveryH := handlers.NewDeliveryHandler(deliverySvc)
                orderH := handlers.NewOrderHandler(orderSvc)
                aiH := handlers.NewAIHandler(aiSvc)
                waAdminH := handlers.NewWhatsAppAdminHandler(waClient, waTemplateMgr, aiSvc, shopSvc, waSvc)
                subH := handlers.NewSubscriptionHandler(subSvc, quotaSvc, cronSvc)

                r.Route("/shops/{shopId:[0-9a-f-]+}", func(r chi.Router) {
                        r.Use(middleware.RequireAuth, middleware.ShopContext)
                        // Shop-handler scoped routes (/, /activate, /suspend,
                        // /reactivate, /validation, /subscription,
                        // /subscription/payment).
                        shopH.RegisterShopScopedRoutes(r)
                        // Catalog / stock / delivery / orders / AI / WhatsApp admin /
                        // subscription / quota / usage routes.
                        catalogH.Register(r)
                        stockH.Register(r)
                        deliveryH.Register(r)
                        orderH.Register(r)
                        aiH.Register(r)
                        waAdminH.Register(r)
                        subH.Register(r)
                        // NOVA v3 — product options + payment config.
                        if productOptionH != nil {
                                productOptionH.Register(r)
                        }
                        if paymentCfgH != nil {
                                paymentCfgH.Register(r)
                        }
                        // Media upload (Cloudflare R2)
                        if r2Store != nil {
                                uploadH := handlers.NewUploadHandler(r2Store)
                                uploadH.Register(r)
                        }
                })

                // Platform-admin routes (super_admin / admin only). Stubs return
                // a placeholder JSON until handlers are implemented in a future task.
                r.Route("/admin", func(r chi.Router) {
                        r.Use(middleware.RequireAuth)
                        r.Use(middleware.RequireRole("super_admin", "admin"))
                        r.Get("/", func(w http.ResponseWriter, r *http.Request) {
                                w.Header().Set("Content-Type", "application/json; charset=utf-8")
                                w.WriteHeader(http.StatusOK)
                                _, _ = w.Write([]byte(`{"admin":true,"message":"NOVA admin area — endpoints coming in a future task."}`))
                        })
                        // Subscription admin routes (Task 10) — list, late, revenue, cron.
                        subH.RegisterAdmin(r)
                })
        })

        // Catch-all 404 for unknown routes (chi returns 404 by default but we
        // want a JSON body consistent with our error envelope).
        r.NotFound(func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "application/json; charset=utf-8")
                w.WriteHeader(http.StatusNotFound)
                _, _ = w.Write([]byte(`{"error":"not_found","message":"The requested resource does not exist."}`))
        })

        r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "application/json; charset=utf-8")
                w.WriteHeader(http.StatusMethodNotAllowed)
                _, _ = w.Write([]byte(`{"error":"method_not_allowed","message":"This HTTP method is not allowed on this resource."}`))
        })

        return r
}
