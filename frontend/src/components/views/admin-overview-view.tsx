"use client";

import * as React from "react";
import {
  LayoutDashboard,
  Store,
  Banknote,
  AlertTriangle,
  Zap,
  Loader2,
  CheckCircle2,
  PauseCircle,
  CreditCard,
  ArrowRight,
  Activity,
  MessageSquare,
  ShieldCheck,
  LifeBuoy,
  Receipt,
  RefreshCw,
} from "lucide-react";
import { toast } from "sonner";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { cn } from "@/lib/utils";
import {
  adminApi,
  ApiError,
  type AdminSubscriptionItem,
  type RevenueStats,
  type Shop,
} from "@/lib/api";
import { formatFCFA, formatDate } from "@/lib/format";
import { useAuthStore } from "@/stores/auth-store";
import { useUIStore } from "@/stores/ui-store";
import { ViewHeader } from "./view-header";

/** Badge coloré selon le statut d'abonnement (aligné sur le lifecycle Go). */
function statusBadge(status: string) {
  switch (status) {
    case "active":
      return (
        <Badge className="bg-brand/10 text-brand hover:bg-brand/20">
          <CheckCircle2 className="size-3" /> Actif
        </Badge>
      );
    case "trial":
      return (
        <Badge className="bg-blue-500/10 text-blue-600 hover:bg-blue-500/20 dark:text-blue-400">
          Essai
        </Badge>
      );
    case "late":
      return (
        <Badge className="bg-amber-accent/15 text-amber-accent hover:bg-amber-accent/25">
          <AlertTriangle className="size-3" /> En retard
        </Badge>
      );
    case "grace_period":
      return (
        <Badge className="bg-amber-accent/15 text-amber-accent hover:bg-amber-accent/25">
          Période de grâce
        </Badge>
      );
    case "suspended":
      return (
        <Badge variant="destructive">
          <PauseCircle className="size-3" /> Suspendu
        </Badge>
      );
    case "terminated":
      return <Badge variant="outline">Résilié</Badge>;
    default:
      return <Badge variant="outline">{status}</Badge>;
  }
}

type MetricTone = "brand" | "amber" | "blue" | "rose" | "muted";

interface MiniMetric {
  label: string;
  value: string;
  hint?: string;
  tone?: MetricTone;
}

interface KpiSectionProps {
  icon: React.ComponentType<{ className?: string }>;
  title: string;
  metrics: MiniMetric[];
  loading?: boolean;
}

const metricTones: Record<MetricTone, string> = {
  brand: "text-brand",
  amber: "text-amber-accent",
  blue: "text-blue-600 dark:text-blue-400",
  rose: "text-rose-600 dark:text-rose-400",
  muted: "text-muted-foreground",
};

