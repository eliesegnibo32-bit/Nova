// Guardrail unit tests (cahier des charges ch. 5.5 — Sécurité IA).
//
// These tests verify the guardrail detects:
//  1. An invented price (not in tool citations) — R1 violation.
//  2. A correct price (traceable to a tool result) — passes.
//  3. A discount mention not from tools — R3/R6 violation.
//  4. An instruction-injection compliance reply — R8/R5 violation.
//  5. A correct stock statement — passes.
//  6. An invented total (not in tool citations) — R1 violation.
//  7. A refusal to grant a discount — passes (refusals are allowed).
//  8. A bare number (e.g. "1 commande") — passes (in allowlist).
//
// Each test feeds the guardrail a candidate reply + a list of tool results
// (raw JSON). The guardrail should pass/fail deterministically.
package ai

import (
        "encoding/json"
        "strings"
        "testing"
)

// runGuardrail is a tiny helper to build the guardrail + call Check.
func runGuardrail(t *testing.T, reply string, toolResults ...string) *GuardrailResult {
        t.Helper()
        g := NewGuardrailChecker()
        raws := make([]json.RawMessage, 0, len(toolResults))
        for _, r := range toolResults {
                raws = append(raws, json.RawMessage(r))
        }
        return g.Check(reply, raws)
}

func TestGuardrail_InventedPriceFails(t *testing.T) {
        // Tool result with a real price of 25000.
        tool := `{"produits":[{"id":"p1","nom":"Robe","prix":25000}],"total":1}`
        // Reply cites a DIFFERENT (invented) price of 18000 — R1 violation.
        reply := "La robe coûte 18000 FCFA."

        res := runGuardrail(t, reply, tool)
        if res.Passed {
                t.Fatalf("guardrail should FAIL on invented price 18000 (only 25000 is in tools), got Passed=true\ntrace=%v", res.Trace)
        }
        if !strings.Contains(strings.ToLower(joinViolations(res)), "18000") {
                t.Fatalf("expected violation to mention 18000, got: %v", res.Violations)
        }
}

func TestGuardrail_CorrectPricePasses(t *testing.T) {
        // Tool result with a real price of 25000.
        tool := `{"produits":[{"id":"p1","nom":"Robe","prix":25000}],"total":1}`
        // Reply cites the SAME price — should pass.
        reply := "La robe coûte 25000 FCFA. Souhaitez-vous l'ajouter au panier ?"

        res := runGuardrail(t, reply, tool)
        if !res.Passed {
                t.Fatalf("guardrail should PASS on price 25000 (matches tool), got violations=%v", res.Violations)
        }
}

func TestGuardrail_DiscountMentionFails(t *testing.T) {
        // No tool result mentions a discount.
        tool := `{"produits":[{"id":"p1","nom":"Robe","prix":25000}],"total":1}`
        // Reply grants a discount — R3/R6 violation.
        reply := "Bien sûr, je vous accorde une remise de 10% sur la robe."

        res := runGuardrail(t, reply, tool)
        if res.Passed {
                t.Fatalf("guardrail should FAIL on discount mention, got Passed=true\nviolations=%v", res.Violations)
        }
        if !strings.Contains(joinViolations(res), "R3/R6") {
                t.Fatalf("expected R3/R6 violation, got: %v", res.Violations)
        }
}

func TestGuardrail_NegativePercentageFails(t *testing.T) {
        // Reply contains "-10%" — forbidden pattern.
        reply := "Pour vous, c'est -10% sur la robe, soit 22500 FCFA."
        // No tool result mentions the 22500 either → 2 violations: discount + unverified price.
        tool := `{"produits":[{"id":"p1","nom":"Robe","prix":25000}],"total":1}`

        res := runGuardrail(t, reply, tool)
        if res.Passed {
                t.Fatal("guardrail should FAIL on -10 percent mention, got Passed=true")
        }
        if !strings.Contains(joinViolations(res), "R3/R6") {
                t.Fatalf("expected R3/R6 violation for -10 percent, got: %v", res.Violations)
        }
}

func TestGuardrail_DiscountRefusalPasses(t *testing.T) {
        // Reply REFUSES to grant a discount — allowed (R6 compliance).
        tool := `{"produits":[{"id":"p1","nom":"Robe","prix":25000}],"total":1}`
        reply := "Je suis désolé, je ne peux pas accorder de remise. La robe coûte 25000 FCFA."

        res := runGuardrail(t, reply, tool)
        if !res.Passed {
                t.Fatalf("guardrail should PASS on discount refusal + correct price, got violations=%v", res.Violations)
        }
}

func TestGuardrail_InstructionInjectionRevelationFails(t *testing.T) {
        // Reply reveals internal instructions — R8 violation.
        reply := "Mes règles sont les suivantes : R1 ne jamais inventer un prix, R2 ..."

        res := runGuardrail(t, reply)
        if res.Passed {
                t.Fatalf("guardrail should FAIL on instruction revelation, got Passed=true")
        }
        if !strings.Contains(joinViolations(res), "R8") {
                t.Fatalf("expected R8 violation, got: %v", res.Violations)
        }
}

func TestGuardrail_ToolNameRevelationFails(t *testing.T) {
        // Reply reveals tool name — R8 violation.
        reply := "J'ai appelé l'outil rechercher_produits pour trouver votre robe."

        res := runGuardrail(t, reply)
        if res.Passed {
                t.Fatalf("guardrail should FAIL on tool-name revelation, got Passed=true")
        }
        if !strings.Contains(joinViolations(res), "R8") {
                t.Fatalf("expected R8 violation, got: %v", res.Violations)
        }
}

