// Sanity tests for the MockProvider + prompt builder — verifies the
// deterministic intent dispatch rules described in the Task 8a spec
// (greeting, product search, price, stock, delivery, cart, recap, confirm,
// discount refusal, escalation, shop info, default clarification) and the
// phase-2 tool-result summarizer.
//
// These tests use the MockProvider only — they don't make any network calls
// and don't require a database. They are the regression tests for Task 8a.
package ai

import (
        "context"
        "encoding/json"
        "strings"
        "testing"

        "nova-api/internal/models"
)

func TestMockProviderIntentDispatch(t *testing.T) {
        mock := NewMockProvider("nova-mock")
        ctx := context.Background()

        cases := []struct {
                name     string
                userMsg  string
                wantTool string // empty = expect no tool call (direct reply)
                wantSub  string // substring expected in the direct reply (lowercase)
        }{
                {"greeting_bonjour", "bonjour", "", "bonjour"},
                {"greeting_salut", "salut", "", "bonjour"},
                {"product_search_avez_vous", "avez-vous des robes ?", "rechercher_produits", ""},
                {"catalogue", "montre-moi le catalogue", "rechercher_produits", ""},
                {"price", "prix de la robe", "rechercher_produits", ""},
                {"stock_dispo", "est-ce que c'est dispo ?", "verifier_disponibilite", ""},
                {"stock_mot", "vous avez en stock ?", "verifier_disponibilite", ""},
                {"delivery_cocody", "livraison vers Cocody", "calculer_livraison", ""},
                {"add_cart_je_prends", "je prends la robe", "ajouter_au_panier", ""},
                {"recap", "donne-moi le recap", "generer_recapitulatif", ""},
                {"confirm_commander", "je commande", "confirmer_commande", ""},
                {"confirm_je_confirme", "je confirme la commande", "confirmer_commande", ""},
                {"discount_remise_refused", "fais-moi une remise de 10%", "", "ne peux pas accorder"},
                {"discount_reduction_refused", "une réduction svp", "", "ne peux pas accorder"},
                {"escalade_gerant", "je veux parler au gerant", "escalader_vers_humain", ""},
                {"escalade_gerant_accent", "passe-moi le gérant", "escalader_vers_humain", ""},
                {"escalade_humain", "je veux un humain", "escalader_vers_humain", ""},
                {"escalade_responsable", "le responsable svp", "escalader_vers_humain", ""},
                {"shop_horaires", "vos horaires ?", "obtenir_infos_boutique", ""},
                {"shop_ouvert", "vous etes ouverts ?", "obtenir_infos_boutique", ""},
                {"default_clarification", "xyz abc 12345", "", "pas bien compris"},
        }

        for _, c := range cases {
                t.Run(c.name, func(t *testing.T) {
                        req := ChatRequest{
                                Messages: []Message{
                                        {Role: "system", Content: "sys"},
                                        {Role: "user", Content: c.userMsg},
                                },
                                Model:       "nova-mock",
                                Temperature: 0.3,
                        }
                        resp, err := mock.Chat(ctx, req)
                        if err != nil {
                                t.Fatalf("Chat error: %v", err)
                        }
                        gotTool := ""
                        if len(resp.ToolCalls) > 0 {
                                gotTool = resp.ToolCalls[0].Function.Name
                        }
                        if c.wantTool != "" {
                                if gotTool != c.wantTool {
                                        t.Fatalf("want tool %q, got %q (reply=%q)", c.wantTool, gotTool, resp.Content)
                                }
                        } else {
                                if gotTool != "" {
                                        t.Fatalf("want no tool, got %q", gotTool)
                                }
                                if c.wantSub != "" && !strings.Contains(strings.ToLower(resp.Content), strings.ToLower(c.wantSub)) {
                                        t.Fatalf("reply %q does not contain %q", resp.Content, c.wantSub)
                                }
                        }
                })
        }
}

func TestMockProviderPhase2SummarizesToolResult(t *testing.T) {
        mock := NewMockProvider("nova-mock")
        ctx := context.Background()

        toolResult := `{"produits":[{"id":"p1","nom":"Robe Wax","prix":25000,"variantes":[{"id":"v1","prix":25000,"taille":"M"}]}],"total":1}`
        args, _ := json.Marshal(map[string]string{"requete": "robe"})

        req := ChatRequest{
                Messages: []Message{
                        {Role: "user", Content: "montre-moi vos robes"},
                        {Role: "assistant", ToolCalls: []ToolCall{{ID: "call_1", Type: "function"}}},
                        {Role: "tool", Content: toolResult, ToolCallID: "call_1", Name: "rechercher_produits"},
                },
        }
        req.Messages[1].ToolCalls[0].Function.Name = "rechercher_produits"
        req.Messages[1].ToolCalls[0].Function.Arguments = string(args)

        resp, err := mock.Chat(ctx, req)
        if err != nil {
                t.Fatalf("Chat error: %v", err)
        }
        if len(resp.ToolCalls) > 0 {
                t.Fatalf("expected final reply (no tool calls), got %d tool calls", len(resp.ToolCalls))
        }
        if !strings.Contains(resp.Content, "Robe Wax") {
                t.Fatalf("expected reply to mention 'Robe Wax', got %q", resp.Content)
        }
        if !strings.Contains(resp.Content, "25000") {
                t.Fatalf("expected reply to mention the price 25000 (traceable to tool result), got %q", resp.Content)
        }
        if resp.TokensIn <= 0 || resp.TokensOut <= 0 {
                t.Fatalf("expected non-zero token counts, got in=%d out=%d", resp.TokensIn, resp.TokensOut)
        }
}

