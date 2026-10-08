/**
 * Données mockées NOVA — utilisées tant que le backend Go (stubs 501)
 * n'est pas branché. Toutes les valeurs sont réalistes pour un commerce
 * de prêt-à-porter à Abidjan (cf. cahier des charges ch. 4.9).
 */

import type { AuthUser } from "./api";

// ====== Auth ======

export const demoUser: AuthUser = {
  id: "usr_demo_001",
  email: "owner@boutique-demo.ci",
  fullName: "Awa Koné",
  role: "owner",
};

export const demoShop = {
  id: "shp_demo_001",
  name: "Boutique Élégance",
  slug: "elegance",
  country: "Côte d'Ivoire",
  currency: "XOF",
  status: "active" as const,
  ownerName: "Awa Koné",
  plan: "Essentiel",
  planPrice: 10_000,
  planRenewal: "2026-11-15",
};

// ====== Catalogue ======

export type ProductStatus = "published" | "draft";

export interface Product {
  id: string;
  name: string;
  category: string;
  variantLabel: string; // ex: "Noir / M"
  price: number;
  stock: number;
  threshold: number;
  status: ProductStatus;
  thumbnailColor: string; // tailwind class background
  sku: string;
}

export const products: Product[] = [
  {
    id: "p1",
    name: "Robe noire wax",
    category: "Robes",
    variantLabel: "Noir / M",
    price: 18_000,
    stock: 4,
    threshold: 5,
    status: "published",
    thumbnailColor: "bg-zinc-900",
    sku: "RBN-WAX-M",
  },
  {
    id: "p2",
    name: "Sac à main cuir",
    category: "Accessoires",
    variantLabel: "Noir",
    price: 24_500,
    stock: 12,
    threshold: 4,
    status: "published",
    thumbnailColor: "bg-zinc-800",
    sku: "SAC-CUIR-N",
  },
  {
    id: "p3",
    name: "Chaussures talon bois",
    category: "Chaussures",
    variantLabel: "Cognac / 39",
    price: 22_000,
    stock: 2,
    threshold: 3,
    status: "published",
    thumbnailColor: "bg-amber-700",
    sku: "CHS-TAL-39",
  },
  {
    id: "p4",
    name: "Ensemble pagne 6 yards",
    category: "Pagnes",
    variantLabel: "Multicolore",
    price: 9_500,
    stock: 35,
    threshold: 10,
    status: "published",
    thumbnailColor: "bg-rose-600",
    sku: "PGN-6YD-MC",
  },
  {
    id: "p5",
    name: "Boubou brodé homme",
    category: "Homme",
    variantLabel: "Bleu / L",
    price: 28_000,
    stock: 6,
    threshold: 3,
    status: "published",
    thumbnailColor: "bg-blue-800",
    sku: "BBR-HOM-L",
  },
  {
    id: "p6",
    name: "Foulard soie premium",
    category: "Accessoires",
    variantLabel: "Émeraude",
    price: 7_500,
    stock: 0,
    threshold: 5,
    status: "published",
    thumbnailColor: "bg-emerald-700",
    sku: "FSL-EME",
  },
  {
    id: "p7",
    name: "Sandales tong doré",
    category: "Chaussures",
    variantLabel: "Unique",
    price: 6_000,
    stock: 22,
    threshold: 8,
    status: "published",
    thumbnailColor: "bg-yellow-600",
    sku: "SDT-UNI",
  },
  {
    id: "p8",
    name: "Ensemble enfant fête",
    category: "Enfant",
    variantLabel: "Rose / 4 ans",
    price: 12_000,
    stock: 1,
    threshold: 4,
    status: "published",
    thumbnailColor: "bg-pink-500",
    sku: "ENF-FET-4A",
  },
  {
    id: "p9",
    name: "Ceinture métallique",
    category: "Accessoires",
    variantLabel: "Or",
    price: 4_500,
    stock: 18,
    threshold: 6,
    status: "draft",
    thumbnailColor: "bg-yellow-500",
    sku: "CNT-MET-OR",
  },
  {
    id: "p10",
    name: "Robe soirée satin",
    category: "Robes",
    variantLabel: "Bordeaux / S",
    price: 26_500,
    stock: 7,
    threshold: 4,
    status: "published",
    thumbnailColor: "bg-red-800",
    sku: "RBS-SAT-S",
  },
];

export const productCategories = [
  "Toutes",
  "Robes",
  "Accessoires",
  "Chaussures",
  "Pagnes",
  "Homme",
  "Enfant",
];

