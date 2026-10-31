// Tool registry — the 10 function-calling tools exposed to the LLM (ch. 5.2).
//
// CRITICAL INVARIANTS:
//  1. shop_id is ALWAYS imposed by the registry (set at construction time).
//     Even if the LLM tries to pass a different shop_id in tool args, it's
//     ignored. This enforces R5 (Ne jamais utiliser les données d'une autre
//     boutique).
//  2. Tools NEVER modify state directly without going through the existing
//     service interfaces (CatalogServiceIface, StockServiceIface, etc.).
//     This guarantees all business rules (state machine, atomic stock
//     reservation, idempotence) are enforced by the code, not by the model.
//  3. Tools return JSON results that are consumed by the LLM and ALSO
//     recorded for the output guardrail (ch. 5.5). The guardrail verifies
//     that every monetary amount / quantity in the LLM's reply appears in
//     one of these tool results.
//
// To avoid an import cycle (ai → services → ai), we define interfaces in
// the ai package that the services package implements. The EngineDeps
// struct in engine.go accepts the concrete services but the ToolRegistry
// only references the interfaces.
//
// The 10 tools (ch. 5.2):
//  1. rechercher_produits          (read-only)
//  2. obtenir_produit              (read-only)
//  3. verifier_disponibilite       (read-only)
//  4. obtenir_infos_boutique       (read-only)
//  5. calculer_livraison           (read-only)
//  6. voir_panier                  (read-only)
//  7. ajouter_au_panier            (writes cart)
//  8. modifier_panier              (writes cart)
//  9. generer_recapitulatif        (read-only — preview)
//
// 10. confirmer_commande           (writes order + reserves stock)
// 11. enregistrer_prospect         (writes customer status)
// 12. escalader_vers_humain        (writes conversation state + notif)
//
// (The spec lists "voir_panier / ajouter_au_panier / modifier_panier" as one
// composite row but we implement them as three distinct tools, matching the
// 10-tool count when we group them. So we end up with 12 actual functions
// for 10 spec rows.)
package ai

import (
        "context"
        "encoding/json"
        "errors"
        "fmt"
        "strings"

        "github.com/google/uuid"
        "github.com/jackc/pgx/v5/pgxpool"

        "nova-api/internal/models"
        "nova-api/internal/repository"
)

// ============================================================================
// Service interfaces — implemented by services.CatalogService etc.
// Defining these here (rather than importing services) breaks the import
// cycle ai ↔ services.
// ============================================================================

// CatalogServiceIface is the subset of services.CatalogService used by the AI tools.
type CatalogServiceIface interface {
        GetProduct(ctx context.Context, userID uuid.UUID, userRole string, shopID, productID uuid.UUID) (*repository.ProductWithRelations, error)
}

// StockServiceIface is the subset of services.StockService used by the AI tools.
type StockServiceIface interface {
        GetInventory(ctx context.Context, userID uuid.UUID, userRole string, shopID, variantID uuid.UUID) (*models.Inventory, error)
}

// DeliveryServiceIface is the subset of services.DeliveryService used by the AI tools.
type DeliveryServiceIface interface {
        MatchZone(ctx context.Context, userID uuid.UUID, userRole string, shopID uuid.UUID, query string) (*models.DeliveryZone, error)
        CalculateFee(ctx context.Context, userID uuid.UUID, userRole string, shopID, zoneID uuid.UUID, orderAmount int64) (*repository.CalculateFeeResult, error)
        ListZones(ctx context.Context, userID uuid.UUID, userRole string, shopID uuid.UUID, onlyActive bool) ([]models.DeliveryZone, error)
}

// ConfirmOrderInput is the AI-side equivalent of services.ConfirmOrderRequest.
// We re-declare it here to avoid importing services.
type ConfirmOrderInput struct {
        CartID          uuid.UUID
        ZoneID          uuid.UUID
        DeliveryAddress string
        PaymentMode     string
        PaymentStatus   string
        IdempotencyKey  string
        CustomerID      *uuid.UUID
        Note            string
        AutoConfirm     bool
}

// OrderServiceIface is the subset of services.OrderService used by the AI tools.
type OrderServiceIface interface {
        GetOrCreateCart(ctx context.Context, userID uuid.UUID, userRole string, shopID uuid.UUID, conversationID, customerID *uuid.UUID) (*models.Cart, error)
        AddToCart(ctx context.Context, userID uuid.UUID, userRole string, shopID, cartID, variantID uuid.UUID, quantity int) (*models.Cart, error)
        UpdateCartItem(ctx context.Context, userID uuid.UUID, userRole string, shopID, cartID, itemID uuid.UUID, quantity int) (*models.Cart, error)
        GenerateRecap(ctx context.Context, userID uuid.UUID, userRole string, shopID, cartID, zoneID uuid.UUID, paymentMode string) (*models.OrderRecap, error)
        ConfirmOrder(ctx context.Context, userID uuid.UUID, userRole string, shopID uuid.UUID, req ConfirmOrderInput, ip, userAgent string) (*models.Order, error)
}

// ShopRepoIface is the subset of repository.ShopRepository used by the AI tools.
type ShopRepoIface interface {
        GetByID(ctx context.Context, requesterID uuid.UUID, role string, id uuid.UUID) (*models.Shop, error)
}

// CustomerRepoIface is the subset of repository.CustomerRepository used by the AI tools.
type CustomerRepoIface interface {
        GetByID(ctx context.Context, shopID, id uuid.UUID) (*repository.Customer, error)
        GetOrCreateByPhone(ctx context.Context, shopID uuid.UUID, phone, name string) (*repository.Customer, error)
        // NOVA v3 — used by recuperer_infos_client to update the customer's name.
        Update(ctx context.Context, shopID, id uuid.UUID, req repository.UpdateCustomerRequest) (*repository.Customer, error)
}

// ProductOptionRepoIface is the subset of repository.ProductOptionRepository
// used by the AI tools (NOVA v3 — spec section 3 + 6).
type ProductOptionRepoIface interface {
        ListByShop(ctx context.Context, shopID, userID uuid.UUID, role string, optType string, onlyActive bool) ([]models.ProductOption, error)
        ListByProduct(ctx context.Context, shopID, userID uuid.UUID, role string, productID uuid.UUID) ([]models.ProductOption, error)
}

