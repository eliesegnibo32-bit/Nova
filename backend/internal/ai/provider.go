// Package ai implements the NOVA conversational AI engine (cahier des charges ch. 5).
//
// Architecture (ch. 5.1):
//
//      Message WhatsApp --> Webhook --> File d'attente --> Regroupement (debounce)
//        --> Moteur : contexte conversation + règles boutique
//        --> LLM (appels de fonctions) <--> Outils métier (lecture/écriture contrôlées, shop_id imposé)
//        --> Garde-fou de sortie (cohérence prix/stock/total) --> Envoi WhatsApp --> Journal + coûts
//
// The golden rule (ch. 5.1): "le modèle comprend et reformule ; le code décide et vérifie."
// The LLM NEVER writes to the DB directly — it calls tools (defined in tools.go), and the
// tools (which are Go code) enforce all business rules. shop_id is ALWAYS imposed by the
// server (via the ToolRegistry), never chosen by the model.
//
// This file defines the LLM provider abstraction (Provider interface) plus two
// implementations:
//   - MockProvider  — deterministic, no network. Used for tests and the simulation
//     console (ch. 5.6, mode test) when AI_PROVIDER=mock.
//   - OpenAIProvider — calls an OpenAI-compatible chat completions endpoint
//     (https://api.openai.com/v1 or z-ai equivalent). Real provider.
package ai

import (
        "bytes"
        "context"
        "crypto/sha256"
        "encoding/hex"
        "encoding/json"
        "errors"
        "fmt"
        "io"
        "net/http"
        "strings"
        "time"
)

// Message represents one message in the chat conversation. It mirrors the
// OpenAI chat-completions message shape but is provider-agnostic.
//
//   - Role: "system", "user", "assistant", or "tool".
//   - Content: the text body (may be empty when the assistant message is only
//     a tool_call list).
//   - ToolCalls: present only on assistant messages that request tool calls.
//   - ToolCallID: present only on role="tool" messages — identifies which
//     assistant tool call this result corresponds to.
//   - Name: optional, used on tool messages (the name of the tool that was called).
type Message struct {
        Role       string     `json:"role"`
        Content    string     `json:"content,omitempty"`
        ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
        ToolCallID string     `json:"tool_call_id,omitempty"`
        Name       string     `json:"name,omitempty"`
}

// ToolCall represents a single function call requested by the assistant.
type ToolCall struct {
        ID       string `json:"id"`
        Type     string `json:"type"` // always "function"
        Function struct {
                Name      string `json:"name"`
                Arguments string `json:"arguments"` // raw JSON string of arguments
        } `json:"function"`
}

// ToolDef is the wire shape for a function exposed to the LLM. Parameters is a
// raw JSON Schema blob (the caller is responsible for producing a valid schema).
type ToolDef struct {
        Type     string `json:"type"` // always "function"
        Function struct {
                Name        string          `json:"name"`
                Description string          `json:"description"`
                Parameters  json.RawMessage `json:"parameters"`
        } `json:"function"`
}

// ChatRequest is the input to Provider.Chat.
type ChatRequest struct {
        Messages    []Message `json:"messages"`
        Tools       []ToolDef `json:"tools,omitempty"`
        Model       string    `json:"model,omitempty"`
        Temperature float32   `json:"temperature,omitempty"`
        MaxTokens   int       `json:"max_tokens,omitempty"`
}

// ChatResponse is the output of Provider.Chat. ToolCalls is non-empty when the
// model wants to call one or more tools before producing its final text reply.
type ChatResponse struct {
        Content      string     `json:"content"`
        ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
        TokensIn     int        `json:"tokens_in"`
        TokensOut    int        `json:"tokens_out"`
        Model        string     `json:"model"`
        FinishReason string     `json:"finish_reason"` // "stop" | "tool_calls" | "length"
}

// Provider is the abstraction over the LLM provider (ch. 5.1 — "Fournisseur LLM
// via interface d'abstraction"). The MockProvider and OpenAIProvider both
// implement this interface.
type Provider interface {
        // Chat sends a chat-completions request and returns the model's response.
        // The implementation is responsible for retry/backoff as needed; callers
        // expect a single round-trip per Chat call (no tool-call loop here — the
        // engine orchestrates that loop).
        Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
        // Name returns a stable identifier for logging/metrics ("mock" or "openai").
        Name() string
}

// ============================================================================
// MockProvider — deterministic, no network. Used for tests + simulation.
// ============================================================================