// ====== Stock ======

export interface StockRow {
  id: string;
  productId: string;
  productName: string;
  variantLabel: string;
  onHand: number;
  reserved: number;
  available: number; // onHand - reserved
  threshold: number;
  thumbnailColor: string;
}

export const stockRows: StockRow[] = products.map((p) => ({
  id: `inv_${p.id}`,
  productId: p.id,
  productName: p.name,
  variantLabel: p.variantLabel,
  onHand: p.stock,
  reserved: p.id === "p1" ? 2 : p.id === "p4" ? 3 : p.id === "p7" ? 4 : 0,
  available: Math.max(
    0,
    p.stock - (p.id === "p1" ? 2 : p.id === "p4" ? 3 : p.id === "p7" ? 4 : 0),
  ),
  threshold: p.threshold,
  thumbnailColor: p.thumbnailColor,
}));

export function stockStatus(s: StockRow): "ok" | "low" | "out" {
  if (s.available <= 0) return "out";
  if (s.available < s.threshold) return "low";
  return "ok";
}

// ====== Commandes ======

// V3 : la commande expose à la fois d'anciens statuts (pending, confirmed, …)
// et les nouveaux statuts v3 (en_attente_confirmation, …). Les vues V3
// (orders-view) n'affichent que les nouveaux dans les onglets, mais le type
// reste permissif pour ne pas casser le code historique.
export type OrderStatus =
  | "pending"
  | "confirmed"
  | "delivering"
  | "delivered"
  | "cancelled"
  | "en_attente_confirmation"
  | "en_attente_paiement"
  | "paiement_signalé"
  | "en_cours"
  | "prete"
  | "terminee"
  | "refusee"
  | "annulee";

export type PaymentStatus = "paid" | "unpaid" | "refunded";

export interface OrderItem {
  productName: string;
  variantLabel: string;
  quantity: number;
  unitPrice: number;
}

export interface Order {
  id: string;
  number: string;
  customerName: string;
  customerPhone: string;
  customerZone: string;
  items: OrderItem[];
  subtotal: number;
  deliveryFee: number;
  total: number;
  payment: PaymentStatus;
  status: OrderStatus;
  createdAt: string;
  deliveryAddress: string;
}

