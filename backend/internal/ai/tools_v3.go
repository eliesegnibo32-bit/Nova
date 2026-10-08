// NOVA v3 — AI tools for product options (plats/accompagnements/boissons),
// food upsell, client info collection, and payment info.
//
// New tools (spec section 6):
//   - lister_plats                    — list plats for the shop
//   - lister_accompagnements          — list accompagnements
//   - lister_boissons                 — list boissons
//   - proposer_vente_complementaire   — propose available sides/drinks for a plat
//   - recuperer_infos_client          — collect nom, prénom, téléphone, lieu (REQUIRED before order)
//
// Enhanced tools:
//   - obtenir_infos_boutique          — include payment config (mode, delay, methods)
//   - generer_recapitulatif           — include options + "Offert" for 0 FCFA
package ai

import (
        "context"
        "encoding/json"
        "errors"
        "fmt"

        "github.com/google/uuid"

        "nova-api/internal/repository"
)

// ============================================================================
// Tool registration — add the new tools to the registry's Definitions()
// ============================================================================

// v3ToolDefs returns the additional tool definitions for NOVA v3.
// Called by ToolRegistry.Definitions() to append the new tools.
func v3ToolDefs() []ToolDef {
        return []ToolDef{
                mkTool("lister_plats",
                        "Liste les plats (plats principaux) disponibles dans la boutique. Retourne nom, prix (ou 'Offert' si 0), disponibilité.",
                        json.RawMessage(`{"type":"object","properties":{}}`)),
                mkTool("lister_accompagnements",
                        "Liste les accompagnements disponibles (ketchup, mayonnaise, frites...). Retourne nom, prix (ou 'Offert' si 0), disponibilité.",
                        json.RawMessage(`{"type":"object","properties":{}}`)),
                mkTool("lister_boissons",
                        "Liste les boissons disponibles (Coca, eau, jus, Fanta...). Retourne nom, prix (ou 'Offert' si 0), disponibilité.",
                        json.RawMessage(`{"type":"object","properties":{}}`)),
                mkTool("proposer_vente_complementaire",
                        "Propose des ventes complémentaires (accompagnements + boissons) disponibles pour un plat. À utiliser UNIQUEMENT pour les produits alimentaires.",
                        json.RawMessage(`{
                                "type":"object",
                                "properties":{
                                        "plat_id":{"type":"string","description":"UUID du produit (plat) pour lequel proposer des options."}
                                },
                                "required":["plat_id"]
                        }`)),
                mkTool("recuperer_infos_client",
                        "Collecte les informations client (nom, prénom, téléphone, lieu de livraison). À appeler AVANT de confirmer une commande. Met à jour le profil client.",
                        json.RawMessage(`{
                                "type":"object",
                                "properties":{
                                        "nom":{"type":"string","description":"Nom de famille du client."},
                                        "prenom":{"type":"string","description":"Prénom du client (optionnel)."},
                                        "telephone":{"type":"string","description":"Numéro de téléphone du client."},
                                        "lieu":{"type":"string","description":"Lieu de livraison (quartier, commune, adresse)."}
                                },
                                "required":["nom","telephone","lieu"]
                        }`)),
        }
}

// ============================================================================
// Tools — lister_plats / lister_accompagnements / lister_boissons
// ============================================================================

func (r *ToolRegistry) toolListerPlats(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
        return r.listOptionsByType(ctx, "plat")
}

func (r *ToolRegistry) toolListerAccompagnements(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
        return r.listOptionsByType(ctx, "accompagnement")
}

func (r *ToolRegistry) toolListerBoissons(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
        return r.listOptionsByType(ctx, "boisson")
}

