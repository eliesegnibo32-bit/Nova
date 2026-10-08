"use client";

import * as React from "react";
import {
  MessageCircle,
  ShoppingCart,
  Banknote,
  Truck,
  ArrowUpRight,
  ArrowRight,
  Sparkles,
  AlertTriangle,
  Clock,
  CheckCircle2,
  XCircle,
  Bell,
} from "lucide-react";
import { motion } from "framer-motion";
import { toast } from "sonner";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Progress } from "@/components/ui/progress";
import { cn } from "@/lib/utils";
import {
  homeKpis,
  conversationsToReprocess,
  ordersToConfirm,
  lowStockItems,
  demoShop,
} from "@/lib/mock-data";
import { formatFCFA, timeAgo } from "@/lib/format";
import { useUIStore } from "@/stores/ui-store";

interface KpiCardProps {
  icon: React.ComponentType<{ className?: string }>;
  label: string;
  value: string;
  hint?: string;
  tone: "brand" | "amber" | "blue" | "slate";
  index: number;
}

const toneClasses: Record<KpiCardProps["tone"], string> = {
  brand: "bg-brand/10 text-brand",
  amber: "bg-amber-accent/15 text-amber-accent",
  blue: "bg-blue-500/10 text-blue-600 dark:text-blue-400",
  slate: "bg-slate-500/10 text-slate-600 dark:text-slate-300",
};

function KpiCard({ icon: Icon, label, value, hint, tone, index }: KpiCardProps) {
  return (
    <motion.div
      initial={{ opacity: 0, y: 12 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.3, delay: index * 0.05 }}
    >
      <Card className="overflow-hidden">
        <CardContent className="flex items-start justify-between gap-3 py-5">
          <div className="min-w-0">
            <div className="text-xs font-medium text-muted-foreground">{label}</div>
            <div className="mt-1 text-2xl font-bold tracking-tight">{value}</div>
            {hint && (
              <div className="mt-1 text-xs text-muted-foreground">{hint}</div>
            )}
          </div>
          <span
            className={cn(
              "flex size-10 shrink-0 items-center justify-center rounded-lg",
              toneClasses[tone],
            )}
          >
            <Icon className="size-5" />
          </span>
        </CardContent>
      </Card>
    </motion.div>
  );
}

function SectionCard({
  title,
  count,
  icon: Icon,
  children,
  action,
}: {
  title: string;
  count?: number;
  icon: React.ComponentType<{ className?: string }>;
  children: React.ReactNode;
  action?: React.ReactNode;
}) {
  return (
    <Card className="flex h-full flex-col">
      <CardHeader className="flex-row items-center justify-between gap-2 border-b pb-3">
        <CardTitle className="flex items-center gap-2 text-sm font-semibold">
          <Icon className="size-4 text-muted-foreground" />
          {title}
          {count !== undefined && (
            <Badge variant="secondary" className="ml-1">
              {count}
            </Badge>
          )}
        </CardTitle>
        {action}
      </CardHeader>
      <CardContent className="flex flex-1 flex-col gap-2 py-3">
        {children}
      </CardContent>
    </Card>
  );
}