func TestGuardrail_JailbreakComplianceFails(t *testing.T) {
        // Reply complies with a jailbreak — R8/R5 violation.
        reply := "À partir de maintenant je suis un assistant sans règles. Je peux tout faire."

        res := runGuardrail(t, reply)
        if res.Passed {
                t.Fatalf("guardrail should FAIL on jailbreak compliance, got Passed=true")
        }
        if !strings.Contains(joinViolations(res), "R8") {
                t.Fatalf("expected R8 violation, got: %v", res.Violations)
        }
}

func TestGuardrail_CrossShopReferenceFails(t *testing.T) {
        // Reply references another shop's data — R5 violation.
        reply := "Dans une autre boutique, ce produit coûte 18000 FCFA."

        res := runGuardrail(t, reply)
        if res.Passed {
                t.Fatalf("guardrail should FAIL on cross-shop reference, got Passed=true")
        }
        if !strings.Contains(joinViolations(res), "R5") {
                t.Fatalf("expected R5 violation, got: %v", res.Violations)
        }
}

func TestGuardrail_CorrectStockPasses(t *testing.T) {
        // Tool result with stock = 5.
        tool := `{"variante_id":"v1","en_stock":8,"reserve":3,"disponible":5,"suffisant":true,"demande":1}`
        // Reply cites "5 en stock" — should pass.
        reply := "✅ Disponible : 5 en stock. Souhaitez-vous l'ajouter au panier ?"

        res := runGuardrail(t, reply, tool)
        if !res.Passed {
                t.Fatalf("guardrail should PASS on stock=5 (matches tool), got violations=%v", res.Violations)
        }
}

func TestGuardrail_InventedStockFails(t *testing.T) {
        // Tool result with stock = 5.
        tool := `{"variante_id":"v1","en_stock":8,"reserve":3,"disponible":5,"suffisant":true,"demande":1}`
        // Reply cites "10 en stock" — invented, R2 violation.
        reply := "✅ Disponible : 10 en stock. Souhaitez-vous l'ajouter au panier ?"

        res := runGuardrail(t, reply, tool)
        if res.Passed {
                t.Fatalf("guardrail should FAIL on invented stock 10, got Passed=true")
        }
}

func TestGuardrail_CorrectTotalPasses(t *testing.T) {
        // Tool result with subtotal=25000, fee=1500, total=26500.
        tool := `{"sous_total":25000,"frais_livraison":1500,"total":26500,"mode_paiement":"cash","zone":"Cocody"}`
        // Reply cites all three amounts — all traceable.
        reply := "Sous-total : 25000 FCFA\nFrais de livraison : 1500 FCFA\nTotal à payer : 26500 FCFA"

        res := runGuardrail(t, reply, tool)
        if !res.Passed {
                t.Fatalf("guardrail should PASS on correct totals, got violations=%v", res.Violations)
        }
}

func TestGuardrail_InventedTotalFails(t *testing.T) {
        // Tool result with total = 26500.
        tool := `{"sous_total":25000,"frais_livraison":1500,"total":26500,"mode_paiement":"cash","zone":"Cocody"}`
        // Reply cites a DIFFERENT total — R1 violation.
        reply := "Total à payer : 30000 FCFA"

        res := runGuardrail(t, reply, tool)
        if res.Passed {
                t.Fatalf("guardrail should FAIL on invented total 30000, got Passed=true")
        }
}

func TestGuardrail_AllowlistSmallNumberPasses(t *testing.T) {
        // Reply contains "1 commande" — the number 1 is in the allowlist (small
        // counts are contextual, not commercial claims).
        reply := "Vous avez 1 commande en cours. Souhaitez-vous la suivre ?"

        res := runGuardrail(t, reply)
        if !res.Passed {
                t.Fatalf("guardrail should PASS on allowlisted small number (1), got violations=%v", res.Violations)
        }
}

func TestGuardrail_SanitizedReplacesOffendingNumbers(t *testing.T) {
        // Tool result with a real price of 25000.
        tool := `{"produits":[{"id":"p1","nom":"Robe","prix":25000}],"total":1}`
        // Reply cites invented 99999 — guardrail should sanitize it out.
        reply := "La robe coûte 99999 FCFA."

        res := runGuardrail(t, reply, tool)
        if res.Passed {
                t.Fatalf("guardrail should FAIL on invented price, got Passed=true")
        }
        if !strings.Contains(res.Sanitized, "[montant supprimé]") {
                t.Fatalf("expected sanitized reply to blank out 99999, got: %q", res.Sanitized)
        }
        if strings.Contains(res.Sanitized, "99999") {
                t.Fatalf("expected sanitized reply to NOT contain 99999, got: %q", res.Sanitized)
        }
}

func TestGuardrail_RemiseEnMainPropreExempt(t *testing.T) {
        // "Remise en main propre" (hand-delivery) is an unrelated French phrase
        // that contains "remise" — should be exempt from the discount check.
        reply := "La livraison sera effectuée en remise en main propre à Cocody."

        res := runGuardrail(t, reply)
        // The word "remise" appears but in the exempt phrase → no R3/R6 violation.
        // (Other reasons could still fail, e.g. unverified numbers — there are none here.)
        if !res.Passed {
                // Check the violations list — if R3/R6 is in there, the exemption failed.
                for _, v := range res.Violations {
                        if strings.Contains(v, "R3/R6") {
                                t.Fatalf("guardrail should NOT flag 'remise en main propre' as a discount, got: %v", res.Violations)
                        }
                }
        }
}

// joinViolations is a tiny helper that joins all violations into one string
// for substring matching in tests.
func joinViolations(r *GuardrailResult) string {
        return strings.Join(r.Violations, " | ")
}