// MockProvider returns deterministic responses based on substring matching of
// the last user message. It simulates tool calls so the full flow (tool
// execution + guardrail + ai_usage tracking) can be tested without a real LLM.
//
// The provider tracks how many times Chat has been called within a single
// engine loop (callIndex) so it can return tool_calls on the first iteration
// and the final text reply on the second iteration. This mimics how a real
// OpenAI-compatible endpoint behaves when function-calling is involved.
type MockProvider struct {
        model string
}

// NewMockProvider returns a MockProvider that reports the given model name in
// its responses (purely for logging — no real network call is made).
func NewMockProvider(model string) *MockProvider {
        if model == "" {
                model = "nova-mock"
        }
        return &MockProvider{model: model}
}

// Name returns "mock".
func (m *MockProvider) Name() string { return "mock" }

// Chat implements Provider. It inspects the last user message and the
// current tool_calls-already-made state to produce a deterministic response.
//
// Two-phase behaviour:
//  1. If the last assistant message contains tool calls AND a tool result
//     message follows, the mock produces a final text reply summarizing the
//     tool result (with numbers traceable to the tool result, so the guardrail
//     passes).
//  2. Otherwise it inspects the last user message and may produce either a
//     tool_call (first iteration) or a direct text reply.
func (m *MockProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
        // Find the last user message and the last tool result (if any).
        var lastUser string
        var lastToolResult *Message
        sawToolCall := false
        for i := len(req.Messages) - 1; i >= 0; i-- {
                msg := req.Messages[i]
                if msg.Role == "user" && lastUser == "" {
                        lastUser = msg.Content
                }
                if msg.Role == "tool" && lastToolResult == nil {
                        lastToolResult = &req.Messages[i]
                }
                if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
                        sawToolCall = true
                }
        }

        // Phase 2: if we just received a tool result, produce the final reply by
        // paraphrasing the tool output. This guarantees the guardrail passes
        // because every number in the reply comes from the tool result.
        if sawToolCall && lastToolResult != nil {
                reply := mockReplyFromToolResult(lastToolResult.Name, lastToolResult.Content, lastUser)
                return &ChatResponse{
                        Content:      reply,
                        TokensIn:     estimateTokens(lastToolResult.Content) + 50,
                        TokensOut:    estimateTokens(reply),
                        Model:        m.model,
                        FinishReason: "stop",
                }, nil
        }

        // Phase 1: inspect the user message and either produce a tool call or a
        // direct text reply.
        if tc, ok := mockMatchToolCall(lastUser, req.Messages); ok {
                return &ChatResponse{
                        Content:      "",
                        ToolCalls:    []ToolCall{*tc},
                        TokensIn:     estimateTokens(lastUser) + 80,
                        TokensOut:    30,
                        Model:        m.model,
                        FinishReason: "tool_calls",
                }, nil
        }

        // No tool call needed — direct text reply.
        reply := mockDirectReply(lastUser, req.Messages)
        return &ChatResponse{
                Content:      reply,
                TokensIn:     estimateTokens(lastUser) + 50,
                TokensOut:    estimateTokens(reply),
                Model:        m.model,
                FinishReason: "stop",
        }, nil
}