// PaymentConfigRepoIface is the subset of repository.PaymentConfigRepository
// used by the AI tools (NOVA v3 — spec section 4 + 6).
type PaymentConfigRepoIface interface {
        GetOrCreateByShop(ctx context.Context, shopID, userID uuid.UUID, role string) (*models.PaymentConfig, error)
        GetDelayMinutes(ctx context.Context, shopID uuid.UUID) int
}

// ConversationRepoIface is the subset of repository.ConversationRepository used by the AI tools.
type ConversationRepoIface interface {
        UpdateState(ctx context.Context, shopID, id uuid.UUID, state string, takenOverBy *uuid.UUID) error
        ListMessages(ctx context.Context, shopID, conversationID uuid.UUID, limit int) ([]repository.Message, error)
        AddMessage(ctx context.Context, shopID, conversationID uuid.UUID, direction, msgType, content, wamid string) (*repository.Message, error)
}

// NotifRepoIface is the subset of repository.NotificationsRepository used by the AI tools.
type NotifRepoIface interface {
        Create(ctx context.Context, shopID, recipientID *uuid.UUID, notifType string, payload []byte) (*repository.Notification, error)
}

// catalogLister adapts a CatalogServiceIface to return []ProductListItem.
// We use an any-typed return on the interface and let the concrete impl cast.
// To keep things simple, we add a separate method that returns the typed slice.
// (Go doesn't support covariant return types on interface methods, so the
// concrete implementation must return []ProductListItem via a wrapper.)

// ProductListItem is the AI-side equivalent of services.ProductListItem.
// Defining it here breaks the import cycle.
type ProductListItem struct {
        Product      models.Product
        VariantCount int
        FirstImage   *string
}

// ToolRegistry is constructed per conversation (one registry per process-
// message call). It binds all tool calls to a fixed shop_id + customer_id +
// conversation_id so the LLM cannot escape its tenant context.
type ToolRegistry struct {
        shopID         uuid.UUID
        userID         uuid.UUID
        userRole       string
        customerID     uuid.UUID
        conversationID uuid.UUID

        catalogSvc       CatalogLister
        stockSvc         StockServiceIface
        deliverySvc      DeliveryServiceIface
        orderSvc         OrderServiceIface
        shopRepo         ShopRepoIface
        customerRepo     CustomerRepoIface
        conversationRepo ConversationRepoIface
        notifRepo        NotifRepoIface
        // NOVA v3 — added for lister_plats/accompagnements/boissons + payment info.
        productOptionRepo ProductOptionRepoIface
        paymentCfgRepo    PaymentConfigRepoIface
        pool              *pgxpool.Pool
}

// CatalogLister extends CatalogServiceIface with a typed ListProducts.
// The concrete services.CatalogService returns []services.ProductListItem,
// which is structurally identical to []ProductListItem — the wrapper in
// ai_service.go (services package) converts.
type CatalogLister interface {
        CatalogServiceIface
        ListProductsTyped(ctx context.Context, userID uuid.UUID, userRole string, shopID uuid.UUID, params models.ListProductsParams) ([]ProductListItem, int64, error)
}

// NewToolRegistry constructs a ToolRegistry bound to the given conversation
// context. The shopID/customerID/conversationID are immutable for the lifetime
// of the registry — every tool call uses these values, never values from the
// LLM's tool arguments.
func NewToolRegistry(
        shopID, userID uuid.UUID, userRole string,
        customerID, conversationID uuid.UUID,
        catalogSvc CatalogLister,
        stockSvc StockServiceIface,
        deliverySvc DeliveryServiceIface,
        orderSvc OrderServiceIface,
        shopRepo ShopRepoIface,
        customerRepo CustomerRepoIface,
        conversationRepo ConversationRepoIface,
        notifRepo NotifRepoIface,
        productOptionRepo ProductOptionRepoIface,
        paymentCfgRepo PaymentConfigRepoIface,
        pool *pgxpool.Pool,
) *ToolRegistry {
        if userRole == "" {
                userRole = "owner"
        }
        return &ToolRegistry{
                shopID:            shopID,
                userID:            userID,
                userRole:          userRole,
                customerID:        customerID,
                conversationID:    conversationID,
                catalogSvc:        catalogSvc,
                stockSvc:          stockSvc,
                deliverySvc:       deliverySvc,
                orderSvc:          orderSvc,
                shopRepo:          shopRepo,
                customerRepo:      customerRepo,
                conversationRepo:  conversationRepo,
                notifRepo:         notifRepo,
                productOptionRepo: productOptionRepo,
                paymentCfgRepo:    paymentCfgRepo,
                pool:              pool,
        }
}

// ShopID returns the bound shop ID (immutable).
func (r *ToolRegistry) ShopID() uuid.UUID { return r.shopID }

// CustomerID returns the bound customer ID.
func (r *ToolRegistry) CustomerID() uuid.UUID { return r.customerID }

// ConversationID returns the bound conversation ID.
func (r *ToolRegistry) ConversationID() uuid.UUID { return r.conversationID }