function KpiSection({ icon: Icon, title, metrics, loading }: KpiSectionProps) {
  return (
    <Card>
      <CardHeader className="border-b pb-3">
        <CardTitle className="flex items-center gap-2 text-sm font-semibold">
          <Icon className="size-4 text-brand" /> {title}
        </CardTitle>
      </CardHeader>
      <CardContent className="py-4">
        {loading ? (
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
            {Array.from({ length: Math.max(metrics.length, 3) }).map((_, i) => (
              <div key={i} className="space-y-1.5">
                <Skeleton className="h-3 w-16" />
                <Skeleton className="h-6 w-12" />
              </div>
            ))}
          </div>
        ) : (
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
            {metrics.map((m) => (
              <div key={m.label} className="min-w-0">
                <div className="text-[11px] text-muted-foreground">{m.label}</div>
                <div
                  className={cn(
                    "mt-0.5 text-lg font-bold tracking-tight tabular-nums",
                    metricTones[m.tone ?? "muted"],
                  )}
                  title={m.value}
                >
                  {m.value}
                </div>
                {m.hint && (
                  <div className="text-[10px] text-muted-foreground">{m.hint}</div>
                )}
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

const NA = "non disponible";

export function AdminOverviewView() {
  const user = useAuthStore((s) => s.user);
  const setCurrentView = useUIStore((s) => s.setCurrentView);

  const [loading, setLoading] = React.useState(true);
  const [revenue, setRevenue] = React.useState<RevenueStats | null>(null);
  const [recent, setRecent] = React.useState<AdminSubscriptionItem[]>([]);
  const [late, setLate] = React.useState<AdminSubscriptionItem[]>([]);
  const [shops, setShops] = React.useState<Shop[]>([]);
  const [runningCron, setRunningCron] = React.useState(false);

  const load = React.useCallback(async () => {
    setLoading(true);
    try {
      const [rev, subs, lateSubs, shopsResp] = await Promise.all([
        adminApi.getRevenue(),
        adminApi.listSubscriptions({ limit: 100 }),
        adminApi.listLateSubscriptions(),
        adminApi.listShops({ limit: 500 }),
      ]);
      setRevenue(rev);
      setRecent(subs?.items ?? []);
      setLate(lateSubs?.items ?? []);
      setShops(shopsResp?.shops ?? []);
    } catch (err) {
      const msg =
        err instanceof ApiError
          ? err.message
          : "Impossible de charger les données d'administration.";
      toast.error("Erreur de chargement", { description: msg });
    } finally {
      setLoading(false);
    }
  }, []);

  React.useEffect(() => {
    load();
  }, [load]);

  async function handleRunCron() {
    setRunningCron(true);
    try {
      await adminApi.runCron();
      toast.success("Cron déclenché", {
        description: "Les traitements d'abonnement ont été exécutés.",
      });
      await load();
    } catch (err) {
      const msg =
        err instanceof ApiError ? err.message : "Échec du déclenchement cron.";
      toast.error("Échec cron", { description: msg });
    } finally {
      setRunningCron(false);
    }
  }

  // ====== Métriques calculées ======

  const trialCount = React.useMemo(
    () => recent.filter((s) => s.status === "trial").length,
    [recent],
  );

  const expiringSoonCount = React.useMemo(() => {
    const now = Date.now();
    const sevenDays = 7 * 24 * 60 * 60 * 1000;
    return recent.filter((s) => {
      if (!s.next_billing_at) return false;
      const ts = new Date(s.next_billing_at).getTime();
      return ts > now && ts - now <= sevenDays;
    }).length;
  }, [recent]);

  const newSignupsCount = React.useMemo(() => {
    const thirtyDays = 30 * 24 * 60 * 60 * 1000;
    const cutoff = Date.now() - thirtyDays;
    return shops.filter((s) => {
      if (!s.createdAt) return false;
      return new Date(s.createdAt).getTime() >= cutoff;
    }).length;
  }, [shops]);

  const recentSliced = recent.slice(0, 6);

  // Construction des 6 sections KPI
  const boutiqueMetrics: MiniMetric[] = [
    { label: "Total", value: String(shops.length), tone: "brand" },
    { label: "Actives", value: String(revenue?.active_count ?? 0), tone: "brand" },
    { label: "En essai", value: String(trialCount), tone: "blue" },
    {
      label: "Suspendues",
      value: String(revenue?.suspended_count ?? 0),
      tone: "rose",
    },
    {
      label: "Résiliées",
      value: String(revenue?.terminated_count ?? 0),
      tone: "muted",
    },
    {
      label: "Impayées",
      value: String(late.length),
      tone: "amber",
      hint: "Retard + grâce",
    },
    {
      label: "Nouvelles (30j)",
      value: String(newSignupsCount),
      tone: "blue",
    },
  ];

  const subscriptionMetrics: MiniMetric[] = [
    { label: "Actifs", value: String(revenue?.active_count ?? 0), tone: "brand" },
    {
      label: "Expirant bientôt",
      value: String(expiringSoonCount),
      tone: "amber",
      hint: "≤ 7 jours",
    },
    { label: "En retard", value: String(late.length), tone: "amber" },
    {
      label: "Résiliés",
      value: String(revenue?.terminated_count ?? 0),
      tone: "muted",
    },
    {
      label: "MRR",
      value: formatFCFA(revenue?.mrr ?? 0),
      tone: "brand",
      hint: "Récurrent",
    },
  ];

  const usageMetrics: MiniMetric[] = [
    { label: "Messages reçus", value: NA, tone: "muted" },
    { label: "Messages envoyés", value: NA, tone: "muted" },
    { label: "Conversations actives", value: NA, tone: "muted" },
    { label: "Volume IA", value: NA, tone: "muted" },
    { label: "Consommation / boutique", value: NA, tone: "muted" },
  ];

  const integrationMetrics: MiniMetric[] = [
    { label: "Connexions WhatsApp", value: NA, tone: "muted" },
    { label: "Erreurs API (24h)", value: NA, tone: "muted" },
    { label: "Webhooks en échec", value: NA, tone: "muted" },
    { label: "Incidents ouverts", value: NA, tone: "muted" },
  ];

  const financeMetrics: MiniMetric[] = [
    { label: "Frais d'installation", value: NA, tone: "muted" },
    {
      label: "Abo. encaissés",
      value: formatFCFA(revenue?.total_revenue ?? 0),
      tone: "brand",
    },
    { label: "Remboursements", value: NA, tone: "muted" },
    { label: "Coûts techniques", value: NA, tone: "muted" },
    { label: "Marge estimée", value: NA, tone: "muted" },
  ];

  const supportMetrics: MiniMetric[] = [
    { label: "Tickets ouverts", value: "0", tone: "muted" },
    { label: "Urgents (P1)", value: "0", tone: "muted" },
    { label: "En retard", value: "0", tone: "muted" },
    { label: "1ère réponse", value: NA, tone: "muted" },
  ];

  return (
    <div className="space-y-5">
      <ViewHeader
        title="Administration — Vue d'ensemble"
        description={`Pilotage de la plateforme NOVA${
          user?.fullName ? ` · connecté en tant que ${user.fullName}` : ""
        }.`}
        actions={
          <div className="flex items-center gap-2">
            <Button variant="outline" size="sm" onClick={load} disabled={loading}>
              {loading ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <RefreshCw className="size-4" />
              )}
              Actualiser
            </Button>
            <Button onClick={handleRunCron} disabled={runningCron || loading}>
              {runningCron ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <Zap className="size-4" />
              )}
              Déclencher cron
            </Button>
          </div>
        }
      />

      {/* 6 sections KPI thématiques */}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2 xl:grid-cols-3">
        <KpiSection
          icon={Store}
          title="Boutiques"
          metrics={boutiqueMetrics}
          loading={loading}
        />
        <KpiSection
          icon={CreditCard}
          title="Abonnements"
          metrics={subscriptionMetrics}
          loading={loading}
        />
        <KpiSection
          icon={Activity}
          title="Utilisation"
          metrics={usageMetrics}
          loading={loading}
        />
        <KpiSection
          icon={MessageSquare}
          title="Intégrations"
          metrics={integrationMetrics}
          loading={loading}
        />
        <KpiSection
          icon={Banknote}
          title="Finances"
          metrics={financeMetrics}
          loading={loading}
        />
        <KpiSection
          icon={LifeBuoy}
          title="Support"
          metrics={supportMetrics}
          loading={loading}
        />
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        {/* Abonnements récents */}
        <Card className="lg:col-span-2">
          <CardHeader className="border-b pb-3">
            <div className="flex items-center justify-between gap-2">
              <CardTitle className="flex items-center gap-2 text-sm font-semibold">
                <CreditCard className="size-4 text-brand" /> Abonnements récents
              </CardTitle>
              <Button
                variant="ghost"
                size="sm"
                className="h-8 gap-1 text-xs"
                onClick={() => setCurrentView("admin-shops")}
              >
                Voir tout <ArrowRight className="size-3.5" />
              </Button>
            </div>
          </CardHeader>
          <CardContent className="py-2">
            {loading ? (
              <div className="space-y-2 py-3">
                {Array.from({ length: 4 }).map((_, i) => (
                  <Skeleton key={i} className="h-9 w-full" />
                ))}
              </div>
            ) : recentSliced.length === 0 ? (
              <div className="py-10 text-center text-sm text-muted-foreground">
                Aucun abonnement enregistré pour le moment.
              </div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Boutique</TableHead>
                    <TableHead>Plan</TableHead>
                    <TableHead>Statut</TableHead>
                    <TableHead className="text-right">
                      Prochaine facturation
                    </TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {recentSliced.map((s) => (
                    <TableRow key={s.id}>
                      <TableCell className="font-medium">
                        {s.shop_name || "—"}
                        {s.shop_slug && (
                          <div className="text-[11px] text-muted-foreground">
                            {s.shop_slug}.nova.ci
                          </div>
                        )}
                      </TableCell>
                      <TableCell>{s.plan_name || "—"}</TableCell>
                      <TableCell>{statusBadge(s.status)}</TableCell>
                      <TableCell className="text-right tabular-nums text-muted-foreground">
                        {s.next_billing_at ? formatDate(s.next_billing_at) : "—"}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>

        {/* Colonne droite : alertes + répartition plan + actions */}
        <div className="space-y-4">
          {/* Abonnements en retard */}
          <Card>
            <CardHeader className="border-b pb-3">
              <CardTitle className="flex items-center gap-2 text-sm font-semibold">
                <AlertTriangle className="size-4 text-amber-accent" /> En retard
                / grâce
              </CardTitle>
            </CardHeader>
            <CardContent className="py-4">
              {loading ? (
                <Skeleton className="h-16 w-full" />
              ) : late.length === 0 ? (
                <div className="flex items-center gap-2 rounded-lg border border-brand/20 bg-brand/5 p-3 text-sm text-brand">
                  <CheckCircle2 className="size-4 shrink-0" />
                  Aucun abonnement en retard. Tout est à jour.
                </div>
              ) : (
                <Alert variant="default" className="border-amber-accent/40 bg-amber-accent/5">
                  <AlertTriangle className="text-amber-accent" />
                  <AlertTitle className="text-amber-accent">
                    {late.length} abonnement{late.length > 1 ? "s" : ""} à risque
                  </AlertTitle>
                  <AlertDescription>
                    <ul className="mt-1 max-h-40 space-y-1 overflow-y-auto pr-1 scrollbar-thin">
                      {late.slice(0, 6).map((s) => (
                        <li key={s.id} className="flex items-center justify-between gap-2 text-xs">
                          <span className="truncate">
                            {s.shop_name || s.shop_slug || "Boutique"}
                          </span>
                          <span className="shrink-0 text-muted-foreground">
                            {s.next_billing_at ? formatDate(s.next_billing_at) : "—"}
                          </span>
                        </li>
                      ))}
                      {late.length > 6 && (
                        <li className="text-[11px] text-muted-foreground">
                          + {late.length - 6} autre(s)…
                        </li>
                      )}
                    </ul>
                  </AlertDescription>
                </Alert>
              )}
            </CardContent>
          </Card>

          {/* Répartition par plan */}
          <Card>
            <CardHeader className="border-b pb-3">
              <CardTitle className="flex items-center gap-2 text-sm font-semibold">
                <LayoutDashboard className="size-4 text-brand" /> Répartition par plan
              </CardTitle>
            </CardHeader>
            <CardContent className="py-4">
              {loading ? (
                <div className="space-y-2">
                  {Array.from({ length: 3 }).map((_, i) => (
                    <Skeleton key={i} className="h-8 w-full" />
                  ))}
                </div>
              ) : revenue?.by_plan && revenue.by_plan.length > 0 ? (
                <ul className="space-y-2">
                  {revenue.by_plan.map((p) => (
                    <li
                      key={p.plan_name}
                      className="flex items-center justify-between gap-2 rounded-lg border p-2.5"
                    >
                      <div className="min-w-0">
                        <div className="truncate text-sm font-medium">{p.plan_name}</div>
                        <div className="text-[11px] text-muted-foreground">
                          {formatFCFA(p.monthly_price)} / mois
                        </div>
                      </div>
                      <Badge className="bg-brand/10 text-brand hover:bg-brand/20">
                        {p.active_count} actif{p.active_count > 1 ? "s" : ""}
                      </Badge>
                    </li>
                  ))}
                </ul>
              ) : (
                <div className="py-6 text-center text-sm text-muted-foreground">
                  Aucune donnée par plan.
                </div>
              )}
            </CardContent>
          </Card>

          {/* Actions rapides */}
          <Card>
            <CardHeader className="border-b pb-3">
              <CardTitle className="flex items-center gap-2 text-sm font-semibold">
                <Zap className="size-4 text-amber-accent" /> Actions rapides
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-2 py-4">
              <Button
                variant="outline"
                size="sm"
                className="w-full justify-start gap-2"
                onClick={handleRunCron}
                disabled={runningCron}
              >
                {runningCron ? (
                  <Loader2 className="size-4 animate-spin" />
                ) : (
                  <Zap className="size-4 text-amber-accent" />
                )}
                Déclencher cron abonnements
              </Button>
              <Button
                variant="outline"
                size="sm"
                className="w-full justify-start gap-2"
                onClick={() => setCurrentView("admin-revenue")}
              >
                <Banknote className="size-4 text-brand" /> Voir les revenus
              </Button>
              <Button
                variant="outline"
                size="sm"
                className="w-full justify-start gap-2"
                onClick={() => setCurrentView("admin-shops")}
              >
                <Store className="size-4 text-brand" /> Gérer les boutiques
              </Button>
              <Button
                variant="outline"
                size="sm"
                className="w-full justify-start gap-2"
                onClick={() => setCurrentView("admin-monitoring")}
              >
                <Activity className="size-4 text-brand" /> Supervision IA & WhatsApp
              </Button>
              <Button
                variant="outline"
                size="sm"
                className="w-full justify-start gap-2"
                onClick={() => setCurrentView("admin-audit")}
              >
                <ShieldCheck className="size-4 text-brand" /> Journal d&apos;audit
              </Button>
            </CardContent>
          </Card>
        </div>
      </div>

      {/* Note : données manquantes */}
      <div className="flex items-start gap-2 rounded-lg border border-dashed bg-muted/30 p-3 text-xs text-muted-foreground">
        <Receipt className="size-4 shrink-0 text-muted-foreground" />
        <div>
          Les métriques marquées <strong className="text-foreground">« non disponible »</strong> ne
          sont pas encore remontées par le backend Go. Aucune donnée n&apos;est
          extrapolée — les champs seront renseignés au fur et à mesure du
          déploiement des endpoints d&apos;agrégation (utilisation, intégrations,
          finances, support).
        </div>
      </div>
    </div>
  );
}