export const orders: Order[] = [
  {
    id: "o1",
    number: "CMD-2026-1042",
    customerName: "Aminata Koffi",
    customerPhone: "+225 07 08 12 34 56",
    customerZone: "Cocody",
    items: [
      {
        productName: "Robe noire wax",
        variantLabel: "Noir / M",
        quantity: 1,
        unitPrice: 18_000,
      },
      {
        productName: "Sac à main cuir",
        variantLabel: "Noir",
        quantity: 1,
        unitPrice: 24_500,
      },
    ],
    subtotal: 42_500,
    deliveryFee: 1_500,
    total: 44_000,
    payment: "paid",
    status: "en_attente_confirmation",
    createdAt: "2026-10-30T09:24:00",
    deliveryAddress: "Cocody Angré, 7e tranche, près du pont",
  },
  {
    id: "o2",
    number: "CMD-2026-1041",
    customerName: "Fatou Bamba",
    customerPhone: "+225 05 06 78 90 12",
    customerZone: "Yopougon",
    items: [
      {
        productName: "Ensemble pagne 6 yards",
        variantLabel: "Multicolore",
        quantity: 2,
        unitPrice: 9_500,
      },
    ],
    subtotal: 19_000,
    deliveryFee: 2_000,
    total: 21_000,
    payment: "unpaid",
    status: "en_attente_paiement",
    createdAt: "2026-10-30T08:15:00",
    deliveryAddress: "Yopougon Selmer, à côté de la pharmacie",
  },
  {
    id: "o3",
    number: "CMD-2026-1040",
    customerName: "Mariam Touré",
    customerPhone: "+225 01 02 03 04 05",
    customerZone: "Plateau",
    items: [
      {
        productName: "Boubou brodé homme",
        variantLabel: "Bleu / L",
        quantity: 1,
        unitPrice: 28_000,
      },
    ],
    subtotal: 28_000,
    deliveryFee: 1_000,
    total: 29_000,
    payment: "paid",
    status: "paiement_signalé",
    createdAt: "2026-10-30T07:42:00",
    deliveryAddress: "Plateau Avenue Chardy, immeuble Botreau 3e étage",
  },
  {
    id: "o4",
    number: "CMD-2026-1039",
    customerName: "Adama Cissé",
    customerPhone: "+225 07 11 22 33 44",
    customerZone: "Marcory",
    items: [
      {
        productName: "Chaussures talon bois",
        variantLabel: "Cognac / 39",
        quantity: 1,
        unitPrice: 22_000,
      },
      {
        productName: "Ceinture métallique",
        variantLabel: "Or",
        quantity: 1,
        unitPrice: 4_500,
      },
    ],
    subtotal: 26_500,
    deliveryFee: 1_500,
    total: 28_000,
    payment: "paid",
    status: "en_cours",
    createdAt: "2026-10-29T18:10:00",
    deliveryAddress: "Marcory Zone 4, rue du Canal, villa 12B",
  },
  {
    id: "o5",
    number: "CMD-2026-1038",
    customerName: "Bintou Sow",
    customerPhone: "+225 05 44 55 66 77",
    customerZone: "Treichville",
    items: [
      {
        productName: "Robe soirée satin",
        variantLabel: "Bordeaux / S",
        quantity: 1,
        unitPrice: 26_500,
      },
    ],
    subtotal: 26_500,
    deliveryFee: 1_500,
    total: 28_000,
    payment: "paid",
    status: "prete",
    createdAt: "2026-10-29T15:30:00",
    deliveryAddress: "Treichville avenue 13, près du marché",
  },
  {
    id: "o6",
    number: "CMD-2026-1037",
    customerName: "Salimata Diabaté",
    customerPhone: "+225 07 88 99 00 11",
    customerZone: "Abobo",
    items: [
      {
        productName: "Sandales tong doré",
        variantLabel: "Unique",
        quantity: 1,
        unitPrice: 6_000,
      },
      {
        productName: "Foulard soie premium",
        variantLabel: "Émeraude",
        quantity: 1,
        unitPrice: 7_500,
      },
    ],
    subtotal: 13_500,
    deliveryFee: 2_000,
    total: 15_500,
    payment: "paid",
    status: "terminee",
    createdAt: "2026-10-28T11:20:00",
    deliveryAddress: "Abobo Belgique, à l'arrêt Gare",
  },
  {
    id: "o7",
    number: "CMD-2026-1036",
    customerName: "Hawa Traoré",
    customerPhone: "+225 01 23 45 67 89",
    customerZone: "Koumassi",
    items: [
      {
        productName: "Ensemble enfant fête",
        variantLabel: "Rose / 4 ans",
        quantity: 1,
        unitPrice: 12_000,
      },
    ],
    subtotal: 12_000,
    deliveryFee: 1_500,
    total: 13_500,
    payment: "refunded",
    status: "refusee",
    createdAt: "2026-10-28T09:00:00",
    deliveryAddress: "Koumassi Brillat, rue 12",
  },
  {
    id: "o8",
    number: "CMD-2026-1035",
    customerName: "Awa Bamba",
    customerPhone: "+225 07 12 34 56 78",
    customerZone: "Cocody",
    items: [
      {
        productName: "Sac à main cuir",
        variantLabel: "Noir",
        quantity: 2,
        unitPrice: 24_500,
      },
    ],
    subtotal: 49_000,
    deliveryFee: 1_500,
    total: 50_500,
    payment: "paid",
    status: "terminee",
    createdAt: "2026-10-27T14:00:00",
    deliveryAddress: "Cocody Riviera Palmeraie, lot 24",
  },
  {
    id: "o9",
    number: "CMD-2026-1034",
    customerName: "Koffi Yao",
    customerPhone: "+225 05 67 89 01 23",
    customerZone: "Plateau",
    items: [
      {
        productName: "Boubou brodé homme",
        variantLabel: "Bleu / L",
        quantity: 1,
        unitPrice: 28_000,
      },
    ],
    subtotal: 28_000,
    deliveryFee: 1_000,
    total: 29_000,
    payment: "paid",
    status: "annulee",
    createdAt: "2026-10-26T16:30:00",
    deliveryAddress: "Plateau rue du commerce, 1er étage",
  },
  {
    id: "o10",
    number: "CMD-2026-1033",
    customerName: "Adjoua Kouassi",
    customerPhone: "+225 07 23 45 67 89",
    customerZone: "Yopougon",
    items: [
      {
        productName: "Robe noire wax",
        variantLabel: "Noir / M",
        quantity: 1,
        unitPrice: 18_000,
      },
      {
        productName: "Foulard soie premium",
        variantLabel: "Émeraude",
        quantity: 1,
        unitPrice: 7_500,
      },
    ],
    subtotal: 25_500,
    deliveryFee: 2_000,
    total: 27_500,
    payment: "paid",
    status: "terminee",
    createdAt: "2026-10-25T13:00:00",
    deliveryAddress: "Yopougon Sicogi, à l'école primaire",
  },
  {
    id: "o11",
    number: "CMD-2026-1032",
    customerName: "Fatim Soro",
    customerPhone: "+225 05 99 88 77 66",
    customerZone: "Marcory",
    items: [
      {
        productName: "Ensemble pagne 6 yards",
        variantLabel: "Multicolore",
        quantity: 3,
        unitPrice: 9_500,
      },
    ],
    subtotal: 28_500,
    deliveryFee: 1_500,
    total: 30_000,
    payment: "paid",
    status: "terminee",
    createdAt: "2026-10-24T10:15:00",
    deliveryAddress: "Marcory Zone 3, rue des Jardins",
  },
  {
    id: "o12",
    number: "CMD-2026-1031",
    customerName: "Aminata Koffi",
    customerPhone: "+225 07 08 12 34 56",
    customerZone: "Cocody",
    items: [
      {
        productName: "Robe soirée satin",
        variantLabel: "Bordeaux / S",
        quantity: 1,
        unitPrice: 26_500,
      },
      {
        productName: "Ceinture métallique",
        variantLabel: "Or",
        quantity: 1,
        unitPrice: 4_500,
      },
    ],
    subtotal: 31_000,
    deliveryFee: 1_500,
    total: 32_500,
    payment: "paid",
    status: "terminee",
    createdAt: "2026-10-23T17:45:00",
    deliveryAddress: "Cocody Angré, 7e tranche, près du pont",
  },
];