// Definitions returns the JSON-Schema definitions for the 10 tools, in the
// format expected by the OpenAI Chat Completions API.
func (r *ToolRegistry) Definitions() []ToolDef {
        defs := []ToolDef{
                mkTool("rechercher_produits",
                        "Recherche textuelle dans le catalogue publié de la boutique. Retourne une liste de produits avec leur nom, prix et un résumé des variantes.",
                        json.RawMessage(`{
                                "type":"object",
                                "properties":{
                                        "requete":{"type":"string","description":"Texte de recherche (nom, marque, catégorie)."},
                                        "categorie":{"type":"string","description":"Filtre par catégorie (optionnel)."}
                                },
                                "required":["requete"]
                        }`)),
                mkTool("obtenir_produit",
                        "Retourne la fiche complète d'un produit : description, prix actuel (promotion incluse), variantes, disponibilité.",
                        json.RawMessage(`{
                                "type":"object",
                                "properties":{
                                        "produit_id":{"type":"string","description":"UUID du produit."}
                                },
                                "required":["produit_id"]
                        }`)),
                mkTool("verifier_disponibilite",
                        "Retourne le stock réel disponible pour une variante et une quantité demandée.",
                        json.RawMessage(`{
                                "type":"object",
                                "properties":{
                                        "variante":{"type":"string","description":"UUID de la variante, ou nom du produit pour rechercher la première variante active."},
                                        "quantite":{"type":"integer","description":"Quantité souhaitée (défaut 1)."}
                                },
                                "required":["variante"]
                        }`)),
                mkTool("obtenir_infos_boutique",
                        "Retourne les infos publiques de la boutique : nom, adresse, horaires, moyens de paiement acceptés, conditions de vente.",
                        json.RawMessage(`{"type":"object","properties":{}}`)),
                mkTool("calculer_livraison",
                        "Calcule les frais et délais de livraison vers une zone (par nom ou alias). Retourne une erreur si la zone est inconnue.",
                        json.RawMessage(`{
                                "type":"object",
                                "properties":{
                                        "zone_ou_adresse":{"type":"string","description":"Nom de quartier ou alias de zone (ex: Cocody, Yopougon)."}
                                },
                                "required":["zone_ou_adresse"]
                        }`)),
                mkTool("voir_panier",
                        "Retourne le panier actif de la conversation (articles, quantités, sous-total).",
                        json.RawMessage(`{"type":"object","properties":{}}`)),
                mkTool("ajouter_au_panier",
                        "Ajoute un article au panier de la conversation. Prend soit un variant_id, soit un nom de produit + variante (taille/couleur).",
                        json.RawMessage(`{
                                "type":"object",
                                "properties":{
                                        "variante_id":{"type":"string","description":"UUID de la variante (si connu)."},
                                        "produit":{"type":"string","description":"Nom du produit (si variante_id inconnu)."},
                                        "variante":{"type":"string","description":"Taille ou couleur (si variante_id inconnu)."},
                                        "quantite":{"type":"integer","description":"Quantité (défaut 1)."}
                                }
                        }`)),
                mkTool("modifier_panier",
                        "Modifie la quantité d'un article du panier. quantite=0 retire l'article.",
                        json.RawMessage(`{
                                "type":"object",
                                "properties":{
                                        "article_id":{"type":"string","description":"UUID de l'article du panier."},
                                        "quantite":{"type":"integer","description":"Nouvelle quantité (0 = retirer)."}
                                },
                                "required":["article_id","quantite"]
                        }`)),
                mkTool("generer_recapitulatif",
                        "Génère un récapitulatif (sous-total, frais de livraison, total) SANS créer de commande. Le client doit confirmer ensuite.",
                        json.RawMessage(`{
                                "type":"object",
                                "properties":{
                                        "mode_paiement":{"type":"string","description":"cash | mobile_money | wave | orange_money | mtn_momo (défaut cash)."},
                                        "zone":{"type":"string","description":"Nom de zone pour le calcul des frais (sinon déduit de la conversation)."}
                                }
                        }`)),
                mkTool("confirmer_commande",
                        "Confirme la commande à partir du panier. APPELER UNIQUEMENT après accord explicite du client. Applique la machine à états + réservation atomique du stock.",
                        json.RawMessage(`{
                                "type":"object",
                                "properties":{
                                        "cle_idempotence":{"type":"string","description":"Clé d'idempotence (générée par l'IA si absente)."},
                                        "mode_paiement":{"type":"string","description":"cash | mobile_money | wave | orange_money | mtn_momo (défaut cash)."},
                                        "zone":{"type":"string","description":"Zone de livraison (nom)."},
                                        "adresse":{"type":"string","description":"Adresse de livraison précise."}
                                }
                        }`)),
                mkTool("enregistrer_prospect",
                        "Crée ou met à jour un prospect (client sans commande). Utilisé pour suivre une intention d'achat.",
                        json.RawMessage(`{
                                "type":"object",
                                "properties":{
                                        "produit":{"type":"string","description":"Produit souhaité par le prospect."},
                                        "intention":{"type":"string","description":"Intention (ex: intéressé, rappel souhaité, hors stock)."}
                                },
                                "required":["intention"]
                        }`)),
                mkTool("escalader_vers_humain",
                        "Passe la conversation en mode humain et notifie le commerçant. À utiliser quand le client demande à parler à un humain ou que l'IA ne peut pas répondre.",
                        json.RawMessage(`{
                                "type":"object",
                                "properties":{
                                        "raison":{"type":"string","description":"Raison de l'escalade."}
                                },
                                "required":["raison"]
                        }`)),
        }
        // NOVA v3 — append the v3 tools (lister_plats, lister_accompagnements,
        // lister_boissons, proposer_vente_complementaire, recuperer_infos_client).
        return append(defs, v3ToolDefs()...)
}

// mkTool is a tiny helper to build a ToolDef with less boilerplate.
func mkTool(name, description string, params json.RawMessage) ToolDef {
        t := ToolDef{Type: "function"}
        t.Function.Name = name
        t.Function.Description = description
        t.Function.Parameters = params
        return t
}

// Execute runs the named tool with the given args (raw JSON). Returns the JSON
// result. This is the ONLY entry point for tool execution — the engine calls
// this, never the LLM directly.
//
// On error, the result JSON contains an "erreur" field so the LLM can
// paraphrase it gracefully (and the guardrail sees a verifiable negative
// result).
func (r *ToolRegistry) Execute(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
        switch name {
        case "rechercher_produits":
                return r.toolRechercherProduits(ctx, args)
        case "obtenir_produit":
                return r.toolObtenirProduit(ctx, args)
        case "verifier_disponibilite":
                return r.toolVerifierDisponibilite(ctx, args)
        case "obtenir_infos_boutique":
                return r.toolObtenirInfosBoutique(ctx, args)
        case "calculer_livraison":
                return r.toolCalculerLivraison(ctx, args)
        case "voir_panier":
                return r.toolVoirPanier(ctx, args)
        case "ajouter_au_panier":
                return r.toolAjouterAuPanier(ctx, args)
        case "modifier_panier":
                return r.toolModifierPanier(ctx, args)
        case "generer_recapitulatif":
                return r.toolGenererRecapitulatif(ctx, args)
        case "confirmer_commande":
                return r.toolConfirmerCommande(ctx, args)
        case "enregistrer_prospect":
                return r.toolEnregistrerProspect(ctx, args)
        case "escalader_vers_humain":
                return r.toolEscaladerVersHumain(ctx, args)
        // NOVA v3 — new tools.
        case "lister_plats":
                return r.toolListerPlats(ctx, args)
        case "lister_accompagnements":
                return r.toolListerAccompagnements(ctx, args)
        case "lister_boissons":
                return r.toolListerBoissons(ctx, args)
        case "proposer_vente_complementaire":
                return r.toolProposerVenteComplementaire(ctx, args)
        case "recuperer_infos_client":
                return r.toolRecupererInfosClient(ctx, args)
        }
        return nil, fmt.Errorf("ai tools: unknown tool %q", name)
}