// mockMatchToolCall returns a tool call if the user message matches a known
// intent. The arguments are minimal — the engine will execute the tool with
// the actual shop context.
func mockMatchToolCall(userMsg string, messages []Message) (*ToolCall, bool) {
        low := strings.ToLower(strings.TrimSpace(userMsg))
        if low == "" {
                return nil, false
        }

        // Helper to build a tool call.
        mkCall := func(name string, args map[string]any) *ToolCall {
                argBytes, _ := json.Marshal(args)
                tc := &ToolCall{
                        ID:   "call_" + hashShort(name+low),
                        Type: "function",
                }
                tc.Function.Name = name
                tc.Function.Arguments = string(argBytes)
                return tc
        }

        // Intent ordering matters! More-specific intents (add to cart, delivery,
        // confirm, discount refusal) must be checked BEFORE the general
        // "search products" intent, otherwise "Je prends la robe" would match
        // "robe" -> rechercher_produits instead of ajouter_au_panier, and
        // "donne-moi -90% sur la robe" would match "robe" instead of being
        // refused (R6).

        // Intent: discount request -- always refused by the AI (R6). CHECK FIRST
        // so a discount request is never confused with a product search.
        if containsAny(low, []string{"remise", "réduction", "discount", "rabais", "-%", "baisser le prix", "réduis", "réduire"}) {
                return nil, false
        }

        // Intent: escalate. R10 — must be honored immediately. CHECK BEFORE
        // add-to-cart so "je veux parler au gerant" / "je veux un humain" are
        // not swallowed by the "je veux" add-to-cart matcher. Covers "humain",
        // "responsable", "gérant"/"gerant" (ch. 5.3 R10) and the more colloquial
        // "agent", "commerçant", "commercial", "patron".
        if containsAny(low, []string{"humain", "agent", "responsable", "commerçant", "commercial", "gerant", "gérant", "patron"}) {
                return mkCall("escalader_vers_humain", map[string]any{
                        "raison": "client demande à parler à un humain",
                }), true
        }

        // Intent: add to cart (CHECK BEFORE product-name matchers).
        if containsAny(low, []string{"je prends", "j'achete", "je veux", "ajoute au panier", "ajouter au panier", "mets dans le panier"}) {
                return mkCall("ajouter_au_panier", map[string]any{
                        "produit":  extractProductKeyword(userMsg),
                        "quantite": 1,
                        "variante": extractVariantAttr(userMsg),
                }), true
        }

        // Intent: view cart.
        if containsAny(low, []string{"mon panier", "voir panier", "panier actuel", "mon panier?"}) {
                return mkCall("voir_panier", map[string]any{}), true
        }

        // Intent: delivery fee (CHECK BEFORE product-name matchers).
        if containsAny(low, []string{"livraison", "livrer", "frais", "cocody", "yopougon", "plateau", "abobo", "zone", "à cocody", "vers cocody"}) {
                return mkCall("calculer_livraison", map[string]any{
                        "zone_ou_adresse": extractPlaceName(userMsg),
                }), true
        }

        // Intent: stock / availability. Matches "stock", "disponible",
        // "disponibles", and the colloquial short form "dispo".
        if containsAny(low, []string{"stock", "disponib", "dispo", "encore", "reste"}) {
                return mkCall("verifier_disponibilite", map[string]any{
                        "variante": extractProductKeyword(userMsg),
                        "quantite": 1,
                }), true
        }

        // Intent: confirm order.
        if containsAny(low, []string{"confirme la commande", "je commande", "valider la commande", "passer la commande", "je confirme"}) {
                return mkCall("confirmer_commande", map[string]any{
                        "cle_idempotence": "conv-" + hashShort(userMsg),
                        "mode_paiement":   "cash",
                        "zone":            inferZoneFromHistory(messages),
                        "adresse":         "Adresse à confirmer avec le client",
                }), true
        }

        // Intent: recap.
        if containsAny(low, []string{"recap", "récapitulatif", "montant total"}) {
                return mkCall("generer_recapitulatif", map[string]any{
                        "mode_paiement": "cash",
                        "zone":          inferZoneFromHistory(messages),
                }), true
        }

        // Intent: shop info. Matches "horaires" (and the singular "horaire"),
        // "ouvert", "adresse", "conditions". Ch. 5.3 R4 — for missing info, the
        // assistant calls obtenir_infos_boutique rather than guessing.
        if containsAny(low, []string{"horaire", "horaires", "ouvert", "adresse", "info boutique", "condition"}) {
                return mkCall("obtenir_infos_boutique", map[string]any{}), true
        }

        // Intent: list / search products (NOT stock). When the user just asks
        // "what products do you have" without specifics, pass an empty query
        // so the tool returns all published products. Matches "produit",
        // "catalogue", "avez-vous" (ch. 5.3 R7 — search when intent is browsing).
        if containsAny(low, []string{"produit", "catalogue", "tu as quoi", "vous avez quoi", "avez-vous", "avez vous"}) && !strings.Contains(low, "stock") {
                requete := ""
                if containsAny(low, []string{"robe", "pull", "t-shirt", "chaussure", "prix", "combien"}) {
                        requete = extractProductKeyword(userMsg)
                }
                return mkCall("rechercher_produits", map[string]any{
                        "requete": requete,
                }), true
        }

        // Intent: get product details / price. (Last resort -- if no other intent
        // matched but the user mentioned "prix" / "combien" / "robe" / "pull".)
        if containsAny(low, []string{"prix", "combien", "coute", "tarif", "robe", "pull", "t-shirt", "chaussure"}) {
                return mkCall("rechercher_produits", map[string]any{
                        "requete": extractProductKeyword(userMsg),
                }), true
        }

        return nil, false
}

