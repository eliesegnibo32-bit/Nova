/**
 * NOVA — UI store (vue courante, sidebar mobile).
 * Gère la navigation côté client dans la SPA unique (src/app/page.tsx).
 */

import { create } from "zustand";

export type ViewKey =
  | "home"
  | "products"
  | "stock"
  | "orders"
  | "conversations"
  | "customers"
  | "payment"
  | "stats"
  | "subscription"
  | "team"
  | "profile"
  // Administration (super_admin / admin)
  | "admin-overview"
  | "admin-shops"
  | "admin-users"
  | "admin-revenue"
  | "admin-monitoring"
  | "admin-support"
  | "admin-audit"
  | "admin-settings";

export const viewLabels: Record<ViewKey, string> = {
  home: "Accueil",
  products: "Produits",
  stock: "Stock",
  orders: "Commandes",
  conversations: "Conversations",
  customers: "Clients",
  payment: "Paiement",
  stats: "Statistiques",
  subscription: "Abonnement",
  team: "Équipe",
  profile: "Profil",
  "admin-overview": "Vue d'ensemble",
  "admin-shops": "Boutiques",
  "admin-users": "Utilisateurs",
  "admin-revenue": "Revenus",
  "admin-monitoring": "Supervision",
  "admin-support": "Support",
  "admin-audit": "Journal d'audit",
  "admin-settings": "Paramètres",
};

/** Vues réservées aux rôles plateforme (super_admin / admin). */
export const ADMIN_VIEWS: ViewKey[] = [
  "admin-overview",
  "admin-shops",
  "admin-users",
  "admin-revenue",
  "admin-monitoring",
  "admin-support",
  "admin-audit",
  "admin-settings",
];

/** Onglets disponibles dans la vue Profile (pour pré-positionnement). */
export type ProfileTab =
  | "infos"
  | "securite"
  | "2fa"
  | "parametres";

interface UIState {
  currentView: ViewKey;
  sidebarOpen: boolean; // drawer mobile
  profileTab: ProfileTab; // onglet actif dans ProfileView
  setCurrentView: (v: ViewKey) => void;
  setSidebarOpen: (open: boolean) => void;
  toggleSidebar: () => void;
  setProfileTab: (tab: ProfileTab) => void;
}

export const useUIStore = create<UIState>((set) => ({
  currentView: "home",
  sidebarOpen: false,
  profileTab: "infos",
  setCurrentView: (v) => set({ currentView: v, sidebarOpen: false }),
  setSidebarOpen: (open) => set({ sidebarOpen: open }),
  toggleSidebar: () => set((s) => ({ sidebarOpen: !s.sidebarOpen })),
  setProfileTab: (tab) => set({ profileTab: tab }),
}));