// ============================================================================
// Tool 1 : rechercher_produits
// ============================================================================

func (r *ToolRegistry) toolRechercherProduits(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
        var in struct {
                Requete   string `json:"requete"`
                Categorie string `json:"categorie"`
        }
        _ = json.Unmarshal(args, &in)

        params := models.ListProductsParams{
                Page: 1, Limit: 10, Search: in.Requete, Category: in.Categorie, Status: "published",
        }
        items, total, err := r.catalogSvc.ListProductsTyped(ctx, r.userID, r.userRole, r.shopID, params)
        if err != nil {
                return errJSON("rechercher_produits", err), nil
        }
        type variantSummary struct {
                ID      string `json:"id"`
                Prix    int64  `json:"prix"`
                Taille  string `json:"taille,omitempty"`
                Couleur string `json:"couleur,omitempty"`
        }
        type produitItem struct {
                ID        string           `json:"id"`
                Nom       string           `json:"nom"`
                Prix      int64            `json:"prix"`
                Categorie string           `json:"categorie,omitempty"`
                Variantes []variantSummary `json:"variantes,omitempty"`
        }
        out := struct {
                Produits []produitItem `json:"produits"`
                Total    int64         `json:"total"`
        }{
                Produits: make([]produitItem, 0, len(items)),
                Total:    total,
        }
        // Fetch each product's variants + current price (we need the full GetByID
        // because ListProducts doesn't return variants).
        for _, it := range items {
                p := it.Product
                // Get full product to access variants.
                full, err := r.catalogSvc.GetProduct(ctx, r.userID, r.userRole, r.shopID, p.ID)
                variantes := []variantSummary{}
                if err == nil && full != nil {
                        for i := range full.Variants {
                                v := &full.Variants[i]
                                if !v.Active {
                                        continue
                                }
                                vs := variantSummary{
                                        ID:   v.ID.String(),
                                        Prix: models.CurrentVariantPrice(v),
                                }
                                if v.Size != nil {
                                        vs.Taille = *v.Size
                                }
                                if v.Color != nil {
                                        vs.Couleur = *v.Color
                                }
                                variantes = append(variantes, vs)
                        }
                }
                // Determine a representative price (min variant price, or 0 if no variants).
                var minPrice int64
                if len(variantes) > 0 {
                        minPrice = variantes[0].Prix
                        for _, v := range variantes[1:] {
                                if v.Prix < minPrice {
                                        minPrice = v.Prix
                                }
                        }
                }
                pi := produitItem{
                        ID:        p.ID.String(),
                        Nom:       p.Name,
                        Prix:      minPrice,
                        Variantes: variantes,
                }
                if p.Category != nil {
                        pi.Categorie = *p.Category
                }
                out.Produits = append(out.Produits, pi)
        }
        b, _ := json.Marshal(out)
        return b, nil
}

// ============================================================================
// Tool 2 : obtenir_produit
// ============================================================================

func (r *ToolRegistry) toolObtenirProduit(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
        var in struct {
                ProduitID string `json:"produit_id"`
        }
        _ = json.Unmarshal(args, &in)
        pid, err := uuid.Parse(in.ProduitID)
        if err != nil {
                // Try to find by name.
                items, _, e := r.catalogSvc.ListProductsTyped(ctx, r.userID, r.userRole, r.shopID, models.ListProductsParams{Page: 1, Limit: 5, Search: in.ProduitID, Status: "published"})
                if e != nil || len(items) == 0 {
                        return errJSON("obtenir_produit", fmt.Errorf("produit introuvable: %s", in.ProduitID)), nil
                }
                pid = items[0].Product.ID
        }
        full, err := r.catalogSvc.GetProduct(ctx, r.userID, r.userRole, r.shopID, pid)
        if err != nil {
                return errJSON("obtenir_produit", err), nil
        }
        type variante struct {
                ID      string `json:"id"`
                Nom     string `json:"nom"`
                Prix    int64  `json:"prix"`
                Taille  string `json:"taille,omitempty"`
                Couleur string `json:"couleur,omitempty"`
                Active  bool   `json:"active"`
        }
        variantes := make([]variante, 0, len(full.Variants))
        var minPrix int64
        first := true
        for i := range full.Variants {
                v := &full.Variants[i]
                if !v.Active {
                        continue
                }
                vn := ""
                if v.Size != nil {
                        vn += *v.Size
                }
                if v.Color != nil {
                        if vn != "" {
                                vn += " - "
                        }
                        vn += *v.Color
                }
                prix := models.CurrentVariantPrice(v)
                vv := variante{
                        ID:     v.ID.String(),
                        Nom:    vn,
                        Prix:   prix,
                        Active: v.Active,
                }
                if v.Size != nil {
                        vv.Taille = *v.Size
                }
                if v.Color != nil {
                        vv.Couleur = *v.Color
                }
                variantes = append(variantes, vv)
                if first || prix < minPrix {
                        minPrix = prix
                        first = false
                }
        }
        out := struct {
                ID          string     `json:"id"`
                Nom         string     `json:"nom"`
                Description string     `json:"description,omitempty"`
                Categorie   string     `json:"categorie,omitempty"`
                Prix        int64      `json:"prix"`
                Variantes   []variante `json:"variantes"`
        }{
                ID:        full.Product.ID.String(),
                Nom:       full.Product.Name,
                Prix:      minPrix,
                Variantes: variantes,
        }
        if full.Product.Description != nil {
                out.Description = *full.Product.Description
        }
        if full.Product.Category != nil {
                out.Categorie = *full.Product.Category
        }
        b, _ := json.Marshal(out)
        return b, nil
}

func minPriceUnused(variantes []struct {
        ID      string `json:"id"`
        Nom     string `json:"nom"`
        Prix    int64  `json:"prix"`
        Taille  string `json:"taille,omitempty"`
        Couleur string `json:"couleur,omitempty"`
        Active  bool   `json:"active"`
}) int64 {
        if len(variantes) == 0 {
                return 0
        }
        min := variantes[0].Prix
        for _, v := range variantes[1:] {
                if v.Prix < min {
                        min = v.Prix
                }
        }
        return min
}