export const orderStatusLabel: Record<OrderStatus, string> = {
  pending: "En attente",
  confirmed: "Confirmée",
  delivering: "En livraison",
  delivered: "Livrée",
  cancelled: "Annulée",
  en_attente_confirmation: "En attente",
  en_attente_paiement: "Attente paiement",
  "paiement_signalé": "Paiement signalé",
  en_cours: "En cours",
  prete: "Prête",
  terminee: "Terminée",
  refusee: "Refusée",
  annulee: "Annulée",
};

export const paymentStatusLabel: Record<PaymentStatus, string> = {
  paid: "Payé",
  unpaid: "Impayé",
  refunded: "Remboursé",
};

// ====== Conversations ======

export type ConversationStatus = "ai" | "human" | "closed";

export interface Message {
  id: string;
  from: "customer" | "ai" | "merchant";
  text: string;
  at: string;
}

export interface Conversation {
  id: string;
  customerName: string;
  customerPhone: string;
  avatarColor: string;
  lastMessage: string;
  lastAt: string;
  status: ConversationStatus;
  unread: number;
  needsTakeover: boolean;
  messages: Message[];
}

export const conversations: Conversation[] = [
  {
    id: "c1",
    customerName: "Aminata Koffi",
    customerPhone: "+225 07 08 12 34 56",
    avatarColor: "bg-rose-500",
    lastMessage: "D'accord je prends la robe noire en M alors",
    lastAt: "2026-10-30T09:20:00",
    status: "ai",
    unread: 2,
    needsTakeover: true,
    messages: [
      {
        id: "m1",
        from: "customer",
        text: "Bonjour, est-ce que la robe noire wax est disponible en taille M ?",
        at: "2026-10-30T09:10:00",
      },
      {
        id: "m2",
        from: "ai",
        text: "Bonjour Aminata ! Oui la robe noire wax est disponible en taille M à 18 000 FCFA. Souhaitez-vous la commander ?",
        at: "2026-10-30T09:11:00",
      },
      {
        id: "m3",
        from: "customer",
        text: "Oui je veux. Vous livrez à Cocody Angré ?",
        at: "2026-10-30T09:15:00",
      },
      {
        id: "m4",
        from: "ai",
        text: "Oui, livraison à Cocody Angré : 1 500 FCFA, 24 à 48h. Total commande : 19 500 FCFA. Je passe en livraison humaine pour finaliser.",
        at: "2026-10-30T09:16:00",
      },
      {
        id: "m5",
        from: "customer",
        text: "D'accord je prends la robe noire en M alors",
        at: "2026-10-30T09:20:00",
      },
    ],
  },
  {
    id: "c2",
    customerName: "Fatou Bamba",
    customerPhone: "+225 05 06 78 90 12",
    avatarColor: "bg-amber-500",
    lastMessage: "Et le pagne multicolore est lavable en machine ?",
    lastAt: "2026-10-30T08:55:00",
    status: "ai",
    unread: 1,
    needsTakeover: true,
    messages: [
      {
        id: "m1",
        from: "customer",
        text: "Bonsoir, vous avez le pagne 6 yards ?",
        at: "2026-10-30T08:50:00",
      },
      {
        id: "m2",
        from: "ai",
        text: "Bonsoir Fatou ! Oui, l'ensemble pagne 6 yards multicolore est disponible à 9 500 FCFA. Combien en voulez-vous ?",
        at: "2026-10-30T08:51:00",
      },
      {
        id: "m3",
        from: "customer",
        text: "Et le pagne multicolore est lavable en machine ?",
        at: "2026-10-30T08:55:00",
      },
    ],
  },
  {
    id: "c3",
    customerName: "Mariam Touré",
    customerPhone: "+225 01 02 03 04 05",
    avatarColor: "bg-emerald-500",
    lastMessage: "Merci beaucoup, à demain pour la livraison",
    lastAt: "2026-10-30T07:45:00",
    status: "closed",
    unread: 0,
    needsTakeover: false,
    messages: [
      {
        id: "m1",
        from: "customer",
        text: "Bonjour, je veux commander le boubou bleu L",
        at: "2026-10-30T07:40:00",
      },
      {
        id: "m2",
        from: "ai",
        text: "Bonjour Mariam ! Boubou brodé homme bleu / L : 28 000 FCFA. Livraison Plateau : 1 000 FCFA. Total 29 000 FCFA. Je crée la commande.",
        at: "2026-10-30T07:41:00",
      },
      {
        id: "m3",
        from: "customer",
        text: "Merci beaucoup, à demain pour la livraison",
        at: "2026-10-30T07:45:00",
      },
    ],
  },
  {
    id: "c4",
    customerName: "Adama Cissé",
    customerPhone: "+225 07 11 22 33 44",
    avatarColor: "bg-violet-500",
    lastMessage: "La commande CMD-2026-1039 est confirmée ✅",
    lastAt: "2026-10-29T18:30:00",
    status: "human",
    unread: 0,
    needsTakeover: false,
    messages: [
      {
        id: "m1",
        from: "customer",
        text: "Je veux payer par wave pour ma commande",
        at: "2026-10-29T18:20:00",
      },
      {
        id: "merchant",
        from: "merchant",
        text: "Bonjour Adama, je vous envoie le lien Wave : wave.me/nova/elegance. Référence CMD-2026-1039.",
        at: "2026-10-29T18:25:00",
      },
      {
        id: "m3",
        from: "merchant",
        text: "La commande CMD-2026-1039 est confirmée ✅",
        at: "2026-10-29T18:30:00",
      },
    ],
  },
  {
    id: "c5",
    customerName: "Bintou Sow",
    customerPhone: "+225 05 44 55 66 77",
    avatarColor: "bg-pink-500",
    lastMessage: "Top, j'attends le livreur demain après-midi",
    lastAt: "2026-10-29T16:00:00",
    status: "human",
    unread: 0,
    needsTakeover: false,
    messages: [
      {
        id: "m1",
        from: "customer",
        text: "Vous livrez à Treichville ?",
        at: "2026-10-29T15:50:00",
      },
      {
        id: "m2",
        from: "merchant",
        text: "Oui Bintou, Treichville 1 500 FCFA, livreur prévu demain 14h-16h. La robe soirée satin est emballée.",
        at: "2026-10-29T15:55:00",
      },
      {
        id: "m3",
        from: "customer",
        text: "Top, j'attends le livreur demain après-midi",
        at: "2026-10-29T16:00:00",
      },
    ],
  },
  {
    id: "c6",
    customerName: "Hawa Traoré",
    customerPhone: "+225 01 23 45 67 89",
    avatarColor: "bg-cyan-500",
    lastMessage: "Ok tant pis, vous préviendrez quand dispo ?",
    lastAt: "2026-10-28T10:00:00",
    status: "ai",
    unread: 0,
    needsTakeover: true,
    messages: [
      {
        id: "m1",
        from: "customer",
        text: "Vous avez l'ensemble enfant fête en 4 ans ?",
        at: "2026-10-28T09:55:00",
      },
      {
        id: "m2",
        from: "ai",
        text: "Bonjour Hawa ! Il en reste 1 en 4 ans (12 000 FCFA). Souhaitez-vous le réserver ?",
        at: "2026-10-28T09:56:00",
      },
      {
        id: "m3",
        from: "customer",
        text: "Ok tant pis, vous préviendrez quand dispo ?",
        at: "2026-10-28T10:00:00",
      },
    ],
  },
  {
    id: "c7",
    customerName: "Koffi Yao",
    customerPhone: "+225 05 67 89 01 23",
    avatarColor: "bg-orange-500",
    lastMessage: "Bonjour, j'ai reçu le boubou, parfait merci !",
    lastAt: "2026-10-27T08:00:00",
    status: "closed",
    unread: 0,
    needsTakeover: false,
    messages: [
      {
        id: "m1",
        from: "customer",
        text: "Bonjour, j'ai reçu le boubou, parfait merci !",
        at: "2026-10-27T08:00:00",
      },
    ],
  },
  {
    id: "c8",
    customerName: "Adjoua Kouassi",
    customerPhone: "+225 07 23 45 67 89",
    avatarColor: "bg-lime-600",
    lastMessage: "Le livreur vient d'arriver, merci !",
    lastAt: "2026-10-26T12:00:00",
    status: "closed",
    unread: 0,
    needsTakeover: false,
    messages: [
      {
        id: "m1",
        from: "customer",
        text: "Le livreur vient d'arriver, merci !",
        at: "2026-10-26T12:00:00",
      },
    ],
  },
];

