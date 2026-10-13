// Shop context middleware.
//
// The cahier des charges isolates tenant data via PostgreSQL RLS, which is
// enforced by setting `app.current_shop_id` / `app.user_id` / `app.user_role`
// at the start of each transaction (see internal/db/queries.go). The
// transaction itself is begun by the handler (or a service-layer helper)
// because the right isolation level and commit/rollback boundary are
// business-logic decisions.
//
// This middleware does the lightweight part:
//   - Ensures the session has an active shop (equivalent to RequireShop but
//     returns a slightly different error code so the frontend can prompt
//     the user to pick a shop).
//   - Exposes the active shop_id via context so handlers don't have to
//     re-fetch it from the session every time.
//
// Handlers that need DB access call db.WithTenantTx(ctx, pool, shopID,
// userID, role, fn) to get a properly-scoped transaction.
package middleware

import (
        "context"
        "net/http"

        "github.com/google/uuid"
)

const keyActiveShopID ctxKey = iota + 100 // avoid collision with keyRequestID/keySession

// ShopContext enforces that an active shop is selected and stores its UUID
// in the request context. Use it on shop-scoped route groups (e.g.
// /api/shops/{id}/...).
func ShopContext(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                s, ok := SessionFromContext(r.Context())
                if !ok || s == nil {
                        writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required.")
                        return
                }
                if s.ShopID == nil {
                        writeError(w, http.StatusForbidden, "no_active_shop", "Select a shop before performing this action.")
                        return
                }
                ctx := context.WithValue(r.Context(), keyActiveShopID, *s.ShopID)
                next.ServeHTTP(w, r.WithContext(ctx))
        })
}

// ActiveShopIDFromContext returns the active shop UUID set by ShopContext,
// or the zero UUID if unset.
func ActiveShopIDFromContext(ctx context.Context) uuid.UUID {
        v, ok := ctx.Value(keyActiveShopID).(uuid.UUID)
        if !ok {
                return uuid.Nil
        }
        return v
}