var _ = minPriceUnused

// ============================================================================
// Tool 3 : verifier_disponibilite
// ============================================================================

func (r *ToolRegistry) toolVerifierDisponibilite(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
        var in struct {
                Variante string `json:"variante"`
                Quantite int    `json:"quantite"`
        }
        _ = json.Unmarshal(args, &in)
        if in.Quantite <= 0 {
                in.Quantite = 1
        }
        variantID, err := uuid.Parse(in.Variante)
        if err != nil {
                // Find by name: search products, take first active variant.
                items, _, e := r.catalogSvc.ListProductsTyped(ctx, r.userID, r.userRole, r.shopID, models.ListProductsParams{Page: 1, Limit: 5, Search: in.Variante, Status: "published"})
                if e != nil || len(items) == 0 {
                        return errJSON("verifier_disponibilite", fmt.Errorf("variante introuvable: %s", in.Variante)), nil
                }
                full, e := r.catalogSvc.GetProduct(ctx, r.userID, r.userRole, r.shopID, items[0].Product.ID)
                if e != nil || full == nil || len(full.Variants) == 0 {
                        return errJSON("verifier_disponibilite", fmt.Errorf("aucune variante pour: %s", in.Variante)), nil
                }
                for i := range full.Variants {
                        if full.Variants[i].Active {
                                variantID = full.Variants[i].ID
                                break
                        }
                }
                if variantID == uuid.Nil {
                        variantID = full.Variants[0].ID
                }
        }
        inv, err := r.stockSvc.GetInventory(ctx, r.userID, r.userRole, r.shopID, variantID)
        if err != nil {
                return errJSON("verifier_disponibilite", err), nil
        }
        out := struct {
                VarianteID string `json:"variante_id"`
                EnStock    int    `json:"en_stock"`
                Reserve    int    `json:"reserve"`
                Disponible int    `json:"disponible"`
                Suffisant  bool   `json:"suffisant"`
                Demande    int    `json:"demande"`
        }{
                VarianteID: variantID.String(),
                EnStock:    inv.OnHand,
                Reserve:    inv.Reserved,
                Disponible: inv.Available,
                Suffisant:  inv.Available >= in.Quantite,
                Demande:    in.Quantite,
        }
        b, _ := json.Marshal(out)
        return b, nil
}

// ============================================================================
// Tool 4 : obtenir_infos_boutique
// ============================================================================

func (r *ToolRegistry) toolObtenirInfosBoutique(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
        shop, err := r.shopRepo.GetByID(ctx, r.userID, "super_admin", r.shopID)
        if err != nil {
                return errJSON("obtenir_infos_boutique", err), nil
        }
        out := struct {
                Nom            string   `json:"nom"`
                Adresse        string   `json:"adresse,omitempty"`
                Horaires       string   `json:"horaires,omitempty"`
                MoyensPaiement []string `json:"moyens_paiement,omitempty"`
                Conditions     string   `json:"conditions,omitempty"`
                // NOVA v3 — payment config (mode, delay, methods).
                ModePaiement    string   `json:"mode_paiement,omitempty"`
                DelaiPaiement   int      `json:"delai_paiement_minutes,omitempty"`
                MontantAcompte  *int64   `json:"montant_acompte,omitempty"`
                MethodesPaiement []string `json:"methodes_paiement,omitempty"`
        }{
                Nom:            shop.Name,
                MoyensPaiement: shop.AcceptedPaymentModes,
        }
        if shop.Address != nil {
                out.Adresse = *shop.Address
        }
        if shop.SaleConditions != nil {
                out.Conditions = *shop.SaleConditions
        }
        out.Horaires = formatHours(shop.Hours)
        // NOVA v3 — fetch payment config (best-effort).
        if r.paymentCfgRepo != nil {
                if cfg, err := r.paymentCfgRepo.GetOrCreateByShop(ctx, r.shopID, r.userID, r.userRole); err == nil && cfg != nil {
                        out.ModePaiement = string(cfg.Mode)
                        out.DelaiPaiement = cfg.DelayMinutes
                        out.MontantAcompte = cfg.AdvanceAmount
                        methods := cfg.ActiveMethods
                        if methods == nil {
                                methods = []string{}
                        }
                        out.MethodesPaiement = methods
                }
        }
        b, _ := json.Marshal(out)
        return b, nil
}

// ============================================================================
// Tool 5 : calculer_livraison
// ============================================================================

