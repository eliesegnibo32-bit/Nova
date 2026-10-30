# NOVA — Monorepo

**Employé numérique de votre commerce** — SaaS IA + WhatsApp pour les petites entreprises.

## Structure

```
nova/
├── front-end/     # Next.js 16 (TypeScript + Tailwind + shadcn/ui)
└── backend/       # API Go (chi + PostgreSQL + Cloudflare R2)
```

## Front-end

```bash
cd front-end
bun install
bun run dev    # http://localhost:3000
```

## Backend

```bash
cd backend
cp .env.example .env
go run ./cmd/server     # http://localhost:8080
```

## Auteur

eliesegnibo32-bit — eliesegnibo32@gmail.com