// ====== Clients ======

export type CustomerStatus = "prospect" | "client" | "regular";

export interface Customer {
  id: string;
  name: string;
  phone: string;
  ordersCount: number;
  totalSpent: number;
  lastOrderAt: string;
  status: CustomerStatus;
}

export const customers: Customer[] = [
  {
    id: "cu1",
    name: "Aminata Koffi",
    phone: "+225 07 08 12 34 56",
    ordersCount: 8,
    totalSpent: 145_000,
    lastOrderAt: "2026-10-30T09:24:00",
    status: "regular",
  },
  {
    id: "cu2",
    name: "Fatou Bamba",
    phone: "+225 05 06 78 90 12",
    ordersCount: 3,
    totalSpent: 58_500,
    lastOrderAt: "2026-10-30T08:15:00",
    status: "client",
  },
  {
    id: "cu3",
    name: "Mariam Touré",
    phone: "+225 01 02 03 04 05",
    ordersCount: 5,
    totalSpent: 102_000,
    lastOrderAt: "2026-10-30T07:42:00",
    status: "regular",
  },
  {
    id: "cu4",
    name: "Adama Cissé",
    phone: "+225 07 11 22 33 44",
    ordersCount: 4,
    totalSpent: 89_500,
    lastOrderAt: "2026-10-29T18:10:00",
    status: "client",
  },
  {
    id: "cu5",
    name: "Bintou Sow",
    phone: "+225 05 44 55 66 77",
    ordersCount: 2,
    totalSpent: 41_500,
    lastOrderAt: "2026-10-29T15:30:00",
    status: "client",
  },
  {
    id: "cu6",
    name: "Salimata Diabaté",
    phone: "+225 07 88 99 00 11",
    ordersCount: 6,
    totalSpent: 78_000,
    lastOrderAt: "2026-10-28T11:20:00",
    status: "regular",
  },
  {
    id: "cu7",
    name: "Hawa Traoré",
    phone: "+225 01 23 45 67 89",
    ordersCount: 1,
    totalSpent: 13_500,
    lastOrderAt: "2026-10-28T09:00:00",
    status: "client",
  },
  {
    id: "cu8",
    name: "Awa Bamba",
    phone: "+225 07 12 34 56 78",
    ordersCount: 7,
    totalSpent: 168_000,
    lastOrderAt: "2026-10-27T14:00:00",
    status: "regular",
  },
  {
    id: "cu9",
    name: "Koffi Yao",
    phone: "+225 05 67 89 01 23",
    ordersCount: 3,
    totalSpent: 67_000,
    lastOrderAt: "2026-10-26T16:30:00",
    status: "client",
  },
  {
    id: "cu10",
    name: "Adjoua Kouassi",
    phone: "+225 07 23 45 67 89",
    ordersCount: 5,
    totalSpent: 95_000,
    lastOrderAt: "2026-10-25T13:00:00",
    status: "regular",
  },
];