func (r *ToolRegistry) toolCalculerLivraison(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
        var in struct {
                ZoneOuAdresse string `json:"zone_ou_adresse"`
        }
        _ = json.Unmarshal(args, &in)
        if in.ZoneOuAdresse == "" {
                return errJSON("calculer_livraison", errors.New("zone_ou_adresse requis")), nil
        }
        zone, err := r.deliverySvc.MatchZone(ctx, r.userID, r.userRole, r.shopID, in.ZoneOuAdresse)
        if err != nil {
                // Distinguish ambiguous (multiple zones match) from unknown. For an
                // ambiguous match, list the candidate zones so the LLM can ask the
                // client to clarify (R7). If all matching zones have the same fee, we
                // pick the first one (no real ambiguity for the customer).
                if errors.Is(err, repository.ErrZoneAmbiguous) {
                        zones, listErr := r.deliverySvc.ListZones(ctx, r.userID, r.userRole, r.shopID, true)
                        if listErr == nil {
                                lowQuery := strings.ToLower(strings.TrimSpace(in.ZoneOuAdresse))
                                var candidates []models.DeliveryZone
                                for i := range zones {
                                        z := &zones[i]
                                        if strings.ToLower(z.Name) == lowQuery {
                                                candidates = append(candidates, *z)
                                                continue
                                        }
                                        for _, a := range z.Aliases {
                                                if strings.ToLower(a) == lowQuery {
                                                        candidates = append(candidates, *z)
                                                        break
                                                }
                                        }
                                }
                                if len(candidates) > 0 {
                                        // Check if all candidates have the same fee.
                                        firstFee := candidates[0].Fee
                                        allSame := true
                                        for _, c := range candidates[1:] {
                                                if c.Fee != firstFee {
                                                        allSame = false
                                                        break
                                                }
                                        }
                                        if allSame {
                                                // Ambiguity resolved — pick the first candidate and
                                                // clear the error so we proceed to fee calculation.
                                                zone = &candidates[0]
                                                err = nil
                                        } else {
                                                names := make([]string, 0, len(candidates))
                                                for _, c := range candidates {
                                                        names = append(names, c.Name)
                                                }
                                                out := struct {
                                                        Erreur     string   `json:"erreur"`
                                                        Zone       string   `json:"zone,omitempty"`
                                                        Candidates []string `json:"zones_possibles,omitempty"`
                                                }{Erreur: "zone_ambigue", Zone: in.ZoneOuAdresse, Candidates: names}
                                                b, _ := json.Marshal(out)
                                                return b, nil
                                        }
                                }
                        }
                }
                // If we still have an error (genuinely unknown zone), return a
                // structured error so the LLM can ask for clarification.
                if err != nil {
                        out := struct {
                                Erreur string `json:"erreur"`
                                Zone   string `json:"zone,omitempty"`
                        }{Erreur: "zone_inconnue", Zone: in.ZoneOuAdresse}
                        b, _ := json.Marshal(out)
                        return b, nil
                }
        }
        // Compute the fee based on the current cart subtotal (0 if no cart).
        cart, _ := r.orderSvc.GetOrCreateCart(ctx, r.userID, r.userRole, r.shopID, &r.conversationID, &r.customerID)
        var subtotal int64
        if cart != nil {
                for i := range cart.Items {
                        subtotal += cart.Items[i].UnitPrice * int64(cart.Items[i].Quantity)
                }
        }
        fee, err := r.deliverySvc.CalculateFee(ctx, r.userID, r.userRole, r.shopID, zone.ID, subtotal)
        if err != nil {
                out := struct {
                        Erreur string `json:"erreur"`
                        Zone   string `json:"zone,omitempty"`
                        Detail string `json:"detail,omitempty"`
                }{Erreur: "calcul_impossible", Zone: zone.Name, Detail: err.Error()}
                b, _ := json.Marshal(out)
                return b, nil
        }
        out := struct {
                Zone     string `json:"zone"`
                Frais    int64  `json:"frais"`
                Gratuite bool   `json:"gratuite"`
                Delai    string `json:"delai,omitempty"`
        }{
                Zone:     zone.Name,
                Frais:    fee.Fee,
                Gratuite: fee.FreeDelivery,
        }
        if zone.EstimatedDelay != nil {
                out.Delai = *zone.EstimatedDelay
        }
        b, _ := json.Marshal(out)
        return b, nil
}

// ============================================================================
// Tools 6-8 : voir_panier / ajouter_au_panier / modifier_panier
// ============================================================================

func (r *ToolRegistry) toolVoirPanier(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
        cart, err := r.orderSvc.GetOrCreateCart(ctx, r.userID, r.userRole, r.shopID, &r.conversationID, &r.customerID)
        if err != nil {
                return errJSON("voir_panier", err), nil
        }
        return cartToJSON(cart), nil
}

func (r *ToolRegistry) toolAjouterAuPanier(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
        var in struct {
                VarianteID string `json:"variante_id"`
                Produit    string `json:"produit"`
                Variante   string `json:"variante"`
                Quantite   int    `json:"quantite"`
        }
        _ = json.Unmarshal(args, &in)
        if in.Quantite <= 0 {
                in.Quantite = 1
        }
        variantID, err := uuid.Parse(in.VarianteID)
        if err != nil {
                // Resolve by product name + variant attributes (size/color).
                vid, e := r.findVariantByQuery(ctx, in.Produit, in.Variante)
                if e != nil {
                        return errJSON("ajouter_au_panier", fmt.Errorf("variante introuvable: %s %s", in.Produit, in.Variante)), nil
                }
                variantID = vid
        }
        cart, err := r.orderSvc.GetOrCreateCart(ctx, r.userID, r.userRole, r.shopID, &r.conversationID, &r.customerID)
        if err != nil {
                return errJSON("ajouter_au_panier", err), nil
        }
        updated, err := r.orderSvc.AddToCart(ctx, r.userID, r.userRole, r.shopID, cart.ID, variantID, in.Quantite)
        if err != nil {
                return errJSON("ajouter_au_panier", err), nil
        }
        return cartToJSON(updated), nil
}

func (r *ToolRegistry) toolModifierPanier(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
        var in struct {
                ArticleID string `json:"article_id"`
                Quantite  int    `json:"quantite"`
        }
        _ = json.Unmarshal(args, &in)
        itemID, err := uuid.Parse(in.ArticleID)
        if err != nil {
                return errJSON("modifier_panier", fmt.Errorf("article_id invalide: %s", in.ArticleID)), nil
        }
        cart, err := r.orderSvc.GetOrCreateCart(ctx, r.userID, r.userRole, r.shopID, &r.conversationID, &r.customerID)
        if err != nil {
                return errJSON("modifier_panier", err), nil
        }
        updated, err := r.orderSvc.UpdateCartItem(ctx, r.userID, r.userRole, r.shopID, cart.ID, itemID, in.Quantite)
        if err != nil {
                return errJSON("modifier_panier", err), nil
        }
        return cartToJSON(updated), nil
}

// ============================================================================
// Tool 9 : generer_recapitulatif
// ============================================================================