// listOptionsByType is the shared implementation for the three lister_* tools.
func (r *ToolRegistry) listOptionsByType(ctx context.Context, optType string) (json.RawMessage, error) {
        if r.productOptionRepo == nil {
                return errJSON("lister_"+optType+"s", errors.New("options non configurées pour cette boutique")), nil
        }
        opts, err := r.productOptionRepo.ListByShop(ctx, r.shopID, r.userID, r.userRole, optType, true)
        if err != nil {
                return errJSON("lister_"+optType+"s", err), nil
        }
        type item struct {
                ID         string `json:"id"`
                Nom        string `json:"nom"`
                Prix       int64  `json:"prix"`
                PrixLabel  string `json:"prix_label"`
                StockMode  string `json:"stock_mode"`
                Disponible bool   `json:"disponible"`
        }
        items := make([]item, 0, len(opts))
        for i := range opts {
                o := &opts[i]
                available := o.Active
                if o.StockMode == "epuise" {
                        available = false
                }
                items = append(items, item{
                        ID:         o.ID.String(),
                        Nom:        o.Name,
                        Prix:       o.Price,
                        PrixLabel:  modelsPriceLabel(o.Price),
                        StockMode:  string(o.StockMode),
                        Disponible: available,
                })
        }
        out := struct {
                Type  string `json:"type"`
                Items []item `json:"items"`
                Total int    `json:"total"`
        }{
                Type:  optType,
                Items: items,
                Total: len(items),
        }
        b, _ := json.Marshal(out)
        return b, nil
}

// ============================================================================
// Tool — proposer_vente_complementaire
// ============================================================================

func (r *ToolRegistry) toolProposerVenteComplementaire(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
        if r.productOptionRepo == nil {
                return errJSON("proposer_vente_complementaire", errors.New("options non configurées pour cette boutique")), nil
        }
        var in struct {
                PlatID string `json:"plat_id"`
        }
        _ = json.Unmarshal(args, &in)
        platID, err := uuid.Parse(in.PlatID)
        if err != nil {
                return errJSON("proposer_vente_complementaire", fmt.Errorf("plat_id invalide: %s", in.PlatID)), nil
        }
        // Fetch options attached to the product (plat).
        attached, err := r.productOptionRepo.ListByProduct(ctx, r.shopID, r.userID, r.userRole, platID)
        if err != nil {
                return errJSON("proposer_vente_complementaire", err), nil
        }
        // Also list all shop-level accompagnements + boissons (they may not be
        // explicitly attached to the plat but are still available).
        accs, _ := r.productOptionRepo.ListByShop(ctx, r.shopID, r.userID, r.userRole, "accompagnement", true)
        boissons, _ := r.productOptionRepo.ListByShop(ctx, r.shopID, r.userID, r.userRole, "boisson", true)
        type optItem struct {
                ID         string `json:"id"`
                Nom        string `json:"nom"`
                Prix       int64  `json:"prix"`
                PrixLabel  string `json:"prix_label"`
                StockMode  string `json:"stock_mode"`
                Disponible bool   `json:"disponible"`
        }
        convOpt := func(id, name string, price int64, stockMode string, active bool) optItem {
                available := active
                if stockMode == "epuise" {
                        available = false
                }
                return optItem{
                        ID:         id,
                        Nom:        name,
                        Prix:       price,
                        PrixLabel:  modelsPriceLabel(price),
                        StockMode:  stockMode,
                        Disponible: available,
                }
        }
        attachedItems := make([]optItem, 0, len(attached))
        for i := range attached {
                o := &attached[i]
                attachedItems = append(attachedItems, convOpt(o.ID.String(), o.Name, o.Price, string(o.StockMode), o.Active))
        }
        accsItems := make([]optItem, 0, len(accs))
        for i := range accs {
                o := &accs[i]
                accsItems = append(accsItems, convOpt(o.ID.String(), o.Name, o.Price, string(o.StockMode), o.Active))
        }
        boissonsItems := make([]optItem, 0, len(boissons))
        for i := range boissons {
                o := &boissons[i]
                boissonsItems = append(boissonsItems, convOpt(o.ID.String(), o.Name, o.Price, string(o.StockMode), o.Active))
        }
        out := struct {
                PlatID           string    `json:"plat_id"`
                OptionsAttachees []optItem `json:"options_attachees"`
                Accompagnements  []optItem `json:"accompagnements"`
                Boissons         []optItem `json:"boissons"`
        }{
                PlatID:           platID.String(),
                OptionsAttachees: attachedItems,
                Accompagnements:  accsItems,
                Boissons:         boissonsItems,
        }
        b, _ := json.Marshal(out)
        return b, nil
}