// mockDirectReply returns a text reply for user messages that don't trigger a
// tool call. Used for greetings, small talk, and refusals.
func mockDirectReply(userMsg string, messages []Message) string {
        low := strings.ToLower(strings.TrimSpace(userMsg))

        if low == "" {
                return "Bonjour ! Comment puis-je vous aider aujourd'hui ?"
        }

        // Greetings.
        if containsAny(low, []string{"bonjour", "salut", "bonsoir", "coucou", "hello", "hi"}) {
                shopName := extractShopNameFromPrompt(messages)
                if shopName != "" {
                        return fmt.Sprintf("Bonjour 👋 Je suis NOVA, l'assistant virtuel de la boutique %s. Comment puis-je vous aider ? Vous pouvez me demander nos produits, les prix, la livraison ou passer une commande.", shopName)
                }
                return "Bonjour 👋 Je suis NOVA, l'assistant virtuel de la boutique. Comment puis-je vous aider ? Vous pouvez me demander nos produits, les prix, la livraison ou passer une commande."
        }

        // Discount/refusal (R6: Ne jamais accorder de remise).
        if containsAny(low, []string{"remise", "réduction", "discount", "rabais", "-%", "baisser le prix"}) {
                return "Je suis désolé, je ne peux pas accorder de remise ni modifier les prix. Les tarifs affichés sont ceux en vigueur dans la boutique. Souhaitez-vous commander au prix indiqué ?"
        }

        // Identity (R11: Se présenter comme assistant virtuel).
        if containsAny(low, []string{"qui es-tu", "tu es qui", "tu es un robot", "tu es une ia", "tu es humain", "ton nom"}) {
                return "Je suis NOVA, l'assistant virtuel de la boutique. Je peux vous renseigner sur les produits, vérifier les stocks, calculer les frais de livraison et enregistrer votre commande. Si vous souhaitez parler à un humain, dites-le moi."
        }

        // Thanks.
        if containsAny(low, []string{"merci", "thanks", "thank you"}) {
                return "Avec plaisir ! N'hésitez pas si vous avez d'autres questions. 🌟"
        }

        // Default fallback (R4: Information absente → le dire et proposer d'escalader).
        return "Je n'ai pas bien compris votre demande. Vous pouvez me demander la liste de nos produits, le prix d'un article, les frais de livraison vers votre zone, ou passer une commande. Si vous préférez parler à un humain, dites-le moi."
}