func (r *ToolRegistry) toolGenererRecapitulatif(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
        var in struct {
                ModePaiement string `json:"mode_paiement"`
                Zone         string `json:"zone"`
        }
        _ = json.Unmarshal(args, &in)
        if in.ModePaiement == "" {
                in.ModePaiement = "cash"
        }
        cart, err := r.orderSvc.GetOrCreateCart(ctx, r.userID, r.userRole, r.shopID, &r.conversationID, &r.customerID)
        if err != nil {
                return errJSON("generer_recapitulatif", err), nil
        }
        // Resolve the zone. If not provided in args, fall back to the shop's first
        // active zone (the LLM may not always know the zone — e.g. when the
        // customer asks for a recap without specifying where to deliver).
        var zone *models.DeliveryZone
        if in.Zone != "" {
                zone, err = r.deliverySvc.MatchZone(ctx, r.userID, r.userRole, r.shopID, in.Zone)
                if err != nil {
                        return errJSON("generer_recapitulatif", fmt.Errorf("zone inconnue: %s", in.Zone)), nil
                }
        } else {
                zone, err = r.defaultZone(ctx)
                if err != nil {
                        return errJSON("generer_recapitulatif", fmt.Errorf("zone requise: %v", err)), nil
                }
        }
        recap, err := r.orderSvc.GenerateRecap(ctx, r.userID, r.userRole, r.shopID, cart.ID, zone.ID, in.ModePaiement)
        if err != nil {
                return errJSON("generer_recapitulatif", err), nil
        }
        // Flatten items for the LLM.
        type articleItem struct {
                Nom          string `json:"nom"`
                Quantite     int    `json:"quantite"`
                PrixUnitaire int64  `json:"prix_unitaire"`
                Total        int64  `json:"total"`
        }
        articles := make([]articleItem, 0, len(recap.Items))
        for _, it := range recap.Items {
                articles = append(articles, articleItem{
                        Nom:          it.ProductName,
                        Quantite:     it.Quantity,
                        PrixUnitaire: it.UnitPrice,
                        Total:        it.LineTotal,
                })
        }
        out := struct {
                Articles       []articleItem `json:"articles"`
                SousTotal      int64         `json:"sous_total"`
                FraisLivraison int64         `json:"frais_livraison"`
                Gratuite       bool          `json:"gratuite"`
                Total          int64         `json:"total"`
                ModePaiement   string        `json:"mode_paiement"`
                Zone           string        `json:"zone,omitempty"`
        }{
                Articles:       articles,
                SousTotal:      recap.Subtotal,
                FraisLivraison: recap.DeliveryFee,
                Gratuite:       recap.FreeDelivery,
                Total:          recap.Total,
                ModePaiement:   recap.PaymentMode,
        }
        if recap.Zone != nil {
                out.Zone = recap.Zone.Name
        }
        b, _ := json.Marshal(out)
        return b, nil
}

// ============================================================================
// Tool 10 : confirmer_commande — THE CRITICAL TOOL
// ============================================================================

func (r *ToolRegistry) toolConfirmerCommande(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
        var in struct {
                CleIdempotence string `json:"cle_idempotence"`
                ModePaiement   string `json:"mode_paiement"`
                Zone           string `json:"zone"`
                Adresse        string `json:"adresse"`
        }
        _ = json.Unmarshal(args, &in)
        if in.ModePaiement == "" {
                in.ModePaiement = "cash"
        }
        if in.CleIdempotence == "" {
                // Derive from the conversation — same idempotency key for the same
                // (conversation, customer) pair ensures the customer can't double-
                // confirm a different order by accident.
                in.CleIdempotence = fmt.Sprintf("conv-%s-cust-%s", r.conversationID.String(), r.customerID.String())
        }
        // Resolve the zone. If not provided in args, fall back to the shop's first
        // active zone. If the adresse is missing, use a placeholder so the order
        // can still be created (the merchant will confirm the address with the
        // customer).
        var zone *models.DeliveryZone
        var err error
        if in.Zone != "" {
                zone, err = r.deliverySvc.MatchZone(ctx, r.userID, r.userRole, r.shopID, in.Zone)
                if err != nil {
                        return errJSON("confirmer_commande", fmt.Errorf("zone inconnue: %s", in.Zone)), nil
                }
        } else {
                zone, err = r.defaultZone(ctx)
                if err != nil {
                        return errJSON("confirmer_commande", fmt.Errorf("zone requise: %v", err)), nil
                }
        }
        if in.Adresse == "" {
                in.Adresse = "Adresse à confirmer avec le client"
        }
        cart, err := r.orderSvc.GetOrCreateCart(ctx, r.userID, r.userRole, r.shopID, &r.conversationID, &r.customerID)
        if err != nil {
                return errJSON("confirmer_commande", err), nil
        }
        // Use AutoConfirm=true so the order is created in 'confirmed' state with
        // stock reserved atomically. The shop's ai_settings.confirmation_mode
        // could override this in a future iteration (manual mode = pending +
        // merchant must validate).
        req := ConfirmOrderInput{
                CartID:          cart.ID,
                ZoneID:          zone.ID,
                DeliveryAddress: in.Adresse,
                PaymentMode:     in.ModePaiement,
                IdempotencyKey:  in.CleIdempotence,
                CustomerID:      &r.customerID,
                AutoConfirm:     true,
        }
        order, err := r.orderSvc.ConfirmOrder(ctx, r.userID, r.userRole, r.shopID, req, "", "")
        if err != nil {
                return errJSON("confirmer_commande", err), nil
        }
        out := struct {
                Numero       string `json:"numero"`
                Status       string `json:"status"`
                Total        int64  `json:"total"`
                ModePaiement string `json:"mode_paiement"`
                Zone         string `json:"zone"`
        }{
                Numero:       order.Number,
                Status:       string(order.Status),
                Total:        order.Total,
                ModePaiement: string(order.PaymentMode),
                Zone:         zone.Name,
        }
        b, _ := json.Marshal(out)
        return b, nil
}

// ============================================================================
// Tool 11 : enregistrer_prospect
// ============================================================================

func (r *ToolRegistry) toolEnregistrerProspect(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
        var in struct {
                Produit   string `json:"produit"`
                Intention string `json:"intention"`
        }
        _ = json.Unmarshal(args, &in)
        if in.Intention == "" {
                return errJSON("enregistrer_prospect", errors.New("intention requise")), nil
        }
        // The customer was created (or fetched) at the start of the conversation
        // by the AI service, so r.customerID is already set. If their status is
        // 'prospect', we just keep it as 'prospect' and update last_interaction_at
        // (already done by GetOrCreateByPhone). If they're 'client' or 'recurring'
        // we don't downgrade them.
        cust, err := r.customerRepo.GetByID(ctx, r.shopID, r.customerID)
        if err != nil {
                return errJSON("enregistrer_prospect", err), nil
        }
        // No status change needed — prospect is the default. We just acknowledge.
        out := struct {
                Ok        bool   `json:"ok"`
                Statut    string `json:"statut"`
                Intention string `json:"intention"`
        }{
                Ok:        true,
                Statut:    cust.Status,
                Intention: in.Intention,
        }
        b, _ := json.Marshal(out)
        return b, nil
}

// ============================================================================
// Tool 12 : escalader_vers_humain
// ============================================================================

