# Task 4 — Auth Module Integration Tests

This document describes the manual integration tests to run **once the
DATABASE_URL for Neon (Europe region) is available**. The code compiles and
all unit tests pass without a DB; the steps below exercise the full
auth flow against a real PostgreSQL instance.

## Prerequisites

1. `DATABASE_URL` env var set to the Neon connection string
   (e.g. `postgres://USER:PASS@HOST.neon.tech/DB?sslmode=require`).
2. `SESSION_SECRET` set to a 64-char random string
   (`openssl rand -hex 32`).
3. A TOTP authenticator app on your phone (Google Authenticator, Authy,
   FreeOTP, etc.) — needed for the 2FA tests.
4. `curl` + `jq` for HTTP requests.

## Setup

```bash
cd /home/z/my-project/mini-services/nova-api
export PATH="/home/z/.local/go/bin:$PATH"
export DATABASE_URL="postgres://..."        # Neon
export SESSION_SECRET="$(openssl rand -hex 32)"
export ENVIRONMENT=development               # dev so password-reset returns dev_token
GOTOOLCHAIN=local go run ./cmd/server &
SERVER_PID=$!
sleep 2
# Verify the server started and migrated the DB.
curl -s http://localhost:8080/health/ready | jq .
# Expect {"status":"ok","db":"ok",...}
```

## Test 1 — Register a new owner

```bash
curl -s -i -X POST http://localhost:8080/api/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"owner@test.ci","password":"Abcd1234","full_name":"Awa Test","phone":"+2250700000000"}'
# Expect: 201 + Set-Cookie: nova_session=... + body {id, email, role:"owner", ...}
```

Save the cookie from the `Set-Cookie` header for subsequent calls:
```bash
COOKIE=$(curl -s -i -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"owner@test.ci","password":"Abcd1234"}' \
  | grep -i '^set-cookie:' | sed 's/^[Ss]et-[Cc]ookie: //; s/;.*//')
echo "Cookie: $COOKIE"
```

## Test 2 — Login without 2FA

```bash
curl -s -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"owner@test.ci","password":"Abcd1234"}' | jq .
# Expect: {user:{...}, shops:[]}
```

Bad password → 401:
```bash
curl -s -w "%{http_code}\n" -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"owner@test.ci","password":"WRONG"}'
# Expect: 401 + {"error":"invalid_credentials",...}
```

5 bad passwords in a row → 423 Locked:
```bash
for i in 1 2 3 4 5 6; do
  curl -s -w " [%{http_code}]\n" -X POST http://localhost:8080/api/auth/login \
    -H "Content-Type: application/json" \
    -d '{"email":"owner@test.ci","password":"WRONG"}'
done
# Expect first 5: 401, 401, 401, 401, 423
# 6th: 423 (still locked)
```

## Test 3 — GET /api/auth/me

```bash
curl -s http://localhost:8080/api/auth/me -b "nova_session=$COOKIE" | jq .
# Expect: {user:{id, email, role:"owner", two_factor_enabled:false, ...}, shops:[]}
```

## Test 4 — 2FA setup

```bash
curl -s -X POST http://localhost:8080/api/auth/2fa/setup -b "nova_session=$COOKIE" | jq .
# Expect: {secret:"BASE32...", provisioning_uri:"otpauth://totp/...",
#          qr_data_uri:"data:image/png;base64,..."}

# Copy the `qr_data_uri` value, paste into a browser as <img src="...">,
# scan with your authenticator app, and grab the 6-digit code.
```

## Test 5 — 2FA confirm

```bash
CODE=123456   # replace with the real code from your authenticator app
curl -s -w "\n[%{http_code}]\n" -X POST http://localhost:8080/api/auth/2fa/confirm \
  -H "Content-Type: application/json" \
  -b "nova_session=$COOKIE" \
  -d "{\"code\":\"$CODE\"}"
# Expect: 200 {"ok":true}
```

## Test 6 — Login now requires 2FA

```bash
curl -s -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"owner@test.ci","password":"Abcd1234"}' | jq .
# Expect: {requires_two_factor:true, temp_token:"..."}

TEMP_TOKEN=$(curl -s -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"owner@test.ci","password":"Abcd1234"}' | jq -r .temp_token)
echo "Temp token: $TEMP_TOKEN"
```

## Test 7 — Verify 2FA, complete login

```bash
CODE=123456   # fresh code from authenticator
curl -s -X POST http://localhost:8080/api/auth/2fa/verify \
  -H "Content-Type: application/json" \
  -d "{\"temp_token\":\"$TEMP_TOKEN\",\"code\":\"$CODE\"}" | jq .
# Expect: {user:{...}, shops:[]} + Set-Cookie: nova_session=...
```

## Test 8 — Change password

```bash
curl -s -w "\n[%{http_code}]\n" -X POST http://localhost:8080/api/auth/change-password \
  -H "Content-Type: application/json" \
  -b "nova_session=$COOKIE" \
  -d '{"old_password":"Abcd1234","new_password":"Xyz98765"}'
# Expect: 200 {"ok":true}

# Old password no longer works:
curl -s -w "\n[%{http_code}]\n" -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"owner@test.ci","password":"Abcd1234"}'
# Expect: 401 (or 423 if locked from prior attempts)
```

## Test 9 — Password reset

```bash
# Request reset — returns dev_token in dev mode (no email infrastructure).
RESP=$(curl -s -X POST http://localhost:8080/api/auth/password-reset/request \
  -H "Content-Type: application/json" \
  -d '{"email":"owner@test.ci"}')
echo "$RESP" | jq .
RESET_TOKEN=$(echo "$RESP" | jq -r .dev_token)
echo "Reset token: $RESET_TOKEN"

# Confirm reset with a new password.
curl -s -w "\n[%{http_code}]\n" -X POST http://localhost:8080/api/auth/password-reset/confirm \
  -H "Content-Type: application/json" \
  -d "{\"token\":\"$RESET_TOKEN\",\"new_password\":\"Abcd1234\"}"
# Expect: 200 {"ok":true}

# Login with the new password.
curl -s -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"owner@test.ci","password":"Abcd1234"}' | jq .
```

## Test 10 — Logout

```bash
curl -s -X POST http://localhost:8080/api/auth/logout -b "nova_session=$COOKIE" | jq .
# Expect: 200 {"ok":true} + Set-Cookie: nova_session=; Max-Age=-1
```

## Test 11 — Unknown email enumeration check

```bash
# Reset request for an unknown email — should still return 200 (no enumeration).
curl -s -w "\n[%{http_code}]\n" -X POST http://localhost:8080/api/auth/password-reset/request \
  -H "Content-Type: application/json" \
  -d '{"email":"nobody@test.ci"}'
# Expect: 200 + {ok:true, message:"Si cette adresse email existe...", dev_token:"<uuid>"}
# (dev_token is a random UUID for unknown emails — useless to an attacker)
```

## Cleanup

```bash
kill $SERVER_PID
```

## Audit log verification (in DB)

After running the above tests, query the DB directly to verify every auth
event was recorded:

```sql
SELECT action, actor_role, ip_address, user_agent, created_at
  FROM audit_logs
 WHERE action LIKE 'auth.%'
 ORDER BY created_at DESC
 LIMIT 50;
```

You should see entries for: `auth.register`, `auth.login.success`,
`auth.login.failed`, `auth.login.locked`, `auth.2fa.setup`,
`auth.2fa.enable`, `auth.login.2fa_required`, `auth.login.2fa_success`,
`auth.password.change`, `auth.password_reset.request`,
`auth.password_reset.confirm`, `auth.logout`.