export const customerStatusLabel: Record<CustomerStatus, string> = {
  prospect: "Prospect",
  client: "Client",
  regular: "Récurrent",
};

// ====== Livraisons ======

export type DeliveryStatus =
  | "in_progress"
  | "scheduled"
  | "delivered"
  | "failed";

export interface Delivery {
  id: string;
  orderNumber: string;
  customerName: string;
  zone: string;
  address: string;
  driver: string;
  status: DeliveryStatus;
  scheduledAt?: string;
}

export const deliveries: Delivery[] = [
  {
    id: "d1",
    orderNumber: "CMD-2026-1038",
    customerName: "Bintou Sow",
    zone: "Treichville",
    address: "Treichville avenue 13, près du marché",
    driver: "Ibrahim K.",
    status: "in_progress",
  },
  {
    id: "d2",
    orderNumber: "CMD-2026-1039",
    customerName: "Adama Cissé",
    zone: "Marcory",
    address: "Marcory Zone 4, rue du Canal, villa 12B",
    driver: "Moussa T.",
    status: "in_progress",
  },
  {
    id: "d3",
    orderNumber: "CMD-2026-1042",
    customerName: "Aminata Koffi",
    zone: "Cocody",
    address: "Cocody Angré, 7e tranche, près du pont",
    driver: "—",
    status: "scheduled",
    scheduledAt: "2026-10-31T10:00:00",
  },
  {
    id: "d4",
    orderNumber: "CMD-2026-1041",
    customerName: "Fatou Bamba",
    zone: "Yopougon",
    address: "Yopougon Selmer, à côté de la pharmacie",
    driver: "—",
    status: "scheduled",
    scheduledAt: "2026-10-31T11:00:00",
  },
  {
    id: "d5",
    orderNumber: "CMD-2026-1037",
    customerName: "Salimata Diabaté",
    zone: "Abobo",
    address: "Abobo Belgique, à l'arrêt Gare",
    driver: "Ibrahim K.",
    status: "delivered",
    scheduledAt: "2026-10-29T10:00:00",
  },
  {
    id: "d6",
    orderNumber: "CMD-2026-1035",
    customerName: "Awa Bamba",
    zone: "Cocody",
    address: "Cocody Riviera Palmeraie, lot 24",
    driver: "Moussa T.",
    status: "delivered",
    scheduledAt: "2026-10-28T15:00:00",
  },
  {
    id: "d7",
    orderNumber: "CMD-2026-1036",
    customerName: "Hawa Traoré",
    zone: "Koumassi",
    address: "Koumassi Brillat, rue 12",
    driver: "Ibrahim K.",
    status: "failed",
    scheduledAt: "2026-10-28T17:00:00",
  },
];

