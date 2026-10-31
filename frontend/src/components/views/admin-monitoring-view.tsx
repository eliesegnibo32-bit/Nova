"use client";

import * as React from "react";
import {
  Activity,
  MessageSquare,
  Cpu,
  Gauge,
  ShieldCheck,
  AlertTriangle,
  Loader2,
  RefreshCw,
  ArrowRightLeft,
  FileQuestion,
  TrendingUp,
} from "lucide-react";
import { toast } from "sonner";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Progress } from "@/components/ui/progress";
import { cn } from "@/lib/utils";
import { adminApi, ApiError, type RevenueStats } from "@/lib/api";
import { formatFCFA } from "@/lib/format";
import { ViewHeader } from "./view-header";

/**
 * Vue Admin — Supervision IA & WhatsApp.
 *
 * Le backend Go n'expose pas encore d'endpoint d'agrégation dédié à la
 * supervision (quotas IA, erreurs WhatsApp, incidents…). On présente donc
 * les cartes avec une structure complète, mais on affiche « non disponible »
 * pour les métriques qui ne sont pas encore remontées.
 *
 * Seul `adminApi.getRevenue()` est utilisé pour afficher le MRR et le nombre
 * d'abonnements actifs (contexte activité plateforme).
 */
export function AdminMonitoringView() {
  const [loading, setLoading] = React.useState(true);
  const [revenue, setRevenue] = React.useState<RevenueStats | null>(null);

  const load = React.useCallback(async () => {
    setLoading(true);
    try {
      const rev = await adminApi.getRevenue();
      setRevenue(rev);
    } catch (err) {
      const msg =
        err instanceof ApiError
          ? err.message
          : "Impossible de charger les données de supervision.";
      toast.error("Erreur de chargement", { description: msg });
    } finally {
      setLoading(false);
    }
  }, []);

  React.useEffect(() => {
    load();
  }, [load]);

  return (
    <div className="space-y-5">
      <ViewHeader
        title="Supervision IA & WhatsApp"
        description="État des intégrations WhatsApp, consommation IA, quotas et qualité de service."
        actions={
          <Button variant="outline" size="sm" onClick={load} disabled={loading}>
            {loading ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <RefreshCw className="size-4" />
            )}
            Actualiser
          </Button>
        }
      />

      {/* KPI globaux */}
      <div className="grid grid-cols-2 gap-3 sm:gap-4 lg:grid-cols-4">
        <MetricCard
          icon={MessageSquare}
          label="Connexions WhatsApp actives"
          value={loading ? null : "non disponible"}
          hint="Comptes Meta Business connectés"
          tone="brand"
        />
        <MetricCard
          icon={AlertTriangle}
          label="Webhooks en échec (24h)"
          value={loading ? null : "non disponible"}
          hint="Livraisons WhatsApp non acquittées"
          tone="amber"
        />
        <MetricCard
          icon={Cpu}
          label="Requêtes IA (30j)"
          value={loading ? null : "non disponible"}
          hint="Appels au moteur de conversation"
          tone="blue"
        />
        <MetricCard
          icon={TrendingUp}
          label="MRR plateforme"
          value={loading ? null : formatFCFA(revenue?.mrr ?? 0)}
          hint="Revenu mensuel récurrent"
          tone="brand"
        />
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        {/* WhatsApp */}
        <Card>
          <CardHeader className="border-b pb-3">
            <CardTitle className="flex items-center gap-2 text-sm font-semibold">
              <MessageSquare className="size-4 text-brand" /> WhatsApp
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-3 py-4">
            {loading ? (
              <Skeleton className="h-24 w-full" />
            ) : (
              <>
                <div className="grid grid-cols-2 gap-3">
                  <InfoRow label="Connexions actives" value="non disponible" />
                  <InfoRow label="Erreurs d'envoi (24h)" value="non disponible" tone="amber" />
                  <InfoRow label="Erreurs de réception (24h)" value="non disponible" tone="amber" />
                  <InfoRow label="Webhooks en échec" value="non disponible" tone="rose" />
                </div>
                <div className="rounded-lg border border-dashed bg-muted/30 p-3 text-xs text-muted-foreground">
                  <AlertTriangle className="mb-1 size-3.5 text-amber-accent" />
                  Les endpoints <code className="rounded bg-muted px-1 py-0.5">/api/admin/whatsapp/status</code> et{" "}
                  <code className="rounded bg-muted px-1 py-0.5">/api/admin/incidents</code> ne sont pas encore
                  exposés par le backend Go. Les valeurs seront remontées dès leur déploiement.
                </div>
                <div>
                  <div className="mb-2 text-xs font-medium text-muted-foreground">
                    Derniers incidents
                  </div>
                  <div className="rounded-lg border p-3 text-center text-xs text-muted-foreground">
                    Aucun incident enregistré — flux non connecté.
                  </div>
                </div>
              </>
            )}
          </CardContent>
        </Card>

        {/* IA */}
        <Card>
          <CardHeader className="border-b pb-3">
            <CardTitle className="flex items-center gap-2 text-sm font-semibold">
              <Cpu className="size-4 text-brand" /> IA & consommation
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-3 py-4">
            {loading ? (
              <Skeleton className="h-24 w-full" />
            ) : (
              <>
                <div className="grid grid-cols-2 gap-3">
                  <InfoRow label="Requêtes totales (30j)" value="non disponible" />
                  <InfoRow label="Tokens estimés" value="non disponible" />
                  <InfoRow label="Coût estimé (30j)" value="non disponible" tone="amber" />
                  <InfoRow label="Erreurs IA (24h)" value="non disponible" tone="rose" />
                </div>
                <div>
                  <div className="mb-2 text-xs font-medium text-muted-foreground">
                    Consommation par boutique (top 5)
                  </div>
                  <div className="rounded-lg border p-3 text-center text-xs text-muted-foreground">
                    Aucune donnée agrégée — endpoint{" "}
                    <code className="rounded bg-muted px-1 py-0.5">/api/admin/ai/usage</code> à venir.
                  </div>
                </div>
              </>
            )}
          </CardContent>
        </Card>

        {/* Quotas */}
        <Card>
          <CardHeader className="border-b pb-3">
            <CardTitle className="flex items-center gap-2 text-sm font-semibold">
              <Gauge className="size-4 text-brand" /> Quotas IA
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-3 py-4">
            {loading ? (
              <Skeleton className="h-24 w-full" />
            ) : (
              <>
                <div className="grid grid-cols-3 gap-3">
                  <InfoRow label="Proches du quota" value="non disponible" tone="amber" compact />
                  <InfoRow label="Au quota" value="non disponible" tone="rose" compact />
                  <InfoRow label="Plafond global" value="non disponible" compact />
                </div>
                <div>
                  <div className="mb-2 text-xs font-medium text-muted-foreground">
                    Occupation du plafond global
                  </div>
                  <Progress value={0} className="h-2" />
                  <div className="mt-1 text-[11px] text-muted-foreground">
                    Données non disponibles — le service de quotas n'est pas encore agrégé côté plateforme.
                  </div>
                </div>
              </>
            )}
          </CardContent>
        </Card>

        {/* Qualité */}
        <Card>
          <CardHeader className="border-b pb-3">
            <CardTitle className="flex items-center gap-2 text-sm font-semibold">
              <ShieldCheck className="size-4 text-brand" /> Qualité du service
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-3 py-4">
            {loading ? (
              <Skeleton className="h-24 w-full" />
            ) : (
              <>
                <div className="grid grid-cols-2 gap-3">
                  <InfoRow
                    icon={ArrowRightLeft}
                    label="Transferts vers humain (30j)"
                    value="non disponible"
                  />
                  <InfoRow
                    icon={FileQuestion}
                    label="Réponses sans source (30j)"
                    value="non disponible"
                    tone="amber"
                  />
                  <InfoRow label="Taux de réponse IA" value="non disponible" />
                  <InfoRow label="Satisfaction client" value="non disponible" tone="brand" />
                </div>
                <div className="rounded-lg border border-dashed bg-muted/30 p-3 text-xs text-muted-foreground">
                  Les métriques qualité nécessitent un pipeline d'évaluation des conversations.
                  Elles seront calculées à partir des évaluations client et des transferts humains.
                </div>
              </>
            )}
          </CardContent>
        </Card>
      </div>

      {/* Note */}
      <div className="flex items-start gap-2 rounded-lg border border-dashed bg-muted/30 p-3 text-xs text-muted-foreground">
        <Activity className="size-4 shrink-0 text-muted-foreground" />
        <div>
          Cette vue consolidera à terme l'ensemble des indicateurs d'intégration
          (WhatsApp Cloud API, moteur IA, quotas, incidents). Les branches
          manquantes sont identifiées par <strong className="text-foreground">« non disponible »</strong> —
          aucune donnée n'est extrapolée.
        </div>
      </div>
    </div>
  );
}

