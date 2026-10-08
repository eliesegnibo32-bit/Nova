/**
 * NOVA — API client
 *
 * En production : appelle directement le backend déployé sur Render
 *   (NEXT_PUBLIC_API_URL ou fallback hardcoded vers nova-api-kiq6.onrender.com)
 *
 * En dev local : utilise la gateway Caddy avec XTransformPort=8080
 *   qui route vers le backend Go (port 8080).
 */

import { demoShop } from "@/lib/mock-data";

const API_PORT = "8080";
// Hardcoded fallback — ensures the URL works even if the env var isn't set at build time.
const PROD_API_URL = process.env.NEXT_PUBLIC_API_URL || "https://nova-api-kiq6.onrender.com";

export class ApiError extends Error {
  status: number;
  details?: unknown;
  constructor(message: string, status: number, details?: unknown) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.details = details;
  }
}

function buildUrl(path: string): string {
  // Production: appelle directement le backend Render
  if (PROD_API_URL) {
    const base = PROD_API_URL.replace(/\/$/, "");
    return `${base}${path.startsWith("/") ? path : "/" + path}`;
  }
  // Dev local : gateway Caddy avec XTransformPort
  if (path.includes("XTransformPort=")) return path;
  return path.includes("?")
    ? `${path}&XTransformPort=${API_PORT}`
    : `${path}?XTransformPort=${API_PORT}`;
}

/**
 * Récupère le shopId courant (depuis le store persisté dans localStorage).
 * Les endpoints du backend Go exigent `/api/shops/{shopId}/...`.
 * En mode démo (aucun shopId persisté), on retombe sur la boutique démo.
 */
function getShopId(): string {
  if (typeof window !== "undefined") {
    try {
      const raw = window.localStorage.getItem("nova-auth");
      if (raw) {
        const parsed = JSON.parse(raw) as {
          state?: { currentShopId?: string | null };
        };
        const id = parsed?.state?.currentShopId;
        if (id) return id;
      }
    } catch {
      // ignore — fallback to demo shop
    }
  }
  return demoShop.id;
}

/**
 * Construit un chemin avec le shopId injecté.
 * Ex: shopPath("/options") → "/api/shops/shp_demo_001/options"
 */
function shopPath(sub: string): string {
  return `/api/shops/${getShopId()}${sub}`;
}

/** Exporté pour les composants qui doivent construire des URLs shop-scoped. */
export { shopPath };

export async function apiFetch<T>(
  path: string,
  options?: RequestInit,
): Promise<T> {
  const url = buildUrl(path);
  const res = await fetch(url, {
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
      ...options?.headers,
    },
    ...options,
  });

  const contentType = res.headers.get("content-type") || "";
  const isJson = contentType.includes("application/json");
  const body = isJson ? await res.json().catch(() => null) : null;

  if (!res.ok) {
    const message =
      (body && (body.error || body.message)) || res.statusText || "Erreur API";
    throw new ApiError(message, res.status, body);
  }

  // 204 / empty responses
  if (res.status === 204 || body === null) return undefined as T;
  return body as T;
}

/**
 * Variante de `apiFetch` pour l'upload multipart/form-data (photos vers R2).
 * Ne fixe PAS le Content-Type : le navigateur s'en charge (boundary multipart).
 */
export async function apiUpload<T>(
  path: string,
  formData: FormData,
  options?: RequestInit,
): Promise<T> {
  const url = buildUrl(path);
  const res = await fetch(url, {
    credentials: "include",
    ...options,
    body: formData,
  });

  const contentType = res.headers.get("content-type") || "";
  const isJson = contentType.includes("application/json");
  const body = isJson ? await res.json().catch(() => null) : null;

  if (!res.ok) {
    const message =
      (body && (body.error || body.message)) || res.statusText || "Erreur API";
    throw new ApiError(message, res.status, body);
  }

  if (res.status === 204 || body === null) return undefined as T;
  return body as T;
}

// ====== Types (alignés sur les stubs du backend Go, voir task 3) ======

