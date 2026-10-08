# NOVA — Migrations PostgreSQL

Schéma complet de la base de données NOVA (SaaS IA + WhatsApp pour le commerce
en Côte d'Ivoire). Conformément au cahier des charges v2.0 (ch. 9.3 et 10) :
isolation multi-boutiques par `shop_id` + RLS PostgreSQL.

## Stack

- **PostgreSQL ≥ 13** (recommandé ≥ 15 pour `GENERATED ... STORED`)
- Extensions : `pgcrypto` (`gen_random_uuid()`), `uuid-ossp` (compat)
- Cible de production : **Neon** (PostgreSQL managé serverless)

## Organisation

Les migrations sont numérotées `NNN_description.{up,down}.sql` (convention goose).

| N°  | Sujet                                                     |
|-----|-----------------------------------------------------------|
| 001 | Extensions + types énumérés (11 enums métier)             |
| 002 | Boutiques, utilisateurs, rattachements (`shop_members`)   |
| 003 | Catalogue : produits, variantes, photos                   |
| 004 | Stock : inventaire courant + journal immuable             |
| 005 | Clients, conversations, messages WhatsApp                 |
| 006 | Paniers, commandes, lignes, historique des statuts        |
| 007 | Zones de livraison + livraisons                           |
| 008 | Plans, abonnements, paiements d'abonnement                |
| 009 | Consommation IA, modèles WhatsApp, consentements          |
| 010 | Notifications internes + journaux d'audit (append-only)   |
| 011 | Row Level Security — isolation multi-boutiques            |
| 012 | Seed de développement (plan Essentiel + boutique démo)    |

## Pré-requis : installer goose

```bash
go install github.com/pressly/goose/v3/cmd/goose@latest
# Vérifier :
goose --version
```

## Exécuter les migrations

### Up (appliquer toutes les migrations en attente)

```bash
goose -dir migrations postgres "$DATABASE_URL" up
```

### Status (voir l'état des migrations)

```bash
goose -dir migrations postgres "$DATABASE_URL" status
```

### Down (annuler la dernière migration)

```bash
goose -dir migrations postgres "$DATABASE_URL" down
```

### Reset (tout annuler puis tout ré-appliquer)

```bash
goose -dir migrations postgres "$DATABASE_URL" reset
```

### Variable `DATABASE_URL`

Format Neon / PostgreSQL :

```
postgres://USER:PASSWORD@HOST:5432/DBNAME?sslmode=require
```

Exemple local :

```bash
export DATABASE_URL="postgres://nova:nova@localhost:5432/nova_dev?sslmode=disable"
```

## RLS : comment l'application doit se positionner

La politique RLS repose sur trois paramètres de session positionnés par
l'application Go au début de chaque transaction (pgx + `BeginTx`) :

```sql
SET LOCAL app.current_shop_id = '<uuid-boutique>';
SET LOCAL app.user_id         = '<uuid-utilisateur>';
SET LOCAL app.user_role       = 'owner';   -- ou 'super_admin' / 'admin'
```

`SET LOCAL` limite la portée à la transaction courante : aucun risque de fuite
entre requêtes si le pool est en mode transaction.

### Fonctions utilitaires SQL

| Fonction                  | Retour                                             |
|---------------------------|----------------------------------------------------|
| `current_shop_id()`       | UUID de la boutique courante (NULL si non défini)  |
| `current_user_id()`       | UUID de l'utilisateur courant                       |
| `is_platform_admin()`     | `true` si `app.user_role ∈ (super_admin, admin)`   |
| `is_shop_member_of(s,u)`  | `true` si l'utilisateur `u` est membre de `s`      |

### Tables globales (PAS de RLS)

- `plans` : offres SaaS, visibles par toutes les boutiques.
- `message_templates` : modèles Meta, visibles par toutes les boutiques.

### Tables append-only (INSERT + SELECT seulement)

- `stock_movements` : journal de stock (trigger + RLS bloquent UPDATE/DELETE).
- `order_events` : historique des statuts de commande.
- `audit_logs` : journal d'audit (trigger + RLS bloquent UPDATE/DELETE).
- `ai_usage` : journal de consommation IA.

## Démarrage rapide pour le développement

```bash
# 1. Créer une base locale
createdb nova_dev

# 2. Appliquer toutes les migrations (y compris le seed)
export DATABASE_URL="postgres://$(whoami)@localhost:5432/nova_dev?sslmode=disable"
goose -dir migrations postgres "$DATABASE_URL" up

# 3. Vérifier
psql "$DATABASE_URL" -c "\dt"
```

Le seed (`012_seed`) crée :

- Plan **Essentiel** (10 000 FCFA/mois, 1000 messages, 100 produits, 2 employés)
- Boutique démo **Boutique Démo CI** (Cocody)
- Owner `owner@boutique-demo.ci` (mot de passe `demo1234` — hash bcrypt
  placeholder, à remplacer par Argon2id côté application)
- 3 produits avec 6 variantes, stock initial, 3 zones de livraison (Abidjan)

## Conventions de schéma

- **UUID** partout (`gen_random_uuid()` par défaut).
- **Montants** : `bigint` (FCFA en entiers, pas de décimales).
- **Timestamps** : `timestamptz`, défaut `now()`.
- **Suppression logique** : `deleted_at timestamptz` (NULL = actif) sur les
  tables à historique (shops, users, products, customers).
- **Index composites** commençant par `shop_id` (ch. 9.3).
- **Dénormalisation** : `shop_id` est reproduit sur `product_variants`,
  `product_images`, `cart_items`, `order_items`, `order_events`, `deliveries`,
  `subscription_payments`, `ai_usage`, `consents`, `notifications`, `audit_logs`
  pour permettre une vérification RLS efficace sans jointure.
- **Snapshots** : `order_items` fige `product_name`, `variant_info`, `unit_price`
  au moment de la commande (cohérence comptable si le catalogue change).
- **CHECK** : contraintes métier (`promo_price < price`, `reserved <= on_hand`,
  `total = subtotal + delivery_fee`, `line_total = unit_price * quantity`, etc.).

## Tests d'isolation (à implémenter côté application)

Conformément au ch. 9.3, des tests automatiques d'isolation doivent s'exécuter
à chaque déploiement : un utilisateur de la boutique A ne doit ni lire ni écrire
dans la boutique B. Ces tests se feront côté Go (intégration) — voir tâche
ultérieure.

## Prochaines étapes (hors périmètre de cette tâche)

- Dossier `queries/` : requêtes SQL réutilisables (sqlc ou queries brutes).
- Triggers `updated_at` automatiques (à ajouter si pas gérés côté application).
- Partials index pour les files de tâches River (lors de l'intégration River).
- Tables futures mentionnées au ch. 10 : `integrations`, `support_tickets`,
  `warehouses`, `drivers`, `invoices`, `campaigns`.
