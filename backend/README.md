# NOVA API

Go backend of NOVA — the WhatsApp AI assistant SaaS for small businesses
in Côte d'Ivoire. This package contains the HTTP API server (chi router,
pgx database layer, goose migrations, signed session cookies).

> Stack (cahier des charges ch. 9.1) : Go 1.23 · chi v5 · pgx v5 ·
> goose v3 · bcrypt · slog · UUID · signed-cookie sessions.

## Quick start

```bash
# 1. Copy the example env file and fill in the values.
cp .env.example .env
# Edit .env — at minimum set DATABASE_URL and (in production) SESSION_SECRET.

# 2. Run (migrations apply automatically on startup).
make run
```

The server listens on `http://localhost:8080` by default. The first
request that touches the database triggers goose migrations
`001`→`012` (the seed `012` creates a demo shop, an owner user, a few
products and delivery zones).

## Requirements

- Go 1.23+
- PostgreSQL 13+ (15+ recommended for `GENERATED ... STORED`)
- The `goose` CLI **only** if you want to run migrations manually —
  the server runs them automatically at startup.
  ```bash
  go install github.com/pressly/goose/v3/cmd/goose@latest
  ```

## Project layout

```
nova-api/
├── cmd/server/main.go            # entry point: config → logger → DB → migrate → serve
├── internal/
│   ├── config/                   # env-based config
│   ├── db/                       # pgxpool + goose + RLS tenant helpers
│   ├── models/                   # plain structs mirroring the PG schema
│   ├── auth/                     # HMAC-signed session cookies + bcrypt
│   ├── api/
│   │   ├── router.go             # chi router + middleware wiring
│   │   ├── handlers/             # HTTP handlers (health, auth, shops, …)
│   │   └── middleware/           # cors, request_id, logger, auth, shop, audit
│   └── services/                 # (reserved for Task 4 service layer)
├── migrations/                   # goose SQL migrations (Task 2 — 25 files)
├── queries/                      # (reserved for sqlc / .sql files)
├── migrations_embed.go           # //go:embed migrations/*.sql for goose
├── .env.example
├── Makefile
└── go.mod
```

## Configuration

All configuration is via environment variables (no flags, no config files).
See `.env.example` for the full list. Highlights:

| Variable                 | Default                       | Notes                                   |
|--------------------------|-------------------------------|-----------------------------------------|
| `PORT`                   | `8080`                        | HTTP listen port.                       |
| `DATABASE_URL`           | _(empty)_                     | libpq/Neon connection string.           |
| `SESSION_SECRET`         | _(dev fallback)_              | HMAC key for cookies. ≥32B in prod.     |
| `ENVIRONMENT`            | `development`                 | `production` enables Secure cookies.    |
| `LOG_LEVEL`              | `info`                        | `debug`/`info`/`warn`/`error`.          |
| `CORS_ALLOWED_ORIGINS`   | `http://localhost:3000`       | Comma-separated list.                   |

## API endpoints

| Method | Path                                     | Auth               | Status       |
|--------|------------------------------------------|--------------------|--------------|
| GET    | `/health`                                | none               | Live         |
| GET    | `/health/ready`                          | none               | Ready        |
| POST   | `/api/auth/register`                     | none               | Live (Task 4)|
| POST   | `/api/auth/login`                        | none               | Live (Task 4)|
| POST   | `/api/auth/logout`                       | none               | Live         |
| GET    | `/api/auth/me`                           | session            | Live         |
| POST   | `/api/auth/change-password`              | session            | Live (Task 4)|
| POST   | `/api/auth/2fa/setup`                    | session            | Live (Task 4)|
| POST   | `/api/auth/2fa/confirm`                  | session            | Live (Task 4)|
| POST   | `/api/auth/2fa/disable`                  | session            | Live (Task 4)|
| POST   | `/api/auth/2fa/verify`                   | none               | Live (Task 4)|
| POST   | `/api/auth/password-reset/request`       | none               | Live (Task 4)|
| POST   | `/api/auth/password-reset/confirm`       | none               | Live (Task 4)|
| GET    | `/api/shops`                             | session            | Live (Task 5)|
| POST   | `/api/shops`                             | session + admin    | Live (Task 5)|
| POST   | `/api/shops/switch`                      | session            | Live (Task 5)|
| GET    | `/api/shops/{id}`                        | session + member   | Live (Task 5)|
| PATCH  | `/api/shops/{id}`                        | session + owner    | Live (Task 5)|
| POST   | `/api/shops/{id}/activate`               | session + owner    | Live (Task 5)|
| POST   | `/api/shops/{id}/suspend`                | session + admin    | Live (Task 5)|
| POST   | `/api/shops/{id}/reactivate`             | session + admin    | Live (Task 5)|
| GET    | `/api/shops/{id}/validation`             | session + member   | Live (Task 5)|
| GET    | `/api/shops/{id}/subscription`           | session + member   | Live (Task 5)|
| POST   | `/api/shops/{id}/subscription/payment`   | session + admin    | Live (Task 5)|
| GET    | `/api/admin/*`                           | super_admin/admin  | Stub         |