export interface AuthUser {
  id: string;
  email: string;
  fullName: string;
  role: string;
  phone?: string;
  avatar_url?: string | null;
  two_factor_enabled?: boolean;
}

/** Payload d'inscription aligné sur `POST /api/auth/register` du backend Go. */
export interface RegisterPayload {
  email: string;
  password: string;
  full_name: string;
  phone: string;
  shop_name?: string;
}

/** Réponse de `POST /api/auth/2fa/setup`. */
export interface TwoFactorSetup {
  secret: string;
  qr_data_uri: string;
  provisioning_uri: string;
}

export interface Shop {
  id: string;
  name: string;
  slug: string;
  country: string;
  currency: string;
  status: "active" | "pending" | "suspended" | "draft";
  ownerName?: string;
  owner_email?: string;
  createdAt?: string;
  // Champs optionnels utilisés par le formulaire de création de boutique.
  phone?: string | null;
  whatsapp_number?: string | null;
  address?: string | null;
  commune?: string | null;
  description?: string | null;
  hours?: Record<string, { open?: string; close?: string; closed?: boolean }> | null;
  categories?: string[] | null;
  accepted_payment_modes?: string[] | null;
  delivery_zones?: Array<{
    name: string;
    fee: number;
    estimated_delay?: string;
  }> | null;
  plan?: string | null;
}

// ====== API namespaces ======
// Note: le backend Go renvoie actuellement 501 pour la plupart des routes.
// Les stubs ci-dessous existent pour faciliter le branchement futur et
// pouvoir basculer depuis les données mock sans toucher les composants.

export const authApi = {
  me: () => apiFetch<AuthUser | { user: AuthUser; shopId: string | null }>("/api/auth/me"),
  login: (email: string, password: string) =>
    apiFetch<{ user: AuthUser }>("/api/auth/login", {
      method: "POST",
      body: JSON.stringify({ email, password }),
    }),
  register: (payload: RegisterPayload) =>
    apiFetch<{ user: AuthUser }>("/api/auth/register", {
      method: "POST",
      body: JSON.stringify(payload),
    }),
  /** Met à jour le profil courant (nom complet, téléphone, avatar). */
  updateProfile: (payload: {
    full_name?: string;
    phone?: string;
    avatar_url?: string | null;
  }) =>
    apiFetch<{ user: AuthUser }>("/api/auth/me", {
      method: "PATCH",
      body: JSON.stringify(payload),
    }),
  changePassword: (oldPassword: string, newPassword: string) =>
    apiFetch<{ ok: true }>("/api/auth/change-password", {
      method: "POST",
      body: JSON.stringify({ old_password: oldPassword, new_password: newPassword }),
    }),
  setup2FA: () =>
    apiFetch<TwoFactorSetup>("/api/auth/2fa/setup", { method: "POST" }),
  confirm2FA: (code: string) =>
    apiFetch<{ ok: true }>("/api/auth/2fa/confirm", {
      method: "POST",
      body: JSON.stringify({ code }),
    }),
  disable2FA: (code: string) =>
    apiFetch<{ ok: true }>("/api/auth/2fa/disable", {
      method: "POST",
      body: JSON.stringify({ code }),
    }),
  /** Demande un code de réinitialisation (envoyé par e-mail / WhatsApp / SMS).
   *  En dev, le backend renvoie `dev_token` pour permettre de tester le flux. */
  requestPasswordReset: (email: string) =>
    apiFetch<{ message: string; dev_token?: string }>(
      "/api/auth/password-reset/request",
      {
        method: "POST",
        body: JSON.stringify({ email }),
      },
    ),
  confirmPasswordReset: (token: string, newPassword: string) =>
    apiFetch<{ ok: true }>("/api/auth/password-reset/confirm", {
      method: "POST",
      body: JSON.stringify({ token, new_password: newPassword }),
    }),
  logout: () => apiFetch<{ ok: true }>("/api/auth/logout", { method: "POST" }),
};

