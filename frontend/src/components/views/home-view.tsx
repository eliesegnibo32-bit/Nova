"use client";

import * as React from "react";
import {
  MessageCircle,
  ShoppingCart,
  Banknote,
  Truck,
  ArrowRight,
  Bell,
  Package,
  AlertTriangle,
  Clock,
} from "lucide-react";
import { Card } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { useAuthStore } from "@/stores/auth-store";
import { useUIStore } from "@/stores/ui-store";
import { homeKpis, demoShop } from "@/lib/mock-data";
import { formatFCFA } from "@/lib/format";

export function HomeView() {
  const user = useAuthStore((s) => s.user);
  const isDemo = useAuthStore((s) => s.isDemo);
  const setCurrentView = useUIStore((s) => s.setCurrentView);

  if (isDemo) {
    // Mode démo — affiche les KPIs fictifs
    return (
      <div className="space-y-6">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h2 className="text-xl font-bold tracking-tight sm:text-2xl">
              Bonjour {user?.fullName?.split(" ")[0] || "👋"}
            </h2>
            <p className="text-sm text-muted-foreground">
              Voici l'activité de votre boutique {demoShop.name} aujourd'hui.
            </p>
          </div>
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <Clock className="size-3.5" />
            {new Date().toLocaleDateString("fr-FR", { weekday: "long", day: "numeric", month: "long", year: "numeric" })}
          </div>
        </div>

        <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
          {[
            { icon: MessageCircle, label: "Conversations", value: String(homeKpis.conversations.total), sub: `${homeKpis.conversations.inProgress} en cours`, color: "text-emerald-600" },
            { icon: ShoppingCart, label: "Commandes", value: String(homeKpis.orders), sub: "à confirmer", color: "text-amber-600" },
            { icon: Banknote, label: "Ventes", value: formatFCFA(homeKpis.sales), sub: "aujourd'hui", color: "text-emerald-600" },
            { icon: Truck, label: "Livraisons", value: String(homeKpis.deliveries.inProgress), sub: "en cours", color: "text-blue-600" },
          ].map((kpi) => (
            <Card key={kpi.label} className="p-4">
              <div className="flex items-center gap-3">
                <div className="flex size-10 items-center justify-center rounded-lg bg-muted">
                  <kpi.icon className={`size-5 ${kpi.color}`} />
                </div>
                <div>
                  <div className="text-2xl font-bold">{kpi.value}</div>
                  <div className="text-xs text-muted-foreground">{kpi.label}</div>
                </div>
              </div>
            </Card>
          ))}
        </div>

        <Card className="p-4">
          <h4 className="mb-3 text-sm font-semibold">À traiter</h4>
          <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
            <div className="space-y-2">
              <div className="text-xs text-muted-foreground">Conversations à reprendre (5)</div>
              <Button size="sm" variant="outline" onClick={() => setCurrentView("conversations")}>Voir tout</Button>
            </div>
            <div className="space-y-2">
              <div className="text-xs text-muted-foreground">Commandes à confirmer (4)</div>
              <Button size="sm" variant="outline" onClick={() => setCurrentView("orders")}>Voir tout</Button>
            </div>
            <div className="space-y-2">
              <div className="text-xs text-muted-foreground">Stocks faibles (8)</div>
              <Button size="sm" variant="outline" onClick={() => setCurrentView("stock")}>Voir tout</Button>
            </div>
          </div>
        </Card>

        <Card className="p-4">
          <div className="flex items-center justify-between">
            <div>
              <div className="text-sm font-medium">Échéance abonnement</div>
              <div className="text-xs text-muted-foreground">Renouvellement le 15/11/2026 — {formatFCFA(demoShop.planPrice)} / mois</div>
            </div>
            <Button size="sm" onClick={() => setCurrentView("subscription")}>Payer maintenant</Button>
          </div>
        </Card>
      </div>
    );
  }

  // Vrai compte — dashboard vide
  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-xl font-bold tracking-tight sm:text-2xl">
            Bonjour {user?.fullName?.split(" ")[0] || "👋"}
          </h2>
          <p className="text-sm text-muted-foreground">Bienvenue dans votre tableau de bord.</p>
        </div>
      </div>

      <Card className="p-8 text-center">
        <Package className="mx-auto mb-4 size-12 text-muted-foreground" />
        <h3 className="mb-2 text-lg font-semibold">Votre boutique est prête !</h3>
        <p className="mb-6 text-sm text-muted-foreground">
          Commencez par ajouter vos produits pour que NOVA puisse les proposer à vos clients.
        </p>
        <div className="flex flex-wrap justify-center gap-3">
          <Button onClick={() => setCurrentView("products")}>
            <Package className="size-4" /> Ajouter des produits
          </Button>
          <Button variant="outline" onClick={() => setCurrentView("stock")}>Gérer le stock</Button>
          <Button variant="outline" onClick={() => setCurrentView("payment")}>Configurer le paiement</Button>
        </div>
      </Card>

      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        {[
          { icon: MessageCircle, label: "Conversations", value: "0", color: "text-emerald-600" },
          { icon: ShoppingCart, label: "Commandes", value: "0", color: "text-amber-600" },
          { icon: Banknote, label: "Ventes", value: "0 FCFA", color: "text-emerald-600" },
          { icon: Truck, label: "Livraisons", value: "0", color: "text-blue-600" },
        ].map((kpi) => (
          <Card key={kpi.label} className="p-4">
            <div className="flex items-center gap-3">
              <div className="flex size-10 items-center justify-center rounded-lg bg-muted">
                <kpi.icon className={`size-5 ${kpi.color}`} />
              </div>
              <div>
                <div className="text-2xl font-bold">{kpi.value}</div>
                <div className="text-xs text-muted-foreground">{kpi.label}</div>
              </div>
            </div>
          </Card>
        ))}
      </div>
    </div>
  );
}