export const deliveryStatusLabel: Record<DeliveryStatus, string> = {
  in_progress: "En cours",
  scheduled: "Planifiée",
  delivered: "Livrée",
  failed: "Échec",
};

// ====== Notifications ======

export interface Notification {
  id: string;
  type: "order" | "conversation" | "stock" | "subscription";
  title: string;
  description: string;
  at: string;
  read: boolean;
}

export const notifications: Notification[] = [
  {
    id: "n1",
    type: "conversation",
    title: "Conversation à reprendre",
    description: "Aminata Koffi attend une réponse depuis 4 min.",
    at: "2026-10-30T09:21:00",
    read: false,
  },
  {
    id: "n2",
    type: "order",
    title: "Nouvelle commande",
    description: "CMD-2026-1042 — 44 000 FCFA (Cocody).",
    at: "2026-10-30T09:24:00",
    read: false,
  },
  {
    id: "n3",
    type: "stock",
    title: "Stock faible",
    description: "Robe noire wax : 4 en stock (sous le seuil de 5).",
    at: "2026-10-30T08:00:00",
    read: false,
  },
  {
    id: "n4",
    type: "subscription",
    title: "Abonnement à renouveler",
    description: "Renouvellement le 15/11/2026 — 10 000 FCFA.",
    at: "2026-10-25T07:00:00",
    read: true,
  },
  {
    id: "n5",
    type: "order",
    title: "Commande livrée",
    description: "CMD-2026-1037 livrée à Salimata Diabaté.",
    at: "2026-10-29T11:30:00",
    read: true,
  },
];

// ====== Stats ======

export interface DailySale {
  day: string;
  label: string;
  ca: number;
  orders: number;
}

