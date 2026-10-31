# NOVA — Monorepo

**Employé numérique de votre commerce** — SaaS IA + WhatsApp pour les petites entreprises de Côte d'Ivoire.

## Structure

```
nova/
├── frontend/     # Next.js 16 (TypeScript + Tailwind + shadcn/ui)
└── backend/       # API Go (chi router + PostgreSQL + Cloudflare R2)
```

## Frontend

Application Next.js 16 avec :
- Espace utilisateur (boutique) : Accueil, Produits, Stock, Commandes, Conversations, Clients, Paiement, Statistiques, Abonnement, Équipe
- Espace superadmin (séparé) : Vue d'ensemble, Boutiques, Utilisateurs, Revenus, Supervision IA/WhatsApp, Support, Journal d'audit, Paramètres
- Auth : login, register, mode démo, onboarding boutique
- Upload images vers Cloudflare R2
- Mode démo isolé (données mockées uniquement si isDemo=true)

```bash
cd frontend
bun install
bun run dev    # http://localhost:3000
```

## Backend

API Go avec :
- Auth : register, login, 2FA, password reset, sessions HMAC
- Catalogue : produits, variantes, options (plats), images
- Stock : inventory, movements, adjust/receive
- Commandes : workflow 8 statuts
- WhatsApp : Meta Cloud API, webhooks, templates
- Abonnements : plans, quotas, lifecycle, payments
- Admin : boutiques, abonnements, revenus, cron
- RLS : isolation tenant par shop_id
- 100+ endpoints, ~31 000 lignes Go

```bash
cd backend
cp .env.example .env
go run ./cmd/server     # http://localhost:8080
```

## Comptes de test

### Superadmin
- Email : superadmin2026@gmail.com
- Mot de passe : Admin2006

### Utilisateur boutique
- Email : eliesegnibo2@gmail.com
- Mot de passe : Test1234

## Auteur

eliesegnibo32-bit — eliesegnibo32@gmail.com

## Licence

Propriétaire — © 2026 NOVA
