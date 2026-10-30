/**
 * NOVA — Auth store (Zustand + persist localStorage).
 *
 * Le backend Go renvoie actuellement 501 sur /api/auth/login ; le bouton
 * "Mode démo" de l'écran de connexion alimente ce store avec l'utilisateur
 * de démonstration (demoUser) sans appel réseau.
 */

import { create } from "zustand";
import { persist, createJSONStorage } from "zustand/middleware";
import { authApi, type AuthUser } from "@/lib/api";
import { demoUser, demoShop } from "@/lib/mock-data";

interface AuthState {
  user: AuthUser | null;
  currentShopId: string | null;
  currentShopName: string | null;
  currentShopSlug: string | null;
  isAuthenticated: boolean;
  isLoading: boolean;
  isDemo: boolean;
  hydrated: boolean; // flag persist réhydraté
  setUser: (user: AuthUser | null) => void;
  setShop: (shopId: string | null, shopName?: string | null, shopSlug?: string | null) => void;
  setLoading: (loading: boolean) => void;
  loginDemo: () => void;
  logout: () => Promise<void>;
  hydrate: () => Promise<void>;
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set, get) => ({
      user: null,
      currentShopId: null,
      currentShopName: null,
      currentShopSlug: null,
      isAuthenticated: false,
      isLoading: true,
      isDemo: false,
      hydrated: false,
      setUser: (user) =>
        set({
          user,
          isAuthenticated: !!user,
          isLoading: false,
          isDemo: false,
        }),
      setShop: (shopId, shopName = null, shopSlug = null) =>
        set({
          currentShopId: shopId,
          currentShopName: shopName,
          currentShopSlug: shopSlug,
        }),
      setLoading: (loading) => set({ isLoading: loading }),
      loginDemo: () =>
        set({
          user: demoUser,
          currentShopId: demoShop.id,
          currentShopName: demoShop.name,
          currentShopSlug: demoShop.slug,
          isAuthenticated: true,
          isLoading: false,
          isDemo: true,
        }),
      logout: async () => {
        try {
          await authApi.logout();
        } catch {
          // ignore — backend peut être 501 ou indisponible
        }
        set({ user: null, currentShopId: null, currentShopName: null, currentShopSlug: null, isAuthenticated: false, isDemo: false });
      },
      hydrate: async () => {
        // Si déjà authentifié via persist, on tente de valider via /me
        const state = get();
        if (state.user && !state.isDemo) {
          try {
            const res = await authApi.me() as any;
            const user =
              res && "user" in (res as object)
                ? ((res as { user: AuthUser }).user)
                : (res as AuthUser);
            if (user) {
              // Récupère le shop actif ou le premier shop disponible
              const shops = res?.shops || [];
              const currentShopId = res?.currentShopId || res?.shopId || null;
              if (currentShopId) {
                // Le backend a déjà un shop actif en session
                const shop = shops.find((s: any) => (s.shop_id || s.id) === currentShopId) || shops[0];
                set({
                  user,
                  isAuthenticated: true,
                  isLoading: false,
                  currentShopId,
                  currentShopName: shop?.shop_name || shop?.name || state.currentShopName,
                  currentShopSlug: shop?.shop_slug || shop?.slug || state.currentShopSlug,
                });
              } else if (shops.length > 0 && !state.currentShopId) {
                // Pas de shop actif en session backend → on switch vers le 1er
                const shopId = shops[0]?.shop_id || shops[0]?.id;
                const shopName = shops[0]?.shop_name || shops[0]?.name;
                const shopSlug = shops[0]?.shop_slug || shops[0]?.slug;
                try {
                  const { shopsApi } = await import("@/lib/api");
                  await shopsApi.switch(shopId);
                } catch {
                  // Non bloquant
                }
                set({
                  user,
                  isAuthenticated: true,
                  isLoading: false,
                  currentShopId: shopId,
                  currentShopName: shopName,
                  currentShopSlug: shopSlug,
                });
              } else {
                set({ user, isAuthenticated: true, isLoading: false });
              }
              return;
            }
          } catch {
            // cookie expiré / backend down → on garde la session persistée
            // (utile pour la démo) plutôt que de déconnecter brutalement.
          }
        }
        set({ isLoading: false });
      },
    }),
    {
      name: "nova-auth",
      storage: createJSONStorage(() => localStorage),
      partialize: (s) => ({
        user: s.user,
        currentShopId: s.currentShopId,
        currentShopName: s.currentShopName,
        currentShopSlug: s.currentShopSlug,
        isAuthenticated: s.isAuthenticated,
        isDemo: s.isDemo,
      }),
      onRehydrateStorage: () => (state) => {
        if (state) state.hydrated = true;
      },
    },
  ),
);