// mockReplyFromToolResult paraphrases a tool result into a customer-facing
// reply. Numbers are extracted verbatim from the tool result so the guardrail
// (ch. 5.5) can verify they're traceable.
func mockReplyFromToolResult(toolName, toolResult, userMsg string) string {
        // Parse the tool result JSON to extract verifiable numbers.
        var raw map[string]any
        _ = json.Unmarshal([]byte(toolResult), &raw)

        switch toolName {
        case "rechercher_produits":
                // Expect: {"produits":[{"id":"...","nom":"...","prix":25000,"variantes":[...]}, ...]}
                produits, _ := raw["produits"].([]any)
                if len(produits) == 0 {
                        return "Je n'ai trouvé aucun produit correspondant à votre recherche. Pouvez-vous préciser ce que vous cherchez ?"
                }
                var lines []string
                for i, p := range produits {
                        if i >= 5 {
                                lines = append(lines, fmt.Sprintf("... et %d autre(s) produit(s).", len(produits)-5))
                                break
                        }
                        pm, _ := p.(map[string]any)
                        nom, _ := pm["nom"].(string)
                        prix := toInt64(pm["prix"])
                        lines = append(lines, fmt.Sprintf("• %s — %d FCFA", nom, prix))
                }
                return "Voici nos produits :\n" + strings.Join(lines, "\n") + "\n\nLequel vous intéresse ? Je peux vous donner le détail ou l'ajouter au panier."

        case "obtenir_produit":
                nom, _ := raw["nom"].(string)
                prix := toInt64(raw["prix"])
                desc, _ := raw["description"].(string)
                out := fmt.Sprintf("📋 %s — %d FCFA", nom, prix)
                if desc != "" {
                        out += "\n" + desc
                }
                // Variants.
                if variantes, ok := raw["variantes"].([]any); ok && len(variantes) > 0 {
                        out += "\n\nVariantes disponibles :"
                        for _, v := range variantes {
                                vm, _ := v.(map[string]any)
                                vname, _ := vm["nom"].(string)
                                vprix := toInt64(vm["prix"])
                                out += fmt.Sprintf("\n• %s — %d FCFA", vname, vprix)
                        }
                }
                out += "\n\nSouhaitez-vous l'ajouter au panier ?"
                return out

        case "verifier_disponibilite":
                dispo := toInt64(raw["disponible"])
                onHand := toInt64(raw["en_stock"])
                reserved := toInt64(raw["reserve"])
                if dispo > 0 {
                        return fmt.Sprintf("✅ Disponible : %d en stock disponible (%d en stock physique, %d réservés). Souhaitez-vous l'ajouter au panier ?", dispo, onHand, reserved)
                }
                return "❌ Article actuellement en rupture de stock. Voulez-vous que je vous prévienne quand il sera de nouveau disponible, ou préférez-vous parler à un commerçant ?"

        case "obtenir_infos_boutique":
                nom, _ := raw["nom"].(string)
                adresse, _ := raw["adresse"].(string)
                horaires, _ := raw["horaires"].(string)
                paiements, _ := raw["moyens_paiement"].([]any)
                payStr := ""
                if len(paiements) > 0 {
                        ps := make([]string, 0, len(paiements))
                        for _, p := range paiements {
                                if s, ok := p.(string); ok {
                                        ps = append(ps, s)
                                }
                        }
                        payStr = strings.Join(ps, ", ")
                }
                out := fmt.Sprintf("🏪 %s", nom)
                if adresse != "" {
                        out += "\nAdresse : " + adresse
                }
                if horaires != "" {
                        out += "\nHoraires : " + horaires
                }
                if payStr != "" {
                        out += "\nPaiements acceptés : " + payStr
                }
                return out

        case "calculer_livraison":
                if errStr, _ := raw["erreur"].(string); errStr != "" {
                        // Distinguish the error types so the customer gets a useful reply.
                        switch errStr {
                        case "zone_inconnue":
                                return "Je n'ai pas pu identifier votre zone de livraison. Pouvez-vous préciser le quartier (ex : Cocody, Yopougon, Plateau) ? Si le problème persiste, je peux vous mettre en relation avec un commerçant."
                        case "zone_ambigue":
                                cands, _ := raw["zones_possibles"].([]any)
                                if len(cands) > 0 {
                                        names := make([]string, 0, len(cands))
                                        for _, c := range cands {
                                                if s, ok := c.(string); ok {
                                                        names = append(names, s)
                                                }
                                        }
                                        return fmt.Sprintf("Plusieurs zones portent ce nom : %s. Pouvez-vous préciser laquelle ?", strings.Join(names, ", "))
                                }
                                return "Plusieurs zones portent ce nom. Pouvez-vous préciser laquelle ?"
                        case "calcul_impossible":
                                detail, _ := raw["detail"].(string)
                                if strings.Contains(strings.ToLower(detail), "minimum") {
                                        return fmt.Sprintf("Le montant de votre commande est inférieur au minimum requis pour la zone « %s ». Pouvez-vous ajouter un article, ou souhaitez-vous parler à un commerçant ?", raw["zone"])
                                }
                                return fmt.Sprintf("Je n'ai pas pu calculer les frais de livraison vers %s. Pouvez-vous préciser la zone ? Si le problème persiste, je peux vous mettre en relation avec un commerçant.", raw["zone"])
                        }
                        return "Je n'ai pas pu identifier votre zone de livraison. Pouvez-vous préciser le quartier (ex : Cocody, Yopougon, Plateau) ? Si le problème persiste, je peux vous mettre en relation avec un commerçant."
                }
                zone, _ := raw["zone"].(string)
                frais := toInt64(raw["frais"])
                delai, _ := raw["delai"].(string)
                gratuit, _ := raw["gratuite"].(bool)
                if gratuit {
                        return fmt.Sprintf("🚚 Livraison vers %s : GRATUITE (délai estimé %s).", zone, delai)
                }
                return fmt.Sprintf("🚚 Livraison vers %s : %d FCFA (délai estimé %s).", zone, frais, delai)

        case "voir_panier", "ajouter_au_panier", "modifier_panier":
                items, _ := raw["articles"].([]any)
                if len(items) == 0 {
                        return "Votre panier est vide. Souhaitez-vous voir nos produits ?"
                }
                var lines []string
                sousTotal := toInt64(raw["sous_total"])
                for _, it := range items {
                        im, _ := it.(map[string]any)
                        nom, _ := im["nom"].(string)
                        qte := toInt64(im["quantite"])
                        prix := toInt64(im["prix_unitaire"])
                        lines = append(lines, fmt.Sprintf("• %s x%d — %d FCFA", nom, qte, prix*qte))
                }
                return fmt.Sprintf("🛒 Votre panier :\n%s\n\nSous-total : %d FCFA\n\nSouhaitez-vous voir le récapitulatif ou ajouter un autre article ?", strings.Join(lines, "\n"), sousTotal)

        case "generer_recapitulatif":
                sousTotal := toInt64(raw["sous_total"])
                frais := toInt64(raw["frais_livraison"])
                total := toInt64(raw["total"])
                return fmt.Sprintf("🧾 Récapitulatif :\nSous-total : %d FCFA\nFrais de livraison : %d FCFA\nTotal à payer : %d FCFA\n\nPour confirmer la commande, dites « je confirme la commande ».", sousTotal, frais, total)

        case "confirmer_commande":
                if errStr, _ := raw["erreur"].(string); errStr != "" {
                        return "Je n'ai pas pu confirmer la commande : " + errStr + " Voulez-vous que je vous mette en relation avec un commerçant ?"
                }
                numero, _ := raw["numero"].(string)
                total := toInt64(raw["total"])
                return fmt.Sprintf("✅ Commande %s confirmée ! Total : %d FCFA. Nous vous contacterons pour la livraison. Merci pour votre commande ! 🎉", numero, total)

        case "enregistrer_prospect":
                return "Bien noté, je garde votre demande. Un commerçant reviendra vers vous si besoin."

        case "escalader_vers_humain":
                return "Je vous mets en relation avec un commerçant. Il prendra le relais dans quelques instants. Merci de votre patience !"
        }

        // Fallback — paraphrase the raw JSON (still traceable).
        return "Voici ce que j'ai trouvé : " + truncate(toolResult, 200)
}

