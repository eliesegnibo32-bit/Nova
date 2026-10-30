// AI engine configuration (cahier des charges ch. 5.6 — Performance et
// optimisation des coûts).
//
// All knobs are sourced from environment variables with sensible defaults so
// the engine works out-of-the-box in development (MockProvider, no API key)
// and is fully configurable in production (OpenAI-compatible provider,
// configurable debounce, max history, retries).
//
// The configuration is consumed by cmd/server/main.go to build the
// ai.EngineConfig + select the provider. It is intentionally a plain struct
// (no envconfig library) so the dependency footprint stays minimal.
package ai

import (
        "fmt"
        "os"
        "strconv"
        "strings"
)

// AIConfig holds every runtime knob of the AI engine.
type AIConfig struct {
        // APIKey is the OpenAI-compatible API key. When empty, the engine falls
        // back to MockProvider (ch. 5.6 — mode test).
        APIKey string
        // BaseURL is the OpenAI-compatible endpoint base (no trailing slash).
        // Examples: https://api.openai.com/v1, https://api.z.ai/api/paas/v4.
        BaseURL string
        // Model is the model name passed to the provider (and stored in ai_usage).
        Model string
        // Temperature is the LLM sampling temperature (0.0–1.0). Low values (0.3)
        // make responses more deterministic, which is desirable for an assistant
        // that must cite exact prices.
        Temperature float32
        // MaxTokens caps the response length (cahier des charges recommends short
        // replies: 1–3 phrases).
        MaxTokens int
        // MaxRetries is the max number of guardrail-regeneration attempts before
        // escalating to a human (ch. 5.5). Default 2 → 1 initial + 2 retries.
        MaxRetries int
        // DebounceMs is the silence window for grouping rapid-fire messages from
        // the same conversation (ch. 5.6). Default 4000 ms.
        DebounceMs int
        // MaxHistoryMessages is the max number of prior messages included in the
        // LLM context (ch. 5.6 — context bounding). Older messages are summarized
        // via conversation.summary. Default 20.
        MaxHistoryMessages int
}

// DefaultAIConfig returns the canonical defaults (matches the cahier des
// charges ch. 5.6 — Temperature 0.3, MaxTokens 500, Debounce 4 s, MaxHistory 20).
func DefaultAIConfig() AIConfig {
        return AIConfig{
                APIKey:             "",
                BaseURL:            "https://api.openai.com/v1",
                Model:              "gpt-4o-mini",
                Temperature:        0.3,
                MaxTokens:          500,
                MaxRetries:         2,
                DebounceMs:         4000,
                MaxHistoryMessages: 20,
        }
}

// LoadAIConfig reads AI_* environment variables and returns an AIConfig.
// Missing or malformed values fall back to the defaults from DefaultAIConfig.
//
// Environment variables:
//   - AI_API_KEY       (default "")
//   - AI_BASE_URL      (default "https://api.openai.com/v1")
//   - AI_MODEL         (default "gpt-4o-mini")
//   - AI_TEMPERATURE   (default 0.3)
//   - AI_MAX_TOKENS    (default 500)
//   - AI_MAX_RETRIES   (default 2)
//   - AI_DEBOUNCE_MS   (default 4000)
//   - AI_MAX_HISTORY   (default 20)
func LoadAIConfig() AIConfig {
        cfg := DefaultAIConfig()
        if v := os.Getenv("AI_API_KEY"); v != "" {
                cfg.APIKey = v
        }
        if v := os.Getenv("AI_BASE_URL"); v != "" {
                cfg.BaseURL = v
        }
        if v := os.Getenv("AI_MODEL"); v != "" {
                cfg.Model = v
        }
        if v := os.Getenv("AI_TEMPERATURE"); v != "" {
                if f, err := strconv.ParseFloat(v, 32); err == nil && f >= 0 && f <= 2 {
                        cfg.Temperature = float32(f)
                }
        }
        if v := os.Getenv("AI_MAX_TOKENS"); v != "" {
                if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 8000 {
                        cfg.MaxTokens = n
                }
        }
        if v := os.Getenv("AI_MAX_RETRIES"); v != "" {
                if n, err := strconv.Atoi(v); err == nil && n >= 0 && n <= 5 {
                        cfg.MaxRetries = n
                }
        }
        if v := os.Getenv("AI_DEBOUNCE_MS"); v != "" {
                if n, err := strconv.Atoi(v); err == nil && n >= 0 && n <= 60000 {
                        cfg.DebounceMs = n
                }
        }
        if v := os.Getenv("AI_MAX_HISTORY"); v != "" {
                if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
                        cfg.MaxHistoryMessages = n
                }
        }
        return cfg
}

// IsMock returns true when no API key is configured — the engine should use
// MockProvider in that case (ch. 5.6 — mode test).
func (c AIConfig) IsMock() bool { return strings.TrimSpace(c.APIKey) == "" }

// String returns a debug-friendly representation (API key masked).
func (c AIConfig) String() string {
        key := "****"
        if c.APIKey == "" {
                key = "(empty — mock mode)"
        } else if len(c.APIKey) > 8 {
                key = c.APIKey[:4] + "…" + c.APIKey[len(c.APIKey)-4:]
        }
        return fmt.Sprintf("AIConfig{model=%s baseURL=%s key=%s temp=%.2f maxTokens=%d retries=%d debounce=%dms history=%d}",
                c.Model, c.BaseURL, key, c.Temperature, c.MaxTokens, c.MaxRetries, c.DebounceMs, c.MaxHistoryMessages)
}

// ToEngineConfig converts an AIConfig into the EngineConfig consumed by the
// engine. MaxHistoryMessages is also surfaced here (the engine uses it to
// bound the conversation context). MaxRetries is mapped to MaxConversationTurns
// (1 initial + retries, capped at 5 to avoid runaway loops).
func (c AIConfig) ToEngineConfig() EngineConfig {
        turns := c.MaxRetries + 1
        if turns > 5 {
                turns = 5
        }
        if turns < 1 {
                turns = 1
        }
        return EngineConfig{
                Model:                c.Model,
                Temperature:          c.Temperature,
                MaxTokens:            c.MaxTokens,
                DebounceMs:           c.DebounceMs,
                MaxConversationTurns: turns,
                MaxRetries:           c.MaxRetries,
                MaxHistoryMessages:   c.MaxHistoryMessages,
        }
}