export const shopsApi = {
  list: () => apiFetch<{ shops: Shop[] }>("/api/shops"),
  get: (id: string) => apiFetch<{ shop: Shop }>(`/api/shops/${id}`),
  create: (payload: Partial<Shop>) =>
    apiFetch<{ shop: Shop; subscription?: unknown }>("/api/shops", {
      method: "POST",
      body: JSON.stringify(payload),
    }),
  update: (id: string, payload: Partial<Shop>) =>
    apiFetch<{ shop: Shop }>(`/api/shops/${id}`, {
      method: "PUT",
      body: JSON.stringify(payload),
    }),
  activate: (id: string) =>
    apiFetch<{ shop: Shop }>(`/api/shops/${id}/activate`, { method: "POST" }),
  switch: (shopId: string) =>
    apiFetch<{ ok: true; shop_id: string }>("/api/shops/switch", {
      method: "POST",
      body: JSON.stringify({ shop_id: shopId }),
    }),
};

// ====== Produits & variantes ======

export type ProductStatus = "published" | "draft";

export interface ProductVariant {
  id?: string;
  label: string;
  size?: string;
  color?: string;
  sku?: string;
  price?: number;
  stock: number;
  threshold?: number;
  stock_mode?: StockMode;
}

export interface ProductFull {
  id: string;
  name: string;
  description?: string;
  category: string;
  brand?: string;
  price: number;
  status: ProductStatus;
  image_url?: string | null;
  variants?: ProductVariant[];
}

export interface ProductInput {
  name: string;
  description?: string;
  category: string;
  brand?: string;
  price: number;
  status?: ProductStatus;
  image_url?: string | null;
  variants?: Array<{
    label: string;
    size?: string;
    color?: string;
    sku?: string;
    price?: number;
    stock: number;
    threshold?: number;
    stock_mode?: StockMode;
  }>;
}

export const productsApi = {
  list: (params?: { q?: string; category?: string; status?: string }) => {
    const qs = new URLSearchParams();
    if (params?.q) qs.set("q", params.q);
    if (params?.category) qs.set("category", params.category);
    if (params?.status) qs.set("status", params.status);
    const sub = qs.toString() ? `/products?${qs.toString()}` : "/products";
    return apiFetch<{ products: ProductFull[] }>(shopPath(sub));
  },
  get: (id: string) =>
    apiFetch<{ product: ProductFull }>(shopPath(`/products/${id}`)),
  create: (payload: ProductInput) =>
    apiFetch<{ product: ProductFull }>(shopPath("/products"), {
      method: "POST",
      body: JSON.stringify(payload),
    }),
  update: (id: string, payload: Partial<ProductInput>) =>
    apiFetch<{ product: ProductFull }>(shopPath(`/products/${id}`), {
      method: "PATCH",
      body: JSON.stringify(payload),
    }),
  remove: (id: string) =>
    apiFetch<void>(shopPath(`/products/${id}`), { method: "DELETE" }),
  delete: (id: string) =>
    apiFetch<void>(shopPath(`/products/${id}`), { method: "DELETE" }),
  publish: (id: string) =>
    apiFetch<{ product: ProductFull }>(shopPath(`/products/${id}/publish`), {
      method: "POST",
    }),
  archive: (id: string) =>
    apiFetch<{ product: ProductFull }>(shopPath(`/products/${id}/archive`), {
      method: "POST",
    }),
};

export const ordersApi = {
  list: (params?: { status?: string; q?: string }) => {
    const qs = new URLSearchParams();
    if (params?.status) qs.set("status", params.status);
    if (params?.q) qs.set("q", params.q);
    const path = qs.toString() ? `/api/orders?${qs.toString()}` : "/api/orders";
    return apiFetch<{ orders: unknown[] }>(path);
  },
  get: (id: string) => apiFetch<{ order: unknown }>(`/api/orders/${id}`),
  updateStatus: (id: string, status: string) =>
    apiFetch<{ order: unknown }>(`/api/orders/${id}/status`, {
      method: "PATCH",
      body: JSON.stringify({ status }),
    }),
};