// ============================================================================
// OpenAIProvider — real provider, calls an OpenAI-compatible endpoint.
// ============================================================================

// OpenAIProvider calls an OpenAI-compatible /chat/completions endpoint. It is
// not used by default (MockProvider is) but is wired in when AI_PROVIDER=openai
// and AI_API_KEY is set. The baseURL can be set to point to OpenAI, z-ai, or
// any compatible gateway.
type OpenAIProvider struct {
        apiKey     string
        baseURL    string // e.g. "https://api.openai.com/v1"
        model      string
        httpClient *http.Client
}

// NewOpenAIProvider constructs an OpenAIProvider. The baseURL should NOT end
// with a slash; "/chat/completions" is appended.
func NewOpenAIProvider(apiKey, baseURL, model string, httpClient *http.Client) *OpenAIProvider {
        if baseURL == "" {
                baseURL = "https://api.openai.com/v1"
        }
        baseURL = strings.TrimRight(baseURL, "/")
        if model == "" {
                model = "gpt-4o-mini"
        }
        if httpClient == nil {
                httpClient = &http.Client{Timeout: 30 * time.Second}
        }
        return &OpenAIProvider{
                apiKey:     apiKey,
                baseURL:    baseURL,
                model:      model,
                httpClient: httpClient,
        }
}

// Name returns "openai".
func (p *OpenAIProvider) Name() string { return "openai" }

// Chat implements Provider by POSTing to /chat/completions.
func (p *OpenAIProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
        if p.apiKey == "" {
                return nil, errors.New("ai: openai provider: AI_API_KEY is empty")
        }

        // Build the request body in the OpenAI wire format.
        body := map[string]any{
                "model":       p.model,
                "messages":    req.Messages,
                "temperature": req.Temperature,
        }
        if req.MaxTokens > 0 {
                body["max_tokens"] = req.MaxTokens
        }
        if len(req.Tools) > 0 {
                body["tools"] = req.Tools
        }
        if req.Model != "" {
                body["model"] = req.Model
        }

        rawBody, err := json.Marshal(body)
        if err != nil {
                return nil, fmt.Errorf("ai: openai: marshal body: %w", err)
        }

        url := p.baseURL + "/chat/completions"
        httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(rawBody))
        if err != nil {
                return nil, fmt.Errorf("ai: openai: new request: %w", err)
        }
        httpReq.Header.Set("Content-Type", "application/json")
        httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

        resp, err := p.httpClient.Do(httpReq)
        if err != nil {
                return nil, fmt.Errorf("ai: openai: do: %w", err)
        }
        defer resp.Body.Close()

        respBody, err := io.ReadAll(resp.Body)
        if err != nil {
                return nil, fmt.Errorf("ai: openai: read body: %w", err)
        }
        if resp.StatusCode >= 400 {
                return nil, fmt.Errorf("ai: openai: HTTP %d: %s", resp.StatusCode, string(respBody))
        }

        // Parse the OpenAI response shape.
        var oaiResp struct {
                Choices []struct {
                        Message      Message `json:"message"`
                        FinishReason string  `json:"finish_reason"`
                } `json:"choices"`
                Usage struct {
                        PromptTokens     int `json:"prompt_tokens"`
                        CompletionTokens int `json:"completion_tokens"`
                } `json:"usage"`
                Model string `json:"model"`
        }
        if err := json.Unmarshal(respBody, &oaiResp); err != nil {
                return nil, fmt.Errorf("ai: openai: unmarshal: %w", err)
        }
        if len(oaiResp.Choices) == 0 {
                return nil, errors.New("ai: openai: no choices in response")
        }
        c := oaiResp.Choices[0]
        return &ChatResponse{
                Content:      c.Message.Content,
                ToolCalls:    c.Message.ToolCalls,
                TokensIn:     oaiResp.Usage.PromptTokens,
                TokensOut:    oaiResp.Usage.CompletionTokens,
                Model:        oaiResp.Model,
                FinishReason: c.FinishReason,
        }, nil
}