Auth endpoints (Task 4) and shop endpoints (Task 5) require a working
PostgreSQL connection — in degraded mode (no `DATABASE_URL` or unreachable
DB) they return `503 service_unavailable`.

### Shop endpoints (Task 5) — permission model

| Endpoint                                | Required role                                |
|-----------------------------------------|----------------------------------------------|
| `POST /api/shops`                       | platform admin (super_admin / admin)         |
| `GET /api/shops`                        | any authenticated user (admin sees all;      |
|                                         | owners/employees see their shops)            |
| `GET /api/shops/{id}`                   | member of the shop OR platform admin         |
| `PATCH /api/shops/{id}`                 | owner of the shop OR platform admin          |
| `POST /api/shops/{id}/activate`         | owner of the shop OR platform admin          |
| `POST /api/shops/{id}/suspend`          | platform admin                               |
| `POST /api/shops/{id}/reactivate`       | platform admin                               |
| `GET /api/shops/{id}/validation`        | member of the shop OR platform admin         |
| `POST /api/shops/switch`                | any authenticated user (must be a member     |
|                                         | of the target shop, or a platform admin)     |
| `GET /api/shops/{id}/subscription`      | member of the shop OR platform admin         |
| `POST /api/shops/{id}/subscription/payment` | platform admin                           |

### Development helpers

Two dev-only binaries are provided to help test admin-only endpoints
without a separate admin console:

```bash
# Promote a user to platform admin (run once for testing):
DATABASE_URL=... go run ./cmd/dev-admin -email alice@example.ci

# Mint a signed session cookie for manual curl testing:
SESSION_SECRET=... go run ./cmd/mint-cookie -role super_admin
```

## Design decisions

### Session strategy — signed cookies, not JWTs

Sessions are JSON payloads `{user_id, shop_id, role, exp}` signed with
HMAC-SHA256 (`SESSION_SECRET`), base64url-encoded, stored in an
`HttpOnly; SameSite=Lax` cookie named `nova_session` (in production we add
`Secure`). Why not JWT:

- Smaller payload, no `alg=none` / `kid` abuse surface.
- We can revoke by rotating the server secret (rare) or — planned Task 8 —
  by storing a per-session nonce in `audit_logs` and rejecting unknowns.
- One cookie verify per request, no DB lookup on the hot path. `/api/auth/me`
  re-fetches the user from PG for freshness.

### RLS integration

Every shop-scoped DB write goes through `db.WithTenantTx` which begins a
transaction and runs:

```sql
SET LOCAL app.current_shop_id = $1;
SET LOCAL app.user_id         = $2;
SET LOCAL app.user_role       = $3;
```

`SET LOCAL` scopes the value to the transaction, so there is zero leakage
between requests even when pgxpool reuses a connection. The RLS policies
(migration 011) enforce `shop_id = current_shop_id() OR is_platform_admin()`.

### Error handling

- Handlers return a canonical JSON envelope: `{"error":"code","message":"…"}`.
- All errors flow through `%w` so callers can `errors.Is`.
- `chi/middleware.Recoverer` converts any panic in a handler into a `500`
  with no stack leak to the client.
- The request logger emits a structured slog record per request with
  `request_id`, `method`, `path`, `status`, `duration_ms`. Health-check
  routes are logged at `debug` to keep production logs readable.

### Graceful shutdown

On `SIGINT`/`SIGTERM` the server stops accepting new connections, gives
in-flight handlers up to 30 seconds to complete, then closes the DB pool.
If a handler is still running after 30s the process exits non-zero.

### Degraded mode (development only)

If `DATABASE_URL` is empty or the DB is unreachable in `development`, the
server still boots: `/health` returns 200, `/health/ready` returns 503,
and all DB-dependent endpoints return 503. This makes
`docker compose up`-style boot ordering friendlier. In `production` an
unreachable DB at startup is fatal.

## Migrations

Migrations live in `migrations/` (25 `.sql` files from Task 2). They are
embedded into the binary via `//go:embed migrations/*.sql` in
`migrations_embed.go` and applied automatically on startup by
`db.RunMigrations`. To run them manually:

```bash
make migrate-up        # apply all pending
make migrate-down      # roll back the last one
make migrate-status    # see current state
```

See `migrations/README.md` for the full schema and RLS documentation.

## Development

```bash
make fmt       # go fmt
make vet       # go vet
make test      # go test ./...
make tidy      # go mod tidy
make build     # produces ./bin/nova-api
```

## What's next

- **Task 5** — Shop module ✅ (creation, onboarding, switch, activation,
  suspend/reactivate, team management, subscription payment recording).
- **Task 6** — Catalogue + stock (products, variants, photos, import,
  atomic stock movements).
- **Task 7** — AI engine (tool-calling, guard rails, cost tracking).
- **Task 8** — WhatsApp Cloud API webhook + outbound messaging.