export const conversationsApi = {
  list: () => apiFetch<{ conversations: unknown[] }>("/api/conversations"),
  messages: (id: string) =>
    apiFetch<{ messages: unknown[] }>(`/api/conversations/${id}/messages`),
  takeOver: (id: string) =>
    apiFetch<{ ok: true }>(`/api/conversations/${id}/takeover`, { method: "POST" }),
  release: (id: string) =>
    apiFetch<{ ok: true }>(`/api/conversations/${id}/release`, { method: "POST" }),
};

export const customersApi = {
  list: () => apiFetch<{ customers: unknown[] }>("/api/customers"),
};

export const deliveriesApi = {
  list: (status?: string) => {
    const path = status ? `/api/deliveries?status=${status}` : "/api/deliveries";
    return apiFetch<{ deliveries: unknown[] }>(path);
  },
};

// ====== Stock / Inventory ======

export interface InventoryItem {
  id: string;
  variant_id: string;
  product_id?: string;
  product_name?: string;
  variant_label?: string;
  on_hand: number;
  reserved: number;
  available: number;
  threshold?: number;
  stock_mode?: StockMode;
  image_url?: string | null;
}

export interface StockMovement {
  id: string;
  variant_id: string;
  type: string; // "receive" | "adjust" | "reserve" | "release" | ...
  quantity: number; // signé
  reason?: string;
  created_at: string;
}

export const stockApi = {
  list: () => apiFetch<{ items: InventoryItem[] }>(shopPath("/inventory")),
  get: (variantId: string) =>
    apiFetch<{ inventory: InventoryItem }>(shopPath(`/inventory/${variantId}`)),
  adjust: (variantId: string, delta: number, reason: string) =>
    apiFetch<{ inventory: InventoryItem }>(
      shopPath(`/inventory/${variantId}/adjust`),
      { method: "POST", body: JSON.stringify({ delta, reason }) },
    ),
  receive: (variantId: string, quantity: number, reason: string) =>
    apiFetch<{ inventory: InventoryItem }>(
      shopPath(`/inventory/${variantId}/receive`),
      { method: "POST", body: JSON.stringify({ quantity, reason }) },
    ),
  movements: (variantId?: string) =>
    apiFetch<{ movements: StockMovement[] }>(
      variantId
        ? shopPath(`/inventory/${variantId}/movements`)
        : shopPath("/inventory/movements"),
    ),
  stats: () => apiFetch<unknown>(shopPath("/inventory/stats")),
};

export const statsApi = {
  overview: () => apiFetch<{ stats: unknown }>("/api/stats/overview"),
};

export const subscriptionApi = {
  current: () => apiFetch<{ subscription: unknown }>("/api/subscription"),
  pay: () => apiFetch<{ ok: true }>("/api/subscription/pay", { method: "POST" }),
};

export const teamApi = {
  list: () => apiFetch<{ members: unknown[] }>("/api/team"),
  invite: (payload: unknown) =>
    apiFetch<{ member: unknown }>("/api/team/invite", {
      method: "POST",
      body: JSON.stringify(payload),
    }),
};

// ====== Plats & Options (plats / accompagnements / boissons) ======

export type ProductOptionType = "plat" | "accompagnement" | "boisson";
export type StockMode = "quantite" | "epuise" | "illimite";

export interface ProductOption {
  id: string;
  type: ProductOptionType;
  name: string;
  price: number;
  price_label?: string;
  stock_mode: StockMode;
  stock_qty: number | null;
  active: boolean;
  image_url?: string | null;
}

export interface ProductOptionInput {
  type: ProductOptionType;
  name: string;
  price: number;
  stock_mode: StockMode;
  stock_qty?: number | null;
  active?: boolean;
  image_url?: string | null;
}