export const dailySales: DailySale[] = [
  { day: "2026-10-24", label: "Lun", ca: 30_000, orders: 1 },
  { day: "2026-10-25", label: "Mar", ca: 27_500, orders: 1 },
  { day: "2026-10-26", label: "Mer", ca: 29_000, orders: 1 },
  { day: "2026-10-27", label: "Jeu", ca: 50_500, orders: 1 },
  { day: "2026-10-28", label: "Ven", ca: 28_500, orders: 2 },
  { day: "2026-10-29", label: "Sam", ca: 56_000, orders: 2 },
  { day: "2026-10-30", label: "Dim", ca: 73_000, orders: 3 },
];

export const topProducts = [
  { name: "Robe noire wax", units: 4, revenue: 72_000 },
  { name: "Sac à main cuir", units: 3, revenue: 73_500 },
  { name: "Boubou brodé homme", units: 2, revenue: 56_000 },
  { name: "Ensemble pagne 6 yards", units: 5, revenue: 47_500 },
  { name: "Robe soirée satin", units: 2, revenue: 53_000 },
];

export const cancellationReasons = [
  { reason: "Rupture de stock", count: 2 },
  { reason: "Client absent à la livraison", count: 1 },
  { reason: "Taille incorrecte", count: 1 },
];

// ====== Abonnement ======

export interface SubscriptionPayment {
  id: string;
  date: string;
  amount: number;
  mode: "Wave" | "Orange Money" | "MTN MoMo" | "Carte bancaire";
  reference: string;
  status: "success" | "failed" | "pending";
}

export const subscriptionPayments: SubscriptionPayment[] = [
  {
    id: "sp1",
    date: "2026-10-15",
    amount: 10_000,
    mode: "Wave",
    reference: "WV-2026-1015-001",
    status: "success",
  },
  {
    id: "sp2",
    date: "2026-09-15",
    amount: 10_000,
    mode: "Orange Money",
    reference: "OM-2026-0915-442",
    status: "success",
  },
  {
    id: "sp3",
    date: "2026-08-15",
    amount: 10_000,
    mode: "Wave",
    reference: "WV-2026-0815-119",
    status: "success",
  },
  {
    id: "sp4",
    date: "2026-07-15",
    amount: 10_000,
    mode: "MTN MoMo",
    reference: "MM-2026-0715-778",
    status: "success",
  },
];

export const planUsage = {
  messagesUsed: 740,
  messagesQuota: 1000,
};

// ====== Équipe ======

export type TeamRole = "owner" | "manager" | "employee";
export type TeamStatus = "active" | "invited";

export interface TeamMember {
  id: string;
  name: string;
  email: string;
  role: TeamRole;
  permissions: string[];
  status: TeamStatus;
}

export const teamMembers: TeamMember[] = [
  {
    id: "tm1",
    name: "Awa Koné",
    email: "owner@boutique-demo.ci",
    role: "owner",
    permissions: ["Toutes les permissions"],
    status: "active",
  },
  {
    id: "tm2",
    name: "Mariam Cissé",
    email: "mariam@boutique-demo.ci",
    role: "manager",
    permissions: ["Catalogue", "Stock", "Commandes", "Clients"],
    status: "active",
  },
  {
    id: "tm3",
    name: "Ibrahim Konaté",
    email: "ibrahim@boutique-demo.ci",
    role: "employee",
    permissions: ["Commandes", "Livraisons"],
    status: "active",
  },
  {
    id: "tm4",
    name: "Salimata Bamba",
    email: "salimata@boutique-demo.ci",
    role: "employee",
    permissions: ["Conversations"],
    status: "invited",
  },
];

export const teamRoleLabel: Record<TeamRole, string> = {
  owner: "Propriétaire",
  manager: "Gérant",
  employee: "Employé",
};

// ====== Home dashboard (ch. 4.9) ======

export const homeKpis = {
  conversations: { total: 43, inProgress: 12 },
  orders: 12,
  sales: 286_000,
  deliveries: { inProgress: 6 },
  lowStock: 8,
  aiHandled: 38,
  aiRate: 88, // %
  conversionRate: 23, // %
  aiCostMonth: 12_450,
  conversationsToReprocess: 5,
  ordersToConfirm: 4,
};

export const conversationsToReprocess = conversations
  .filter((c) => c.needsTakeover)
  .slice(0, 5);

export const ordersToConfirm = orders
  .filter((o) => o.status === "pending" || o.status === "en_attente_confirmation")
  .slice(0, 4);

export const lowStockItems = stockRows
  .filter((s) => stockStatus(s) !== "ok")
  .slice(0, 8);
