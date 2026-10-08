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
  isAuthenticated: boolean;
  isLoading: boolean; // hydratation initiale
  hydrated: boolean; // flag persist réhydraté
  setUser: (user: AuthUser | null) => void;
  setShop: (shopId: string | null) => void;
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
      isAuthenticated: false,
      isLoading: true,
      hydrated: false,
      setUser: (user) =>
        set({
          user,
          isAuthenticated: !!user,
          isLoading: false,
        }),
      setShop: (shopId) => set({ currentShopId: shopId }),
      setLoading: (loading) => set({ isLoading: loading }),
      loginDemo: () =>
        set({
          user: demoUser,
          currentShopId: demoShop.id,
          isAuthenticated: true,
          isLoading: false,
        }),
      logout: async () => {
        try {
          await authApi.logout();
        } catch {
          // ignore — backend peut être 501 ou indisponible
        }
        set({ user: null, currentShopId: null, isAuthenticated: false });
      },
      hydrate: async () => {
        // Si déjà authentifié via persist, on tente de valider via /me
        const state = get();
        if (state.user) {
          try {
            const res = await authApi.me();
            const user =
              res && "user" in (res as object)
                ? ((res as { user: AuthUser }).user)
                : (res as AuthUser);
            if (user) {
              set({ user, isAuthenticated: true, isLoading: false });
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
        isAuthenticated: s.isAuthenticated,
      }),
      onRehydrateStorage: () => (state) => {
        if (state) state.hydrated = true;
      },
    },
  ),
);
