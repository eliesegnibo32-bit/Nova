# Shop endpoints — testing guide

This document describes the manual curl tests for the shop endpoints
introduced in Task 5. The shop module is the core multi-tenant feature of
NOVA: shop creation, onboarding, switch, activation, suspend/reactivate,
subscription payment recording.

## Pre-requisites

1. A working PostgreSQL connection (Neon or local). Set `DATABASE_URL` in
   `mini-services/nova-api/.env`.
2. The server running: `cd mini-services/nova-api && ./start.sh`.
3. An admin user. The register endpoint creates `owner`-role users by
   default; to test admin-only endpoints you must promote a user:

   ```bash
   cd mini-services/nova-api
   export DATABASE_URL="postgresql://..."  # same as .env
   go run ./cmd/dev-admin -email alice@example.ci
   # → ✅ User alice@example.ci (id=...) is now role=super_admin
   ```

   The `dev-admin` binary is dev-only (not for production). It connects
   directly to PG and runs a single UPDATE on `users.role`.

## Test scenarios

The tests below assume you have:
- An admin user `admin@nova-test.ci` (password `Admin1234!`) promoted via
  `dev-admin`.
- A merchant user `owner@nova-test.ci` (password `Owner1234!`) registered
  via `/api/auth/register` (NOT promoted — they keep the `owner` role).

### 1. Admin creates a shop on behalf of a merchant

```bash
# Login as admin → get a session cookie.
curl -i -c /tmp/cookies-admin.txt -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@nova-test.ci","password":"Admin1234!"}'

# Create a shop — admin specifies the owner by email.
curl -i -b /tmp/cookies-admin.txt -X POST http://localhost:8080/api/shops \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Boutique Test CI",
    "slug": "boutique-test-ci",
    "owner_email": "owner@nova-test.ci",
    "phone": "+2250700000001",
    "commune": "Cocody",
    "categories": ["Mode", "Accessoires"],
    "accepted_payment_modes": ["cash", "orange_money", "mtn_momo"]
  }'
# Expected: 201 Created
# Body: { "shop": {...status:"draft"...}, "subscription": {...status:"trial"...} }
```

Save the returned shop ID — we'll use it in subsequent calls. Let's call
it `$SHOP_ID`.

### 2. Owner logs in and sees the new shop

```bash
curl -i -c /tmp/cookies-owner.txt -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"owner@nova-test.ci","password":"Owner1234!"}'

# GET /api/auth/me should list the shop in `shops`.
curl -b /tmp/cookies-owner.txt http://localhost:8080/api/auth/me

# GET /api/shops (owner view → only their shops)
curl -b /tmp/cookies-owner.txt http://localhost:8080/api/shops
```

### 3. Owner switches to the new shop

```bash
curl -i -b /tmp/cookies-owner.txt -X POST http://localhost:8080/api/shops/switch \
  -H "Content-Type: application/json" \
  -d "{\"shop_id\":\"$SHOP_ID\"}"
# Expected: 200 OK + Set-Cookie: nova_session=... (with shop_id set)
# Body: { "ok": true, "shop_id": "...", "shop_name": "Boutique Test CI",
#         "shop_status": "draft", "role_in_shop": "owner" }
```

The new cookie now carries `shop_id` — all subsequent shop-scoped requests
will operate on this shop.

### 4. Owner tries to activate → fails (no products, no zones)

```bash
curl -i -b /tmp/cookies-owner.txt -X POST http://localhost:8080/api/shops/$SHOP_ID/activate
# Expected: 422 Unprocessable Entity
# Body: {
#   "error": "activation_criteria_not_met",
#   "message": "L'activation nécessite au moins un produit publié...",
#   "validation": {
#     "can_activate": false,
#     "missing_criteria": ["published_products", "delivery_zones", "hours"],
#     "published_products": 0,
#     "active_delivery_zones": 0,
#     "hours_set": false
#   },
#   "missing_criteria": ["published_products", "delivery_zones", "hours"]
# }

# Owner can also GET the validation state directly:
curl -b /tmp/cookies-owner.txt http://localhost:8080/api/shops/$SHOP_ID/validation
```

### 5. Owner updates the shop (adds hours)

```bash
curl -i -b /tmp/cookies-owner.txt -X PATCH http://localhost:8080/api/shops/$SHOP_ID \
  -H "Content-Type: application/json" \
  -d '{
    "hours": {"mon":["08:00","18:00"],"tue":["08:00","18:00"],"wed":["08:00","18:00"],"thu":["08:00","18:00"],"fri":["08:00","18:00"],"sat":["09:00","17:00"]},
    "description": "Boutique de test pour NOVA"
  }'
# Expected: 200 OK with the updated shop.
```

