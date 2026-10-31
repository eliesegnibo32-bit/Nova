# ADMIN-SPACE-V2 — Espace superadmin NOVA complet (cahier des charges v2)

**Agent** : Z.ai Code (fullstack)
**Task ID** : ADMIN-SPACE-V2
**Date** : 2026-10-31
**Stack** : Next.js 16 + TypeScript + Tailwind 4 + shadcn/ui + Zustand
**Référence** : `upload/Cahier_des_charges_NOVA_Super_Administration_V2.pdf`
**Suite de** : `agent-ctx/ADMIN-FRONTEND-z-ai-code.md` (qui avait posé les 4 premières vues)

## Objectif
Compléter l'espace superadmin NOVA selon le cahier des charges v2 :
1. Enrichir le dashboard global (6 blocs KPI thématiques).
2. Ajouter actions Suspendre / Réactiver / Résilier + dialog de détail boutique.
3. Ajouter une section « Paiements récents » dans la vue Revenus.
4. Créer 4 nouvelles vues : Supervision IA & WhatsApp, Support, Journal d'audit, Paramètres globaux.

## Fichiers modifiés

### 1. `src/stores/ui-store.ts`
- Ajout de 4 nouvelles clés `ViewKey` : `admin-monitoring`, `admin-support`, `admin-audit`, `admin-settings`.
- Ajout des labels FR correspondants dans `viewLabels` (Supervision, Support, Journal d'audit, Paramètres).
- Extension du tableau `ADMIN_VIEWS` avec les 4 nouvelles clés.

### 2. `src/lib/api.ts`
- Ajout de 2 méthodes au namespace `adminApi` :
  - `suspendShop(shopId, reason)` → `POST /api/shops/{id}/suspend` (body `{ reason }`).
  - `reactivateShop(shopId)` → `POST /api/shops/{id}/reactivate`.
- Les deux retournent un `Shop` (réponse du backend Go `models.ToShopResponse`).

### 3. `src/components/admin/admin-shell.tsx`
- Import des 4 nouvelles vues (`AdminMonitoringView`, `AdminSupportView`, `AdminAuditView`, `AdminSettingsView`).
- Ajout des icônes Lucide : `Activity`, `LifeBuoy`, `ScrollText`, `Settings`.
- Extension de `ADMIN_NAV` avec 4 entrées (Supervision, Support, Journal d'audit, Paramètres).
- Ajout des 4 branches dans `renderView()`.

### 4. `src/components/views/admin-overview-view.tsx` — **ENRICHIE**
Remplacement de la grille 4-KPI par **6 sections KPI thématiques** (`KpiSection`), alignées sur le cahier des charges :
- **Boutiques** : Total, Actives, En essai, Suspendues, Résiliées, Impayées, Nouvelles (30j).
- **Abonnements** : Actifs, Expirant bientôt (≤7j), En retard, Résiliés, MRR.
- **Utilisation** : Messages reçus/envoyés, Conversations actives, Volume IA, Consommation/boutique — *non disponible*.
- **Intégrations** : Connexions WhatsApp, Erreurs API, Webhooks en échec, Incidents ouverts — *non disponible*.
- **Finances** : Frais installation, Abo. encaissés (issus de `total_revenue`), Remboursements, Coûts techniques, Marge — *non disponible*.
- **Support** : Tickets ouverts, Urgents P1, En retard, 1ère réponse — *non disponible* (0 pour les comptes).

Métriques calculées localement à partir des données existantes :
- `trialCount` = nombre d'abonnements `trial`.
- `expiringSoonCount` = abonnements dont `next_billing_at` ≤ 7 jours.
- `newSignupsCount` = boutiques créées dans les 30 derniers jours (`shop.createdAt`).

Ajout d'un appel `adminApi.listShops({ limit: 500 })` dans le `Promise.all` pour récupérer le total boutiques et les nouvelles inscriptions. Ajout d'un bouton « Actualiser » et d'un lien « Supervision IA & WhatsApp » / « Journal d'audit » dans les actions rapides. Note de bas de page explicitant le marqueur « non disponible » (aucune extrapolation).

### 5. `src/components/views/admin-shops-view.tsx` — **ENRICHIE**
- **Statut combiné** boutique + abonnement (`combinedStatus`) respectant le cahier des charges :
  En attente (amber), Active (green), En grâce (blue), Suspendue (red), Résiliée (gray), plus Essai/En retard.
- **État paiement** distinct (`paymentStateBadge`) basé sur `subscription.status`.
- **Nouvelles colonnes** : Propriétaire (`ownerName`/`owner_email`), Contacts (`phone`/`whatsapp_number`), Formule, Paiement, Date création.
- **Menu d'actions par ligne** (DropdownMenu) : Voir détails, Suspendre…, Réactiver, Résilier…
- **Dialog Suspendre** avec `<Textarea>` obligatoire (motif, max 500 car.) + compteur — appelle `adminApi.suspendShop`.
- **Dialog Réactiver** de confirmation — appelle `adminApi.reactivateShop`.
- **Résilier** : toast « À implémenter » (endpoint `POST /api/shops/{id}/terminate` non exposé côté Go).
- **Dialog Détails boutique** (`ShopDetail`) avec 6 sections : Informations générales, Abonnement, État WhatsApp, Consommation IA, Historique des changements, Derniers incidents. Les sections sans backend exposé affichent un panneau « non disponible » explicite.
- Filtre par statut étendu (Active, En attente, Essai, En retard, En grâce, Suspendue, Résiliée).
- Recherche étendue au propriétaire et à l'email.
- Clic sur une ligne → ouvre le dialog de détails.
- Responsive : colonnes masquées sur mobile (`hidden md:table-cell`, `hidden lg:table-cell`), table dans `overflow-x-auto`.
- Chargements asynchrones avec `setActing` pour désactiver les boutons pendant l'action.

### 6. `src/components/views/admin-revenue-view.tsx` — **ENRICHIE**
- Ajout d'un appel `adminApi.listSubscriptions({ limit: 200 })` dans le `Promise.all`.
- `priceByPlan` : Map `plan_name → monthly_price` (depuis `revenue.by_plan`).
- `recentPayments` : top 10 abonnements triés par `started_at`/`created_at` desc.
- Nouvelle section **« Paiements récents »** : table (Boutique, Formule, Montant estimé, Date, Statut) + note explicitant que le montant est estimé depuis le prix du plan, en attendant l'endpoint `/api/admin/subscriptions/payments`.
- Ajout du helper `paymentBadge(status)` (À jour / Essai / En retard / En grâce / Suspendu / Résilié).

## Fichiers créés

### 7. `src/components/views/admin-monitoring-view.tsx` — **NOUVEAU**
Vue « Supervision IA & WhatsApp » avec 4 cartes :
- **WhatsApp** : connexions actives, erreurs d'envoi/réception, webhooks en échec, derniers incidents.
- **IA & consommation** : requêtes totales, tokens, coût estimé, erreurs, top 5 boutique.
- **Quotas IA** : proches du quota, au quota, plafond global + `<Progress>` d'occupation.
- **Qualité du service** : transferts vers humain, réponses sans source, taux de réponse, satisfaction.

4 KPI cards en haut + bandeau explicatif. Toutes les métriques non remontées par le backend affichent **« non disponible »**. Seul `adminApi.getRevenue()` est utilisé (MRR plateforme). Sous-composants réutilisables : `MetricCard`, `InfoRow` (avec `tone` et `icon` optionnels).

### 8. `src/components/views/admin-support-view.tsx` — **NOUVEAU**
Vue « Support & incidents » :
- 4 KPI cards : Tickets ouverts, Urgents (P1), En retard (SLA), Résolus (30j).
- Table « Tickets » avec structure complète (Demandeur, Sujet, Priorité, État, Date, Responsable).
- **Badges priorité** : P1 Critique (red/destructive), P2 Haute (orange), P3 Normale (blue), P4 Faible (outline).
- **Badges état** : Nouveau (blue), En cours (brand), Attente commerçant (amber), Attente fournisseur (outline amber), Résolu (emerald), Fermé (outline).
- Filtres : recherche plein texte + Select priorité + Select état.
- État vide soigné (« Système de tickets — disponible prochainement ») car `GET /api/admin/tickets` n'est pas encore exposé.
- 3 cartes d'aperçu des fonctionnalités à venir (Priorités & SLA, File d'assignation, Reporting support).

### 9. `src/components/views/admin-audit-view.tsx` — **NOUVEAU**
Vue « Journal d'audit » :
- Table « Journal d'audit » (Acteur, Date/heure UTC, Objet, Action, Motif, Résultat).
- **Badges action** : `shop.suspend` (destructive), `shop.terminate` (outline muted), `shop.reactivate` (brand), `subscription.payment` (amber), autres (outline).
- **Badges résultat** : Succès (emerald), Échec (destructive).
- Filtres : recherche (acteur/objet), Select type d'action (9 valeurs : `shop.suspend`, `shop.reactivate`, `shop.terminate`, `shop.create`, `shop.update`, `subscription.payment`, `subscription.update`, `user.role_change`, `config.update`), plage de dates (2 `<Input type="date">`).
- État vide soigné (« Journal d'audit — disponible prochainement ») car `GET /api/admin/audit` n'est pas encore exposé.
- 3 cartes d'aperçu (Traçabilité complète, Recherche avancée, Conservation longue 12 mois).

### 10. `src/components/views/admin-settings-view.tsx` — **NOUVEAU**
Vue « Paramètres globaux » (lecture seule, en attente backend) :
- Bandeau « lecture seule » amber en haut.
- 4 cartes principales :
  - **Formules et tarifs** : Essentiel 10 000 FCFA/mois, installation 20 000 FCFA (via `formatFCFA`).
  - **Quotas IA** : messages par formule (1 000 / 5 000 / 20 000) + plafond global (500 000).
  - **Communications** : 4 toggles (annonce essai, retard, suspendues, newsletter) avec `<Switch>`.
  - **Sécurité** : MFA obligatoire (toggle), durée session 8h, tentatives max 5/15min, verrouillage 30min, rotation 90j.
- Chaque carte a un bouton « Modifier » qui toast « À implémenter » (`/api/admin/settings` à venir).
- Carte récapitulative « État du déploiement » listant les 4 sections avec badge « En attente backend ».
- Sous-composants : `PriceRow`, `ConfigRow` (avec `tone`), `ToggleRow`, `DeployRow`.

## Choix de design
- **Aucune donnée extrapolée** : toutes les métriques non remontées par le backend Go affichent explicitement « non disponible ». Les compteurs de tickets sont à `0` (état vide légitime en attendant l'endpoint).
- **Couleurs conformes** : brand green NOVA pour succès/actif, amber-accent pour avertissement/grâce, rose pour erreur/suspendu, blue pour essai/attente, muted pour résilié/fermé. **Pas d'indigo/bleu primaire**.
- **Composants shadcn/ui** utilisés : Card, Badge, Button, Table, Input, Select, Dialog, DropdownMenu, Textarea, Label, Separator, Switch, Progress, Skeleton, Alert.
- **Icônes Lucide** : Activity, LifeBuoy, ScrollText, Settings, AlertTriangle, ShieldCheck, AlertOctagon, XOctagon, PauseCircle, PlayCircle, MoreHorizontal, Eye, Receipt, Gauge, Cpu, MessageSquare, History, etc.
- **FR** partout (labels, descriptions, toasts, états vides).
- **Responsive mobile-first** : grilles `grid-cols-2 lg:grid-cols-4` ou `grid-cols-1 lg:grid-cols-2 xl:grid-cols-3`, colonnes de table masquées sur petit écran, tables dans `overflow-x-auto`, dialogs `sm:max-w-*`.
- **États de chargement** : `<Skeleton>` partout (cards, tables, listes). Boutons désactivés avec `<Loader2 className="animate-spin" />` pendant les actions async.
- **Gestion d'erreur** : `ApiError` distingué des erreurs génériques, toasts `sonner` (error/success/info).
- **Accessibilité** : `aria-label` sur tous les inputs/boutons icon-only, `aria-current="page"` sur la nav active, arrêt de propagation `e.stopPropagation()` sur les actions imbriquées dans les lignes cliquables.

## Backend contract vérifié
Confirmé dans `mini-services/nova-api/internal/api/handlers/shops.go` :
- `POST /api/shops/{id}/suspend` — body `models.SuspendShopRequest{ Reason string }` → `ShopResponse` (status='suspended'). Admin only (`middleware.RequireRole`).
- `POST /api/shops/{id}/reactivate` — no body → `ShopResponse` (status='active'). Admin only.
- `GET /api/shops` — liste globale paginée quand `services.IsPlatformAdmin(s.Role)`.
- Aucun endpoint `terminate` n'est enregistré (`r.Post("/suspend")`, `r.Post("/reactivate")` seulement) → justifie le toast « À implémenter » pour la résiliation.

## Vérification

| Check | Résultat |
| --- | --- |
| `bunx tsc --noEmit` | ✅ 0 erreur dans `src/` (erreurs stale uniquement dans `examples/`, `frontend_copy/`, `skills/` — préexistantes) |
| `bun run lint` | ✅ 0 erreur, 0 warning |
| `bun run dev` (auto) | ✅ `GET / 200 in 275ms` après les derniers edits |

## Problèmes rencontrés et résolus
1. **`void Input;` / `void Cpu; void TrendingUp;`** : j'avais initialement laissé ces statements pour « silencer » les imports inutilisés pendant l'itération. Cela a provoqué des `ReferenceError: Cpu is not defined` à l'exécution (les `void` sont évalués au runtime). Corrigé en supprimant les imports inutilisés à la place. Le dev log montre `GET / 500` transitoires puis `GET / 200` après correction.
2. **`paymentBadge` manquant dans revenue-view** : j'ai référencé la fonction avant de la définir. Ajoutée en haut du fichier.
3. **`isFailure` inutilisé dans audit-view** : variable déclarée mais non utilisée dans `actionBadge`. Refactorisé en `actionStr.startsWith(...)`.

## Limitations connues / suites
- **Vues support/audit/settings** sont des placeholders structurés : l'UI complète est en place mais aucune donnée n'est chargée tant que les endpoints Go `/api/admin/tickets`, `/api/admin/audit`, `/api/admin/settings` ne sont pas exposés.
- **Vue monitoring** : seules les KPI remontées par `/api/admin/subscriptions/revenue` sont réelles (MRR). Le reste est « non disponible ».
- **Résilier une boutique** : bouton présent mais toast « À implémenter » — endpoint `POST /api/shops/{id}/terminate` à ajouter côté Go.
- **Paiements récents** : le montant est estimé depuis le prix du plan (join `by_plan`), en attendant un vrai endpoint `/api/admin/subscriptions/payments` qui retournerait les paiements Wave/Moov/Orange/MTN avec leur date et montant réel.
- **Pas de pagination** sur les tables admin (limite 200/500 items). À ajouter si le volume grossit.