export const optionsApi = {
  list: (type?: ProductOptionType | string) => {
    const path = type
      ? shopPath(`/options?type=${encodeURIComponent(type)}`)
      : shopPath("/options");
    return apiFetch<{ options: ProductOption[]; total: number }>(path);
  },
  create: (payload: ProductOptionInput) =>
    apiFetch<ProductOption>(shopPath("/options"), {
      method: "POST",
      body: JSON.stringify(payload),
    }),
  update: (id: string, payload: Partial<ProductOptionInput>) =>
    apiFetch<ProductOption>(shopPath(`/options/${id}`), {
      method: "PATCH",
      body: JSON.stringify(payload),
    }),
  remove: (id: string) =>
    apiFetch<{ ok: true } | void>(shopPath(`/options/${id}`), {
      method: "DELETE",
    }),
};

// ====== Configuration paiement =====

export type PaymentMode =
  | "paiement_livraison"
  | "paiement_avance"
  | "paiement_integral";

export type PaymentMethod = "wave" | "moov" | "orange" | "mtn";

export interface PaymentConfig {
  shop_id: string;
  mode: PaymentMode;
  delay_minutes: number;
  advance_amount?: number | null;
  wave_link?: string | null;
  wave_number?: string | null;
  moov_number?: string | null;
  orange_number?: string | null;
  mtn_number?: string | null;
  active_methods: PaymentMethod[];
}

export interface PaymentConfigInput {
  mode: PaymentMode;
  delay_minutes: number;
  advance_amount?: number | null;
  wave_link?: string | null;
  wave_number?: string | null;
  moov_number?: string | null;
  orange_number?: string | null;
  mtn_number?: string | null;
  active_methods: PaymentMethod[];
}

export const paymentConfigApi = {
  get: () => apiFetch<PaymentConfig>(shopPath("/payment-config")),
  update: (payload: PaymentConfigInput) =>
    apiFetch<PaymentConfig>(shopPath("/payment-config"), {
      method: "PUT",
      body: JSON.stringify(payload),
    }),
};

// ====== Workflow des commandes (statuts étendus + endpoints) =====

export type OrderV3Status =
  | "en_attente_confirmation"
  | "en_attente_paiement"
  | "paiement_signalé"
  | "en_cours"
  | "prete"
  | "terminee"
  | "refusee"
  | "annulee";

export interface OrderV3 {
  id: string;
  number?: string;
  status: OrderV3Status;
  [k: string]: unknown;
}

export const ordersV3Api = {
  merchantConfirm: (id: string) =>
    apiFetch<OrderV3 | unknown>(shopPath(`/orders/${id}/merchant-confirm`), {
      method: "POST",
    }),
  signalPayment: (id: string) =>
    apiFetch<OrderV3 | unknown>(shopPath(`/orders/${id}/signal-payment`), {
      method: "POST",
    }),
  confirmPayment: (id: string) =>
    apiFetch<OrderV3 | unknown>(shopPath(`/orders/${id}/confirm-payment`), {
      method: "POST",
    }),
  refusePayment: (id: string) =>
    apiFetch<OrderV3 | unknown>(shopPath(`/orders/${id}/refuse-payment`), {
      method: "POST",
    }),
  markReady: (id: string) =>
    apiFetch<OrderV3 | unknown>(shopPath(`/orders/${id}/ready`), {
      method: "POST",
    }),
  complete: (id: string) =>
    apiFetch<OrderV3 | unknown>(shopPath(`/orders/${id}/complete`), {
      method: "POST",
    }),
  cancel: (id: string) =>
    apiFetch<OrderV3 | unknown>(shopPath(`/orders/${id}/cancel-v3`), {
      method: "POST",
    }),
};

// ====== Upload photos (vers R2 via le backend) =====

export interface UploadResult {
  key: string;
  url: string;
  size: number;
  content_type: string;
}

export const uploadApi = {
  /** Téléverse une image produit (multipart/form-data) vers R2. */
  product: (file: File, category = "product") => {
    const fd = new FormData();
    fd.append("file", file);
    fd.append("category", category);
    return apiUpload<UploadResult>(shopPath("/upload/product"), fd);
  },
  /** Téléverse une image plat/option (même endpoint, catégorie différente). */
  option: (file: File) => uploadApi.product(file, "option"),
};

// Le type `StockMode` est défini dans la section « Plats & Options » ci-dessus.