export function HomeView() {
  const setCurrentView = useUIStore((s) => s.setCurrentView);

  const kpis = homeKpis;

  return (
    <div className="space-y-6">
      {/* Header avec salutation + échéance */}
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-xl font-bold tracking-tight sm:text-2xl">
            Bonjour Awa 👋
          </h2>
          <p className="text-sm text-muted-foreground">
            Voici l'activité de votre boutique {demoShop.name} aujourd'hui.
          </p>
        </div>
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <Clock className="size-3.5" />
          {new Date().toLocaleDateString("fr-FR", {
            weekday: "long",
            day: "numeric",
            month: "long",
            year: "numeric",
          })}
        </div>
      </div>

      {/* Bannière abonnement */}
      <div className="flex flex-col gap-3 rounded-xl border border-amber-accent/30 bg-amber-accent/10 p-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-start gap-3">
          <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-amber-accent text-amber-accent-foreground">
            <Bell className="size-4" />
          </span>
          <div>
            <div className="text-sm font-semibold">
              Échéance abonnement — {demoShop.plan}
            </div>
            <div className="text-xs text-muted-foreground">
              Renouvellement le 15/11/2026 — {formatFCFA(demoShop.planPrice)} / mois
            </div>
          </div>
        </div>
        <Button
          size="sm"
          onClick={() => {
            toast.success("Redirection vers le paiement…", {
              description: "Wave / Orange Money / MTN MoMo.",
            });
          }}
        >
          Payer maintenant
        </Button>
      </div>

      {/* KPI grid */}
      <div className="grid grid-cols-2 gap-3 sm:gap-4 lg:grid-cols-4">
        <KpiCard
          index={0}
          icon={MessageCircle}
          label="Conversations"
          value={String(kpis.conversations.total)}
          hint={`${kpis.conversations.inProgress} en cours`}
          tone="brand"
        />
        <KpiCard
          index={1}
          icon={ShoppingCart}
          label="Commandes"
          value={String(kpis.orders)}
          hint="Aujourd'hui"
          tone="amber"
        />
        <KpiCard
          index={2}
          icon={Banknote}
          label="Ventes"
          value={formatFCFA(kpis.sales)}
          hint="Cumul ce mois"
          tone="brand"
        />
        <KpiCard
          index={3}
          icon={Truck}
          label="Livraisons"
          value={String(kpis.deliveries.inProgress)}
          hint="En cours"
          tone="blue"
        />
      </div>

      {/* Activité NOVA */}
      <Card className="overflow-hidden border-brand/30">
        <CardHeader className="border-b bg-brand/5 pb-3">
          <CardTitle className="flex items-center gap-2 text-sm font-semibold">
            <Sparkles className="size-4 text-brand" />
            Activité NOVA — votre IA cette semaine
          </CardTitle>
        </CardHeader>
        <CardContent className="grid gap-4 py-4 sm:grid-cols-3">
          <div className="space-y-1">
            <div className="text-xs text-muted-foreground">
              Conversations traitées par l'IA
            </div>
            <div className="text-xl font-bold">
              {kpis.aiHandled}{" "}
              <span className="text-sm font-medium text-brand">
                ({kpis.aiRate}%)
              </span>
            </div>
            <Progress value={kpis.aiRate} className="h-1.5" />
          </div>
          <div className="space-y-1">
            <div className="text-xs text-muted-foreground">
              Taux de conversion
            </div>
            <div className="text-xl font-bold">{kpis.conversionRate}%</div>
            <Progress value={kpis.conversionRate} className="h-1.5" />
          </div>
          <div className="space-y-1">
            <div className="text-xs text-muted-foreground">
              Coût IA estimé ce mois
            </div>
            <div className="text-xl font-bold">
              {formatFCFA(kpis.aiCostMonth)}
            </div>
            <div className="text-[11px] text-muted-foreground">
              ≈ {formatFCFA(Math.round(kpis.aiCostMonth / kpis.aiHandled))} /
              conversation
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Section "À traiter" */}
      <div>
        <div className="mb-3 flex items-center justify-between">
          <h3 className="text-base font-semibold">À traiter</h3>
          <span className="text-xs text-muted-foreground">
            Ces éléments requièrent votre attention immédiate.
          </span>
        </div>
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
          {/* Conversations à reprendre */}
          <SectionCard
            title="Conversations à reprendre"
            count={kpis.conversationsToReprocess}
            icon={MessageCircle}
            action={
              <Button
                variant="ghost"
                size="sm"
                className="h-7 gap-1 text-xs"
                onClick={() => setCurrentView("conversations")}
              >
                Voir tout <ArrowRight className="size-3" />
              </Button>
            }
          >
            {conversationsToReprocess.length === 0 ? (
              <EmptyHint text="Aucune conversation en attente." />
            ) : (
              conversationsToReprocess.slice(0, 5).map((c) => (
                <button
                  key={c.id}
                  onClick={() => setCurrentView("conversations")}
                  className="flex items-start gap-2.5 rounded-lg border p-2.5 text-left transition-colors hover:bg-accent"
                >
                  <span
                    className={cn(
                      "flex size-8 shrink-0 items-center justify-center rounded-full text-xs font-semibold text-white",
                      c.avatarColor,
                    )}
                  >
                    {c.customerName
                      .split(" ")
                      .map((s) => s[0])
                      .join("")
                      .slice(0, 2)}
                  </span>
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center justify-between gap-2">
                      <span className="truncate text-sm font-medium">
                        {c.customerName}
                      </span>
                      <span className="shrink-0 text-[10px] text-muted-foreground">
                        {timeAgo(c.lastAt)}
                      </span>
                    </div>
                    <div className="line-clamp-1 text-xs text-muted-foreground">
                      {c.lastMessage}
                    </div>
                    <div className="mt-1.5">
                      <Badge className="bg-amber-accent/15 text-amber-accent hover:bg-amber-accent/25">
                        <Sparkles className="size-3" /> Prendre la main
                      </Badge>
                    </div>
                  </div>
                </button>
              ))
            )}
          </SectionCard>

          {/* Commandes à confirmer */}
          <SectionCard
            title="Commandes à confirmer"
            count={kpis.ordersToConfirm}
            icon={ShoppingCart}
            action={
              <Button
                variant="ghost"
                size="sm"
                className="h-7 gap-1 text-xs"
                onClick={() => setCurrentView("orders")}
              >
                Voir tout <ArrowRight className="size-3" />
              </Button>
            }
          >
            {ordersToConfirm.length === 0 ? (
              <EmptyHint text="Aucune commande en attente." />
            ) : (
              ordersToConfirm.slice(0, 4).map((o) => (
                <div
                  key={o.id}
                  className="rounded-lg border p-2.5"
                >
                  <div className="flex items-center justify-between gap-2">
                    <span className="truncate text-sm font-medium">
                      {o.number}
                    </span>
                    <span className="shrink-0 text-xs font-semibold">
                      {formatFCFA(o.total)}
                    </span>
                  </div>
                  <div className="truncate text-xs text-muted-foreground">
                    {o.customerName} · {o.customerZone}
                  </div>
                  <div className="mt-2 flex items-center gap-1.5">
                    <Button
                      size="sm"
                      variant="default"
                      className="h-7 flex-1 gap-1"
                      onClick={() =>
                        toast.success(`Commande ${o.number} confirmée`)
                      }
                    >
                      <CheckCircle2 className="size-3" /> Confirmer
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      className="h-7 gap-1 text-destructive"
                      onClick={() =>
                        toast.error(`Commande ${o.number} refusée`)
                      }
                    >
                      <XCircle className="size-3" /> Refuser
                    </Button>
                  </div>
                </div>
              ))
            )}
          </SectionCard>

          {/* Stocks faibles */}
          <SectionCard
            title="Stocks faibles"
            count={kpis.lowStock}
            icon={AlertTriangle}
            action={
              <Button
                variant="ghost"
                size="sm"
                className="h-7 gap-1 text-xs"
                onClick={() => setCurrentView("stock")}
              >
                Voir tout <ArrowRight className="size-3" />
              </Button>
            }
          >
            {lowStockItems.length === 0 ? (
              <EmptyHint text="Stock sain partout. 🎉" />
            ) : (
              lowStockItems.slice(0, 6).map((s) => {
                const out = s.available <= 0;
                return (
                  <div
                    key={s.id}
                    className="flex items-center gap-2.5 rounded-lg border p-2.5"
                  >
                    <span
                      className={cn(
                        "flex size-8 shrink-0 items-center justify-center rounded-md text-white",
                        s.thumbnailColor,
                      )}
                    >
                      <ShoppingCart className="size-4" />
                    </span>
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-sm font-medium">
                        {s.productName}
                      </div>
                      <div className="truncate text-xs text-muted-foreground">
                        {s.variantLabel}
                      </div>
                    </div>
                    <div className="text-right">
                      <div
                        className={cn(
                          "text-sm font-bold",
                          out ? "text-destructive" : "text-amber-accent",
                        )}
                      >
                        {s.available}
                      </div>
                      <div className="text-[10px] text-muted-foreground">
                        / seuil {s.threshold}
                      </div>
                    </div>
                    <Button
                      size="sm"
                      variant="outline"
                      className="h-7 shrink-0"
                      onClick={() => setCurrentView("stock")}
                    >
                      Réappro
                    </Button>
                  </div>
                );
              })
            )}
          </SectionCard>
        </div>
      </div>

      {/* Accès rapide */}
      <Card>
        <CardContent className="grid gap-3 py-4 sm:grid-cols-2 lg:grid-cols-4">
          {[
            { label: "Ajouter un produit", icon: ShoppingCart, view: "products" as const },
            { label: "Voir les conversations", icon: MessageCircle, view: "conversations" as const },
            { label: "Configurer le paiement", icon: Banknote, view: "payment" as const },
            { label: "Statistiques du mois", icon: Banknote, view: "stats" as const },
          ].map((q) => (
            <button
              key={q.label}
              onClick={() => setCurrentView(q.view)}
              className="flex items-center justify-between gap-2 rounded-lg border bg-background p-3 text-left transition-colors hover:bg-accent"
            >
              <div className="flex items-center gap-2.5">
                <span className="flex size-8 items-center justify-center rounded-md bg-brand/10 text-brand">
                  <q.icon className="size-4" />
                </span>
                <span className="text-sm font-medium">{q.label}</span>
              </div>
              <ArrowUpRight className="size-4 text-muted-foreground" />
            </button>
          ))}
        </CardContent>
      </Card>
    </div>
  );
}

function EmptyHint({ text }: { text: string }) {
  return (
    <div className="flex flex-1 items-center justify-center rounded-lg border border-dashed p-4 text-center text-xs text-muted-foreground">
      {text}
    </div>
  );
}