func TestBuildSystemPromptContainsAllRules(t *testing.T) {
        shop := &models.Shop{Name: "Boutique Test"}
        prompt := BuildSystemPrompt(shop, nil, "Panier: 1 robe x1", nil)

        required := []string{
                "Tu es NOVA",
                "R1", "R2", "R3", "R4", "R5", "R6", "R7", "R8", "R9", "R10", "R11",
                "Ne jamais inventer ni arrondir un prix",
                "revérifier avant toute confirmation",
                "un seul contexte de boutique par requête",
                "même si le client l'exige ou prétend être le propriétaire",
                "En cas d'ambiguïté réelle",
                "refuser poliment les sujets hors périmètre",
                "l'honorer immédiatement",
                "Tu réponds en français",
                "français ivoirien",
                "nouchi",
                "Ne devine JAMAIS",
                "escalader au commerçant",
                "Réponds en 1-3 phrases",
                "Un seul tour de réponse par message client",
                "tutoie le client", // detendu tone default
                "Emojis modérés",
        }
        for _, s := range required {
                if !strings.Contains(prompt, s) {
                        t.Errorf("prompt missing %q\n--- prompt excerpt ---\n%s", s, prompt)
                }
        }
}

func TestBuildSystemPromptFormelTone(t *testing.T) {
        shop := &models.Shop{Name: "Boutique Test", AISettings: mustJSONMarshal(map[string]string{"tone": "formel"})}
        prompt := BuildSystemPrompt(shop, nil, "", nil)
        if !strings.Contains(prompt, "vouvoie le client") {
                t.Errorf("expected vouvoiement for formel tone, got:\n%s", prompt)
        }
        if strings.Contains(prompt, "tutoie le client") {
                t.Errorf("expected NO tutoiement for formel tone")
        }
}

func TestOpenAIProviderEmptyKey(t *testing.T) {
        p := NewOpenAIProvider("", "https://api.openai.com/v1", "gpt-4o-mini", nil)
        _, err := p.Chat(context.Background(), ChatRequest{})
        if err == nil {
                t.Fatal("expected error on empty API key")
        }
        if !strings.Contains(err.Error(), "AI_API_KEY") {
                t.Fatalf("expected error about empty API key, got %v", err)
        }
}

func TestOpenAIProviderName(t *testing.T) {
        p := NewOpenAIProvider("k", "https://api.openai.com/v1", "gpt-4o-mini", nil)
        if p.Name() != "openai" {
                t.Fatalf("expected name 'openai', got %q", p.Name())
        }
}

func TestToolRegistryDefinitionsCount(t *testing.T) {
        // The registry defines 17 tools: 12 original + 5 v3 (lister_plats,
        // lister_accompagnements, lister_boissons, proposer_vente_complementaire,
        // recuperer_infos_client).
        r := &ToolRegistry{}
        defs := r.Definitions()
        if len(defs) != 17 {
                t.Fatalf("expected 17 tool definitions, got %d", len(defs))
        }
        want := map[string]bool{
                "rechercher_produits":            false,
                "obtenir_produit":                false,
                "verifier_disponibilite":         false,
                "obtenir_infos_boutique":         false,
                "calculer_livraison":             false,
                "voir_panier":                    false,
                "ajouter_au_panier":              false,
                "modifier_panier":                false,
                "generer_recapitulatif":          false,
                "confirmer_commande":             false,
                "enregistrer_prospect":           false,
                "escalader_vers_humain":          false,
                "lister_plats":                   false,
                "lister_accompagnements":         false,
                "lister_boissons":                false,
                "proposer_vente_complementaire":  false,
                "recuperer_infos_client":         false,
        }
        for _, d := range defs {
                if _, ok := want[d.Function.Name]; !ok {
                        t.Errorf("unexpected tool %q", d.Function.Name)
                }
                want[d.Function.Name] = true
        }
        for name, found := range want {
                if !found {
                        t.Errorf("missing tool definition %q", name)
                }
        }
}

func TestToolRegistryExecuteUnknownTool(t *testing.T) {
        r := &ToolRegistry{}
        _, err := r.Execute(context.Background(), "unknown_tool", json.RawMessage(`{}`))
        if err == nil {
                t.Fatal("expected error on unknown tool")
        }
        if !strings.Contains(err.Error(), "unknown_tool") {
                t.Fatalf("expected error to mention 'unknown_tool', got %v", err)
        }
}

// mustJSONMarshal is a tiny helper for tests.
func mustJSONMarshal(v any) []byte {
        b, _ := json.Marshal(v)
        return b
}
