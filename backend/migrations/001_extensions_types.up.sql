-- +goose Up
-- +goose StatementBegin
-- ============================================================================
-- 001 : Extensions et types énumérés de base du projet NOVA
-- ============================================================================
-- On utilise pgcrypto pour gen_random_uuid() (présent dans le noyau PG >=13).
-- uuid-ossp est aussi créé pour compatibilité (uuid_generate_v4()).
-- ============================================================================

CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- ---------------------------------------------------------------------------
-- Rôles utilisateur (plateforme + boutique)
--   super_admin : administrateur NOVA (plateforme)
--   admin       : administrateur NOVA (niveau inférieur)
--   owner       : propriétaire de boutique
--   employee    : employé de boutique
-- ---------------------------------------------------------------------------
CREATE TYPE user_role AS ENUM (
    'super_admin',
    'admin',
    'owner',
    'employee'
);

-- ---------------------------------------------------------------------------
-- Statut d'un produit dans le catalogue
-- ---------------------------------------------------------------------------
CREATE TYPE product_status AS ENUM (
    'draft',        -- brouillon, non visible
    'published',    -- publié, proposé par l'IA
    'archived'      -- archivé, plus vendu mais conservé pour l'historique
);

-- ---------------------------------------------------------------------------
-- Statut d'une commande (machine à états du cahier des charges ch.4)
-- ---------------------------------------------------------------------------
CREATE TYPE order_status AS ENUM (
    'draft',           -- panier en cours de constitution
    'pending',         -- en attente de confirmation par l'employé
    'confirmed',       -- confirmée, en attente de préparation
    'preparing',       -- en préparation
    'delivering',      -- en livraison
    'delivered',       -- livrée
    'delivery_failed', -- échec de livraison
    'returned',        -- retournée
    'cancelled'        -- annulée
);

-- ---------------------------------------------------------------------------
-- Statut du paiement d'une commande
--   on_delivery     : paiement à la livraison (défaut commerce CI)
--   pending_payment : en attente de paiement
--   declared        : paiement déclaré par le client (à valider)
--   paid            : payé
--   failed          : échec du paiement
--   refunded        : remboursé
-- ---------------------------------------------------------------------------
CREATE TYPE payment_status AS ENUM (
    'on_delivery',
    'pending_payment',
    'declared',
    'paid',
    'failed',
    'refunded'
);

-- ---------------------------------------------------------------------------
-- Modes de paiement acceptés en Côte d'Ivoire
-- ---------------------------------------------------------------------------
CREATE TYPE payment_mode AS ENUM (
    'cash',
    'mobile_money',
    'wave',
    'orange_money',
    'mtn_momo'
);

-- ---------------------------------------------------------------------------
-- Statut d'un abonnement NOVA (cycle de vie commercial)
-- ---------------------------------------------------------------------------
CREATE TYPE subscription_status AS ENUM (
    'trial',         -- période d'essai
    'active',        -- actif et à jour
    'late',          -- en retard de paiement
    'grace_period',  -- période de grâce
    'suspended',     -- suspendu (quota dépassé, impayé)
    'terminated'     -- résilié
);

-- ---------------------------------------------------------------------------
-- État d'une conversation WhatsApp
-- ---------------------------------------------------------------------------
CREATE TYPE conversation_state AS ENUM (
    'ai',     -- gérée par l'IA
    'human',  -- repris par un humain
    'closed'  -- clôturée
);

-- ---------------------------------------------------------------------------
-- Type de mouvement de stock (journal immuable)
-- ---------------------------------------------------------------------------
CREATE TYPE stock_movement_type AS ENUM (
    'receipt',      -- entrée (réception)
    'reservation',  -- réservation (panier/commande)
    'release',      -- libération de réservation
    'exit',         -- sortie (vente confirmée)
    'return',       -- retour client
    'adjustment'    -- ajustement manuel
);

-- ---------------------------------------------------------------------------
-- Statut commercial d'un client
-- ---------------------------------------------------------------------------
CREATE TYPE customer_status AS ENUM (
    'prospect',   -- n'a jamais commandé
    'client',     -- a commandé au moins une fois
    'recurring'   -- client récurrent
);

-- ---------------------------------------------------------------------------
-- Sens d'un message WhatsApp
-- ---------------------------------------------------------------------------
CREATE TYPE message_direction AS ENUM (
    'inbound',   -- reçu du client
    'outbound'   -- envoyé par NOVA / l'employé
);

-- ---------------------------------------------------------------------------
-- Statut d'envoi d'un message WhatsApp (API Cloud Meta)
-- ---------------------------------------------------------------------------
CREATE TYPE message_status AS ENUM (
    'queued',     -- en file d'attente
    'sent',       -- envoyé au transporteur
    'delivered',  -- délivré sur l'appareil
    'read',       -- lu par le destinataire
    'failed'      -- échec d'envoi
);
-- +goose StatementEnd