// ============================================================================
// Tool — recuperer_infos_client
// ============================================================================

func (r *ToolRegistry) toolRecupererInfosClient(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
        var in struct {
                Nom       string `json:"nom"`
                Prenom    string `json:"prenom"`
                Telephone string `json:"telephone"`
                Lieu      string `json:"lieu"`
        }
        _ = json.Unmarshal(args, &in)
        if in.Nom == "" || in.Telephone == "" || in.Lieu == "" {
                return errJSON("recuperer_infos_client", errors.New("nom, telephone et lieu sont requis")), nil
        }
        // Update the customer's name (best-effort).
        fullName := in.Nom
        if in.Prenom != "" {
                fullName = in.Prenom + " " + in.Nom
        }
        // r.customerRepo may be nil in degraded mode — guard.
        if r.customerRepo == nil {
                out := struct {
                        Ok         bool   `json:"ok"`
                        Nom        string `json:"nom"`
                        Telephone  string `json:"telephone"`
                        Lieu       string `json:"lieu"`
                        CustomerID string `json:"customer_id"`
                }{
                        Ok:         true,
                        Nom:        fullName,
                        Telephone:  in.Telephone,
                        Lieu:       in.Lieu,
                        CustomerID: r.customerID.String(),
                }
                b, _ := json.Marshal(out)
                return b, nil
        }
        // Persist the name on the customer.
        _, _ = r.customerRepo.Update(ctx, r.shopID, r.customerID, repositoryUpdateCustomerName(fullName))
        out := struct {
                Ok         bool   `json:"ok"`
                Nom        string `json:"nom"`
                Telephone  string `json:"telephone"`
                Lieu       string `json:"lieu"`
                CustomerID string `json:"customer_id"`
        }{
                Ok:         true,
                Nom:        fullName,
                Telephone:  in.Telephone,
                Lieu:       in.Lieu,
                CustomerID: r.customerID.String(),
        }
        b, _ := json.Marshal(out)
        return b, nil
}

// ============================================================================
// Helpers
// ============================================================================

// modelsPriceLabel returns "Offert" for 0, "<formatted> FCFA" otherwise.
// Mirrors models.FormatPriceLabel without importing the models package
// (which would create a cycle).
func modelsPriceLabel(price int64) string {
        if price == 0 {
                return "Offert"
        }
        return formatFCFAInline(price)
}

// formatFCFAInline formats a FCFA amount with non-breaking space thousand separators.
func formatFCFAInline(n int64) string {
        if n < 0 {
                return "-" + formatFCFAInline(-n)
        }
        s := []byte{}
        str := fmtIntInline(n)
        nb := len(str)
        for i := 0; i < nb; i++ {
                if i > 0 && (nb-i)%3 == 0 {
                        s = append(s, '\u00A0')
                }
                s = append(s, str[i])
        }
        s = append(s, []byte(" FCFA")...)
        return string(s)
}

// fmtIntInline is a tiny itoa helper.
func fmtIntInline(n int64) string {
        if n == 0 {
                return "0"
        }
        var buf [20]byte
        i := len(buf)
        for n > 0 {
                i--
                buf[i] = byte('0' + n%10)
                n /= 10
        }
        return string(buf[i:])
}

// repositoryUpdateCustomerName builds the repository.UpdateCustomerRequest
// for the customer name update.
func repositoryUpdateCustomerName(name string) repository.UpdateCustomerRequest {
        n := name
        return repository.UpdateCustomerRequest{
                Name: &n,
        }
}