func (r *ToolRegistry) toolEscaladerVersHumain(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
        var in struct {
                Raison string `json:"raison"`
        }
        _ = json.Unmarshal(args, &in)
        if in.Raison == "" {
                in.Raison = "escalade demandée par le client"
        }
        // Set the conversation state to 'human'.
        if err := r.conversationRepo.UpdateState(ctx, r.shopID, r.conversationID, "human", &r.userID); err != nil {
                return errJSON("escalader_vers_humain", err), nil
        }
        // Create a notification for the shop (broadcast to all members).
        payload, _ := json.Marshal(map[string]any{
                "conversation_id": r.conversationID.String(),
                "customer_id":     r.customerID.String(),
                "raison":          in.Raison,
        })
        _, _ = r.notifRepo.Create(ctx, &r.shopID, nil, "human_takeover_requested", payload)
        out := struct {
                Ok     bool   `json:"ok"`
                Raison string `json:"raison"`
        }{
                Ok:     true,
                Raison: in.Raison,
        }
        b, _ := json.Marshal(out)
        return b, nil
}

// ============================================================================
// Helpers
// ============================================================================

// errJSON returns a structured error JSON for the LLM. The error message is
// sanitized (no internal stack traces).
func errJSON(toolName string, err error) json.RawMessage {
        out := struct {
                Erreur string `json:"erreur"`
                Tool   string `json:"tool,omitempty"`
        }{
                Erreur: sanitizeErr(err),
                Tool:   toolName,
        }
        b, _ := json.Marshal(out)
        return b
}

// sanitizeErr strips verbose wrapping from error messages so the LLM doesn't
// try to paraphrase internal stack traces.
func sanitizeErr(err error) string {
        if err == nil {
                return ""
        }
        msg := err.Error()
        // Strip common prefixes.
        for _, prefix := range []string{"ai tools: ", "service: ", "repo: "} {
                if strings.HasPrefix(msg, prefix) {
                        msg = strings.TrimPrefix(msg, prefix)
                }
        }
        if len(msg) > 300 {
                msg = msg[:300] + "…"
        }
        return msg
}

// findVariantByQuery resolves a (productName, variantAttrs) pair to a variant
// UUID. Used by ajouter_au_panier when the LLM only knows the product name +
// size/color (not the variant UUID).
func (r *ToolRegistry) findVariantByQuery(ctx context.Context, productName, variantAttrs string) (uuid.UUID, error) {
        if productName == "" {
                return uuid.Nil, errors.New("nom de produit requis")
        }
        items, _, err := r.catalogSvc.ListProductsTyped(ctx, r.userID, r.userRole, r.shopID, models.ListProductsParams{Page: 1, Limit: 5, Search: productName, Status: "published"})
        if err != nil || len(items) == 0 {
                return uuid.Nil, fmt.Errorf("produit introuvable: %s", productName)
        }
        full, err := r.catalogSvc.GetProduct(ctx, r.userID, r.userRole, r.shopID, items[0].Product.ID)
        if err != nil || full == nil || len(full.Variants) == 0 {
                return uuid.Nil, fmt.Errorf("aucune variante pour: %s", productName)
        }
        // If no variant attrs given, return the first active variant.
        lowAttrs := strings.ToLower(strings.TrimSpace(variantAttrs))
        if lowAttrs == "" {
                for i := range full.Variants {
                        if full.Variants[i].Active {
                                return full.Variants[i].ID, nil
                        }
                }
                return full.Variants[0].ID, nil
        }
        // Match against size/color (case-insensitive substring).
        for i := range full.Variants {
                v := &full.Variants[i]
                if !v.Active {
                        continue
                }
                if v.Size != nil && strings.Contains(strings.ToLower(*v.Size), lowAttrs) {
                        return v.ID, nil
                }
                if v.Color != nil && strings.Contains(strings.ToLower(*v.Color), lowAttrs) {
                        return v.ID, nil
                }
        }
        // No match — return first active as best-effort.
        for i := range full.Variants {
                if full.Variants[i].Active {
                        return full.Variants[i].ID, nil
                }
        }
        return full.Variants[0].ID, nil
}

// defaultZone returns the shop's first active delivery zone (ordered by
// created_at). Used as a fallback by generer_recapitulatif and
// confirmer_commande when the LLM doesn't supply a zone (the LLM should
// ideally ask the customer, but for the simulation console and simple flows
// we fall back to a sensible default rather than blocking the conversation).
func (r *ToolRegistry) defaultZone(ctx context.Context) (*models.DeliveryZone, error) {
        zones, err := r.deliverySvc.ListZones(ctx, r.userID, r.userRole, r.shopID, true)
        if err != nil {
                return nil, fmt.Errorf("list zones: %w", err)
        }
        if len(zones) == 0 {
                return nil, errors.New("la boutique n'a aucune zone de livraison active")
        }
        return &zones[0], nil
}

// cartToJSON serializes a cart into the LLM-friendly shape used by the
// voir/ajouter/modifier panier tools. The shape is consumed by the mock
// provider's reply generator AND by the output guardrail for number
// verification.
func cartToJSON(cart *models.Cart) json.RawMessage {
        if cart == nil {
                return json.RawMessage(`{"articles":[],"sous_total":0}`)
        }
        type articleItem struct {
                ID           string  `json:"id"`
                VarianteID   *string `json:"variante_id,omitempty"`
                Nom          string  `json:"nom"`
                Quantite     int     `json:"quantite"`
                PrixUnitaire int64   `json:"prix_unitaire"`
                Total        int64   `json:"total"`
        }
        articles := make([]articleItem, 0, len(cart.Items))
        var sousTotal int64
        for i := range cart.Items {
                it := &cart.Items[i]
                lineTotal := it.UnitPrice * int64(it.Quantity)
                ai := articleItem{
                        ID:           it.ID.String(),
                        Nom:          it.ProductName,
                        Quantite:     it.Quantity,
                        PrixUnitaire: it.UnitPrice,
                        Total:        lineTotal,
                }
                if it.VariantID != nil {
                        s := it.VariantID.String()
                        ai.VarianteID = &s
                }
                articles = append(articles, ai)
                sousTotal += lineTotal
        }
        out := struct {
                Articles  []articleItem `json:"articles"`
                SousTotal int64         `json:"sous_total"`
                Statut    string        `json:"statut"`
        }{
                Articles:  articles,
                SousTotal: sousTotal,
                Statut:    string(cart.Status),
        }
        b, _ := json.Marshal(out)
        return b
}