### 6. Admin lists all shops

```bash
curl -b /tmp/cookies-admin.txt "http://localhost:8080/api/shops?page=1&limit=20"
# Expected: 200 OK
# Body: { "shops": [...], "total": N, "page": 1, "limit": 20 }

# With filters:
curl -b /tmp/cookies-admin.txt "http://localhost:8080/api/shops?status=draft&search=test"
```

### 7. Admin records a subscription payment

```bash
# Get the subscription ID first.
curl -b /tmp/cookies-admin.txt http://localhost:8080/api/shops/$SHOP_ID/subscription
# → { "id": "...", "status": "trial", ... }

curl -i -b /tmp/cookies-admin.txt -X POST \
  http://localhost:8080/api/shops/$SHOP_ID/subscription/payment \
  -H "Content-Type: application/json" \
  -d '{
    "amount": 10000,
    "mode": "orange_money",
    "reference": "OM-REF-001",
    "period_start": "2026-10-01",
    "period_end": "2026-10-31"
  }'
# Expected: 200 OK { ok: true, payment_id: "...", recorded_at: "..." }

# Verify the subscription is now 'active' with next_billing_at = +1 month.
curl -b /tmp/cookies-admin.txt http://localhost:8080/api/shops/$SHOP_ID/subscription
```

### 8. Admin suspends the shop

```bash
curl -i -b /tmp/cookies-admin.txt -X POST \
  http://localhost:8080/api/shops/$SHOP_ID/suspend \
  -H "Content-Type: application/json" \
  -d '{"reason":"Test de suspension"}'
# Expected: 200 OK with shop.status='suspended'

# Owner trying to switch to the suspended shop gets 403.
curl -i -b /tmp/cookies-owner.txt -X POST http://localhost:8080/api/shops/switch \
  -H "Content-Type: application/json" \
  -d "{\"shop_id\":\"$SHOP_ID\"}"
# Expected: 403 Forbidden { error: "shop_suspended" }
```

### 9. Admin reactivates the shop

```bash
curl -i -b /tmp/cookies-admin.txt -X POST \
  http://localhost:8080/api/shops/$SHOP_ID/reactivate
# Expected: 200 OK with shop.status='active'

# Owner can now switch to it again.
curl -i -b /tmp/cookies-owner.txt -X POST http://localhost:8080/api/shops/switch \
  -H "Content-Type: application/json" \
  -d "{\"shop_id\":\"$SHOP_ID\"}"
# Expected: 200 OK + new Set-Cookie
```

### 10. Non-member cannot access the shop

Register a second merchant and try to access the shop:

```bash
curl -i -X POST http://localhost:8080/api/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"other@nova-test.ci","password":"Other1234!","full_name":"Other User"}'

# Login as other user.
curl -i -c /tmp/cookies-other.txt -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"other@nova-test.ci","password":"Other1234!"}'

# Try to GET the shop.
curl -i -b /tmp/cookies-other.txt http://localhost:8080/api/shops/$SHOP_ID
# Expected: 403 Forbidden { error: "not_shop_member" }

# Try to switch to the shop.
curl -i -b /tmp/cookies-other.txt -X POST http://localhost:8080/api/shops/switch \
  -H "Content-Type: application/json" \
  -d "{\"shop_id\":\"$SHOP_ID\"}"
# Expected: 403 Forbidden { error: "not_shop_member" }
```

### 11. Non-admin cannot create a shop

```bash
curl -i -b /tmp/cookies-owner.txt -X POST http://localhost:8080/api/shops \
  -H "Content-Type: application/json" \
  -d '{"name":"Test","slug":"test","owner_email":"owner@nova-test.ci"}'
# Expected: 403 Forbidden { error: "forbidden",
#           message: "Cette action nécessite les droits administrateur." }
```

## Audit log inspection

Every shop action is logged in `audit_logs` with `object_type='shop'`
(or `'subscription_payment'` for payments). To inspect:

```sql
-- Connect to the DB (psql or a SQL client).
SELECT id, action, actor_role, object_type, object_id, ip_address, created_at
  FROM audit_logs
 WHERE object_type = 'shop'
 ORDER BY created_at DESC
 LIMIT 20;
```

Expected actions: `shop.create`, `shop.update`, `shop.activate`,
`shop.suspend`, `shop.reactivate`, `shop.switch`, `subscription.payment`.