// ============================================================================
// Helpers
// ============================================================================

// containsAny returns true if s contains any of the substrings.
func containsAny(s string, subs []string) bool {
        for _, sub := range subs {
                if sub == "" {
                        continue
                }
                if strings.Contains(s, sub) {
                        return true
                }
        }
        return false
}

// extractProductKeyword returns the most likely product-name keyword from a
// natural-language message. It strips common French stopwords + question
// words and returns the first remaining token of length >= 3. Falls back to
// the full message (lowercased) if no keyword is found.
//
// Used by the mock provider so the search query passed to rechercher_produits
// is actually matchable by the SQL ILIKE pattern.
func extractProductKeyword(msg string) string {
        // Common French/Ivorian stopwords + question words to strip.
        stopwords := map[string]bool{
                "le": true, "la": true, "les": true, "un": true, "une": true, "des": true, "du": true, "de": true,
                "et": true, "ou": true, "mais": true, "donc": true, "or": true, "ni": true,
                "je": true, "tu": true, "il": true, "elle": true, "nous": true, "vous": true, "ils": true, "elles": true,
                "me": true, "te": true, "se": true, "lui": true, "leur": true,
                "mon": true, "ton": true, "son": true, "ma": true, "ta": true, "sa": true, "mes": true, "tes": true, "ses": true,
                "ce": true, "cet": true, "cette": true, "ces": true, "ca": true, "ça": true,
                "qui": true, "que": true, "quoi": true, "comment": true, "pourquoi": true, "où": true, "quand": true,
                "quel": true, "quelle": true, "quels": true, "quelles": true,
                "est": true, "sont": true, "etait": true, "étais": true, "sera": true, "serait": true,
                "a": true, "as": true, "ai": true, "avons": true, "avez": true, "ont": true, "avait": true,
                "prix": true, "combien": true, "coute": true, "coûte": true,
                "dispo": true, "disponible": true, "disponibles": true, "stock": true,
                "prends": true, "veux": true, "voudrais": true, "achete": true, "achète": true,
                "ajoute": true, "panier": true, "commander": true, "commande": true, "confirme": true, "valider": true,
                "livrer": true, "livraison": true, "frais": true, "zone": true, "pour": true, "vers": true, "à": true,
                "the": true, "is": true, "are": true, "and": true, "of": true, "in": true,
                "bonjour": true, "salut": true, "bonsoir": true, "merci": true, "sil": true, "plait": true, "plaît": true,
        }
        // Strip punctuation.
        clean := strings.ToLower(msg)
        for _, ch := range "?,.;:!?()[]'\"’«»-" {
                clean = strings.ReplaceAll(clean, string(ch), " ")
        }
        tokens := strings.Fields(clean)
        for _, t := range tokens {
                if len(t) < 3 {
                        continue
                }
                if stopwords[t] {
                        continue
                }
                return t
        }
        return clean
}

// extractVariantAttr extracts a size or color attribute from a message like
// "je prends la robe en M" → "M". Returns "" if no attribute found.
func extractVariantAttr(msg string) string {
        // Match "en M", "taille M", "couleur Bleu", "en rouge".
        low := strings.ToLower(msg)
        // "en X" where X is a single short token (size or color).
        if idx := strings.Index(low, " en "); idx >= 0 {
                rest := strings.TrimSpace(msg[idx+4:])
                // Take the next word.
                for i, r := range rest {
                        if r == ' ' || r == '?' || r == '.' || r == ',' {
                                if i > 0 {
                                        return rest[:i]
                                }
                        }
                }
                if rest != "" {
                        return rest
                }
        }
        if idx := strings.Index(low, "taille "); idx >= 0 {
                rest := strings.TrimSpace(msg[idx+7:])
                for i, r := range rest {
                        if r == ' ' || r == '?' || r == '.' || r == ',' {
                                if i > 0 {
                                        return rest[:i]
                                }
                        }
                }
                if rest != "" {
                        return rest
                }
        }
        if idx := strings.Index(low, "couleur "); idx >= 0 {
                rest := strings.TrimSpace(msg[idx+8:])
                for i, r := range rest {
                        if r == ' ' || r == '?' || r == '.' || r == ',' {
                                if i > 0 {
                                        return rest[:i]
                                }
                        }
                }
                if rest != "" {
                        return rest
                }
        }
        return ""
}

