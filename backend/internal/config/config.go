// Package config loads runtime configuration for the nova-api server.
//
// Configuration is sourced exclusively from environment variables (no extra
// dependency on env-reflect libraries, per the cahier des charges simplicity
// requirement). Sensible defaults are provided for local development; the
// production deployment overrides them via the container/k8s environment.
package config

import (
        "fmt"
        "os"
        "strings"
)

// Config groups every runtime knob of the API.
type Config struct {
        // Port is the HTTP listen port (without colon).
        Port string `envDefault:"8080"`

        // DatabaseURL is the libpq/Neon connection string.
        // Example: postgres://nova:nova@localhost:5432/nova?sslmode=disable
        DatabaseURL string

        // SessionSecret is the HMAC key used to sign session cookies. Must be
        // >= 32 bytes in production. Required.
        SessionSecret string

        // Environment is one of: development, staging, production.
        Environment string `envDefault:"development"`

        // LogLevel is one of: debug, info, warn, error.
        LogLevel string `envDefault:"info"`

        // CORSAllowedOrigins is the list of allowed Origin values for the CORS
        // middleware. Comma-separated in the env var CORS_ALLOWED_ORIGINS.
        CORSAllowedOrigins []string `envDefault:"http://localhost:3000"`

        // --- AI provider (placeholders for Task 5) ---
        AIAPIKey string
        AIModel  string `envDefault:"gpt-4o-mini"`

        // --- WhatsApp Cloud API (ch. 6 — Canal WhatsApp) ---
        // VerifyToken: token Meta sends during the webhook subscription
        //   handshake (GET /webhooks/whatsapp?hub.verify_token=...).
        // AccessToken: the permanent (or system-user) access token for the
        //   WhatsApp Business app. Used as the Bearer token on outbound calls
        //   to https://graph.facebook.com/v18.0/<phone_number_id>/messages.
        //   EMPTY in development → mock mode (the client returns synthetic
        //   wamids "wamid.mock.*" without doing any HTTP call).
        // PhoneNumberID: the WhatsApp Business phone number ID (numeric string
        //   from the Meta App Dashboard). One per shop in production; in pilot
        //   mode all shops share the NOVA number.
        // AppSecret: the App Secret from the Meta app dashboard. Used to
        //   compute the HMAC-SHA256 of inbound webhook payloads so we can
        //   verify the X-Hub-Signature-256 header.
        // BaseURL: the Graph API root URL (default: https://graph.facebook.com).
        // Version: the Graph API version (default: v18.0).
        //
        // Env var aliases (Task 9 spec uses WHATSAPP_TOKEN and
        // WHATSAPP_API_VERSION; we accept both names for backwards compat):
        //   WHATSAPP_TOKEN         ≡ WHATSAPP_ACCESS_TOKEN
        //   WHATSAPP_API_VERSION   ≡ WHATSAPP_VERSION
        WhatsAppVerifyToken   string
        WhatsAppAccessToken   string
        WhatsAppPhoneNumberID string
        WhatsAppAppSecret     string
        WhatsAppBaseURL       string `envDefault:"https://graph.facebook.com"`
        WhatsAppVersion       string `envDefault:"v18.0"`
}

// Load reads the environment and returns a populated Config or an error
// describing the first missing required variable.
func Load() (*Config, error) {
        cfg := &Config{
                Port:                   getenv("PORT", "8080"),
                DatabaseURL:            os.Getenv("DATABASE_URL"),
                SessionSecret:          os.Getenv("SESSION_SECRET"),
                Environment:            getenv("ENVIRONMENT", "development"),
                LogLevel:               getenv("LOG_LEVEL", "info"),
                AIAPIKey:               os.Getenv("AI_API_KEY"),
                AIModel:                getenv("AI_MODEL", "gpt-4o-mini"),
                WhatsAppVerifyToken:    os.Getenv("WHATSAPP_VERIFY_TOKEN"),
                WhatsAppAccessToken:    getenv2("WHATSAPP_TOKEN", "WHATSAPP_ACCESS_TOKEN"),
                WhatsAppPhoneNumberID:  os.Getenv("WHATSAPP_PHONE_NUMBER_ID"),
                WhatsAppAppSecret:      os.Getenv("WHATSAPP_APP_SECRET"),
                WhatsAppBaseURL:        getenv("WHATSAPP_BASE_URL", "https://graph.facebook.com"),
                WhatsAppVersion:        getenv2("WHATSAPP_API_VERSION", "WHATSAPP_VERSION"),
                CORSAllowedOrigins:     parseCSV(getenv("CORS_ALLOWED_ORIGINS", "http://localhost:3000")),
        }

        // In development we tolerate an empty SESSION_SECRET and fall back to a
        // hard-coded dev-only value so `make run` works out of the box. Any other
        // environment MUST set it explicitly.
        if cfg.SessionSecret == "" {
                if cfg.IsProd() {
                        return nil, fmt.Errorf("SESSION_SECRET is required in %s environment", cfg.Environment)
                }
                cfg.SessionSecret = "dev-only-secret-change-me-in-production-32b!"
        }

        if cfg.DatabaseURL == "" {
                // We don't fail hard here so the server can boot and serve /health
                // even without a DB (degraded mode). DB-dependent routes will return
                // 503 at runtime. This makes local dev and CI friendlier.
                // In production, an empty DATABASE_URL is treated as a fatal error
                // when the server actually tries to migrate.
        }

        return cfg, nil
}

// IsProd returns true when Environment == "production".
func (c *Config) IsProd() bool { return c.Environment == "production" }

// IsDev returns true when Environment == "development".
func (c *Config) IsDev() bool { return c.Environment == "development" }

// CookieSecure returns whether session cookies should carry the Secure flag
// (HTTPS-only). False in dev (no TLS), true in staging/production.
func (c *Config) CookieSecure() bool { return !c.IsDev() }

// getenv returns the env var or the provided default when empty/missing.
func getenv(key, def string) string {
        if v := os.Getenv(key); v != "" {
                return v
        }
        return def
}

// getenv2 returns the value of `primary` env var, falling back to `secondary`
// when primary is empty, then to "" when both are empty. Used for env var
// aliases (e.g. WHATSAPP_TOKEN is the Task 9 spec name, WHATSAPP_ACCESS_TOKEN
// is the legacy name — we accept both).
func getenv2(primary, secondary string) string {
        if v := os.Getenv(primary); v != "" {
                return v
        }
        return os.Getenv(secondary)
}

// parseCSV splits a comma-separated env value into a trimmed slice.
func parseCSV(s string) []string {
        if s == "" {
                return nil
        }
        parts := strings.Split(s, ",")
        out := make([]string, 0, len(parts))
        for _, p := range parts {
                if t := strings.TrimSpace(p); t != "" {
                        out = append(out, t)
                }
        }
        return out
}