// ====== Sous-composants ======

interface MetricCardProps {
  icon: React.ComponentType<{ className?: string }>;
  label: string;
  value: string | null;
  hint?: string;
  tone: "brand" | "amber" | "blue" | "rose";
}

const toneClasses: Record<MetricCardProps["tone"], string> = {
  brand: "bg-brand/10 text-brand",
  amber: "bg-amber-accent/15 text-amber-accent",
  blue: "bg-blue-500/10 text-blue-600 dark:text-blue-400",
  rose: "bg-rose-500/10 text-rose-600 dark:text-rose-400",
};

function MetricCard({ icon: Icon, label, value, hint, tone }: MetricCardProps) {
  return (
    <Card>
      <CardContent className="flex items-start justify-between gap-3 py-5">
        <div className="min-w-0">
          <div className="text-xs font-medium text-muted-foreground">{label}</div>
          {value === null ? (
            <Skeleton className="mt-2 h-7 w-24" />
          ) : (
            <div className="mt-1 text-xl font-bold tracking-tight tabular-nums sm:text-2xl">
              {value}
            </div>
          )}
          {hint && <div className="mt-1 text-xs text-muted-foreground">{hint}</div>}
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
  );
}

interface InfoRowProps {
  label: string;
  value: string;
  hint?: string;
  tone?: "default" | "brand" | "amber" | "rose";
  icon?: React.ComponentType<{ className?: string }>;
  compact?: boolean;
}

function InfoRow({ label, value, hint, tone = "default", icon: Icon, compact }: InfoRowProps) {
  const valueTone =
    tone === "brand"
      ? "text-brand"
      : tone === "amber"
        ? "text-amber-accent"
        : tone === "rose"
          ? "text-rose-600 dark:text-rose-400"
          : "text-foreground";
  return (
    <div
      className={cn(
        "rounded-lg border bg-card p-2.5",
        compact ? "text-center" : "",
      )}
    >
      <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
        {Icon && <Icon className="size-3" />}
        <span className="truncate">{label}</span>
      </div>
      <div className={cn("mt-0.5 text-sm font-semibold tabular-nums", valueTone, compact && "text-base")}>
        {value}
      </div>
      {hint && <div className="text-[10px] text-muted-foreground">{hint}</div>}
    </div>
  );
}
