# ADMIN-FRONTEND — Superadmin space for NOVA

**Agent**: Z.ai Code (fullstack)
**Task ID**: ADMIN-FRONTEND
**Date**: 2026-10-31
**Stack**: Next.js 16 + TypeScript + Tailwind 4 + shadcn/ui + Zustand

## Goal
Add a platform-admin (super_admin / admin) section to the NOVA dashboard
SPA, with 4 new views (Vue d'ensemble, Boutiques, Utilisateurs, Revenus)
wired to the existing Go backend endpoints on Render.

## Files modified / created

### Modified
1. **`src/stores/ui-store.ts`**
   - Added 4 new `ViewKey` entries: `admin-overview`, `admin-shops`,
     `admin-users`, `admin-revenue`.
   - Added FR labels (`viewLabels`) for the 4 new keys.
   - Exported a `ADMIN_VIEWS: ViewKey[]` helper.

2. **`src/lib/api.ts`**
   - Added an `adminApi` namespace with 5 methods that hit the
     Go backend's `/api/admin/*` and `/api/shops` endpoints:
       - `listSubscriptions({status, page, limit})`
       - `listLateSubscriptions()`
       - `getRevenue()`  → `RevenueStats`
       - `runCron()` (POST)
       - `listShops({search, status, page, limit})`
   - Added 3 exported types: `AdminSubscriptionItem`, `RevenueByPlan`,
     `RevenueStats` (aligned on `repository.SubscriptionListItem` and
     `repository.RevenueStats` from the Go code).

3. **`src/components/dashboard/sidebar.tsx`**
   - Imported `LayoutDashboard` and `Banknote` icons.
   - `NavList` now reads `useAuthStore` and only renders an
     "Administration" nav group when `user.role === "super_admin" ||
     user.role === "admin"`. The 4 items use the existing brand-green
     active style + the brand/amber/blue/rose tone classes already used
     elsewhere in NOVA.

4. **`src/components/dashboard/shell.tsx`**
   - Imported the 4 new admin view components.
   - Added 4 new branches to `renderView()`.

### Created
5. **`src/components/views/admin-overview-view.tsx`** — 4 KPI cards
   (boutiques actives/suspendues, MRR, revenu total), "Abonnements récents"
   table (last 6), "Abonnements en retard" alert with max-h-40 scroll,
   "Répartition par plan" badges list, "Actions rapides" card with a
   "Déclencher cron" button. Uses `Promise.all([getRevenue,
   listSubscriptions, listLateSubscriptions])` + skeleton loaders +
   sonner toasts.

6. **`src/components/views/admin-shops-view.tsx`** — Search bar + status
   filter (Select), table (name, slug, status, plan, created_at) merged
   from `adminApi.listShops()` + `adminApi.listSubscriptions()` (plan
   via `Map<shop_id, subscription>`). 4 mini-KPI cards by status. Status
   badges: active=green, suspended=red, draft=amber, pending=blue.
   Empty state with Store icon when no shops.

7. **`src/components/views/admin-users-view.tsx`** — Placeholder empty
   state with explanation that `GET /api/admin/users` isn't yet exposed
   by the Go backend. Shows the user's role badge + 3 feature-preview
   cards (Rôles & permissions, Comptes & accès, Audit & activité) so the
   page doesn't look abandoned.

8. **`src/components/views/admin-revenue-view.tsx`** — 4 KPI cards
   (MRR, revenu total, boutiques actives, suspendues), "Répartition par
   plan" table (plan_name, active_count, monthly_price, total_revenue)
   with a totals row, "Abonnements en retard" card with scrollable list
   + amber Alert banner, "Déclencher cron" button. Explanatory footer
   note clarifying the difference between MRR and total revenue.

## Design choices
- **No new API route**: the Go backend already exposes `/api/admin/*`
  and `/api/shops` (which returns the global list for admins). The
  existing `apiFetch` + cookie-based auth handle everything.
- **No TanStack Query**: followed the existing pattern (payment-config,
  products, stock views) which uses `useEffect` + `useState` + `apiFetch`
  directly. This keeps the admin views consistent with the rest of the
  codebase.
- **Branding**: reused NOVA's brand green (`bg-brand`, `text-brand`) and
  amber-accent for "warning" badges. No indigo/blue.
- **Loading states**: skeleton placeholders everywhere (cards, tables,
  lists). Errors surface as `sonner` toasts.
- **Responsive**: 2-col KPI grids on mobile, 4-col on lg. Tables wrapped
  in shadcn's `overflow-x-auto` Table container. Lists capped with
  `max-h-* overflow-y-auto scrollbar-thin`.
- **A11y**: every interactive element has `aria-label` where relevant;
  nav buttons use `aria-current="page"`. Select triggers have aria-labels.
- **FR language** everywhere (matches the rest of the app).

## Verification

| Check | Result |
| --- | --- |
| `bunx tsc --noEmit` | ✅ 0 errors in `src/` (only stale errors in `examples/`, `frontend_copy/`, `skills/`) |
| `bun run lint` | ✅ 0 errors, 0 warnings |
| `bun run dev` | ✅ Server compiles successfully (`✓ Compiled in 364ms / 405ms`), `GET /` returns 200 |

The initial "Module not found '@/components/views/admin-revenue-view'"
errors in `dev.log` were transient — they came from the dev server
hot-reloading `shell.tsx` before all 4 view files had been written.
Once the last file landed, the next two compile cycles were clean and
`GET /` returned 200.

## Backend contract verified
Confirmed the Go router (`mini-services/nova-api/internal/api/router.go`)
mounts the admin routes under `r.Route("/admin", ...)` with
`middleware.RequireRole("super_admin", "admin")`, and
`SubscriptionHandler.RegisterAdmin` registers:
- `GET   /api/admin/subscriptions`
- `GET   /api/admin/subscriptions/late`
- `GET   /api/admin/subscriptions/revenue`
- `POST  /api/admin/cron/run`

Plus `GET /api/shops` returns the global paginated list when the session
user is a platform admin (`services.IsPlatformAdmin(s.Role)` check in
`handlers.ShopHandler.List`). All 5 adminApi methods target real,
deployed endpoints — no proxy needed.

## Known limitations / follow-ups
- **Users view** is a placeholder pending the Go endpoint
  `GET /api/admin/users`. The page is intentional about this and offers
  a "Prévenez-moi quand c'est prêt" CTA that sets a localStorage flag.
- **No admin actions yet** (suspend/reactivate a shop from the UI). The
  backend endpoints `POST /api/shops/{id}/suspend|reactivate` exist but
  were out of scope for this task — added as a TODO for the next sprint.
- The admin-overview "Abonnements récents" table shows the latest 6
  items only; pagination could be added later.
