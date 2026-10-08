"use client";

import * as React from "react";
import { Loader2 } from "lucide-react";
import { motion } from "framer-motion";

import { useAuthStore } from "@/stores/auth-store";
import { LoginScreen } from "@/components/auth/login-screen";
import { DashboardShell } from "@/components/dashboard/shell";
import { OnboardingView } from "@/components/auth/onboarding-view";
import { NovaLogo } from "@/components/ui/nova-logo";

export default function Home() {
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);
  const isLoading = useAuthStore((s) => s.isLoading);
  const hydrate = useAuthStore((s) => s.hydrate);
  const hydrated = useAuthStore((s) => s.hydrated);
  const currentShopId = useAuthStore((s) => s.currentShopId);

  // Tente d'hydrater la session au mount (validité du cookie /api/auth/me).
  React.useEffect(() => {
    hydrate();
  }, [hydrate]);

  // En attendant que persist réhydrate le store, on montre un loader.
  const showLoader = !hydrated || isLoading;

  // Si l'utilisateur est connecté mais n'a PAS de boutique (nouveau compte),
  // on affiche la page d'onboarding pour créer sa première boutique.
  const needsOnboarding = isAuthenticated && !currentShopId;

  return (
    <div className="flex min-h-screen flex-col bg-background">
      {showLoader ? (
        <FullPageLoader />
      ) : needsOnboarding ? (
        <OnboardingView />
      ) : isAuthenticated ? (
        <DashboardShell />
      ) : (
        <LoginScreen />
      )}
    </div>
  );
}

function FullPageLoader() {
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-4 p-6">
      <motion.div
        initial={{ opacity: 0, scale: 0.9 }}
        animate={{ opacity: 1, scale: 1 }}
        transition={{ duration: 0.4 }}
        className="flex flex-col items-center gap-3"
      >
        <NovaLogo size={56} withWordmark={false} />
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="size-4 animate-spin text-brand" />
          Chargement de NOVA…
        </div>
      </motion.div>
    </div>
  );
}