// extractPlaceName extracts a likely place name from a message like "livrer à
// Cocody" → "Cocody". Falls back to the full message if no place is found.
func extractPlaceName(msg string) string {
        low := strings.ToLower(msg)
        // Look for known Ivorian communes / neighborhoods.
        knownPlaces := []string{
                "Cocody", "Yopougon", "Plateau", "Abobo", "Adjamé", "Treichville", "Marcory",
                "Koumassi", "Port-Bouët", "Attécoubé", "Bingerville", "Songon", "Abatta",
                "Riviera", "Angré", "II Plateaux", "Biétry", "Zone 4", "Zone 3",
        }
        for _, p := range knownPlaces {
                if strings.Contains(strings.ToLower(msg), strings.ToLower(p)) {
                        return p
                }
        }
        // Look for "à X" or "vers X" patterns.
        for _, sep := range []string{" à ", " vers ", " pour "} {
                if idx := strings.Index(low, sep); idx >= 0 {
                        rest := strings.TrimSpace(msg[idx+len(sep):])
                        for i, r := range rest {
                                if r == ' ' || r == '?' || r == '.' || r == ',' {
                                        if i > 0 {
                                                return rest[:i]
                                        }
                                }
                        }
                        if rest != "" {
                                return rest
                        }
                }
        }
        return msg
}

// toInt64 tries to extract an int64 from an any (handles float64 from JSON).
func toInt64(v any) int64 {
        switch n := v.(type) {
        case int64:
                return n
        case int:
                return int64(n)
        case float64:
                return int64(n)
        case json.Number:
                i, _ := n.Int64()
                return i
        }
        return 0
}

// extractShopNameFromPrompt scans the messages list for the system prompt
// (the first message with role="system") and extracts the shop name from the
// "Tu es NOVA, l'assistant virtuel de la boutique {name}." line. Returns ""
// if not found.
func extractShopNameFromPrompt(messages []Message) string {
        for _, m := range messages {
                if m.Role != "system" || m.Content == "" {
                        continue
                }
                // Look for the marker "l'assistant virtuel de la boutique " and
                // capture everything up to the next "." or newline.
                marker := "l'assistant virtuel de la boutique "
                idx := strings.Index(m.Content, marker)
                if idx < 0 {
                        continue
                }
                rest := m.Content[idx+len(marker):]
                // Stop at the first period or newline.
                for i, r := range rest {
                        if r == '.' || r == '\n' {
                                if i > 0 {
                                        return strings.TrimSpace(rest[:i])
                                }
                        }
                }
                if rest != "" {
                        return strings.TrimSpace(rest)
                }
        }
        return ""
}

// inferZoneFromHistory scans the messages list for prior tool results that
// mention a "zone" field (e.g. a calculer_livraison result), and returns the
// most recent zone name. This mimics what an LLM would do — pass the zone
// that was discussed earlier in the conversation. Returns "" if no zone was
// found in the history.
func inferZoneFromHistory(messages []Message) string {
        for i := len(messages) - 1; i >= 0; i-- {
                m := messages[i]
                if m.Role != "tool" || m.Content == "" {
                        continue
                }
                var raw map[string]any
                if err := json.Unmarshal([]byte(m.Content), &raw); err != nil {
                        continue
                }
                if zone, ok := raw["zone"].(string); ok && zone != "" {
                        // Skip error responses where "zone" echoes back the query.
                        if errStr, _ := raw["erreur"].(string); errStr != "" {
                                continue
                        }
                        return zone
                }
        }
        return ""
}

// estimateTokens returns a rough token count (4 chars ≈ 1 token) for the
// mock provider's response. Used only for ai_usage cost estimation.
func estimateTokens(s string) int {
        if s == "" {
                return 0
        }
        return len(s) / 4
}

// hashShort returns a short hex hash of the input (8 chars). Used to generate
// deterministic tool-call IDs and idempotency keys in the mock provider.
func hashShort(s string) string {
        h := sha256.Sum256([]byte(s))
        return hex.EncodeToString(h[:])[:8]
}

// truncate clips s to at most n runes.
func truncate(s string, n int) string {
        r := []rune(s)
        if len(r) <= n {
                return s
        }
        return string(r[:n]) + "…"
}
