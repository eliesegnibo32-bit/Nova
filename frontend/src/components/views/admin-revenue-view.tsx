"use client";

import * as React from "react";
import {
  Banknote,
  TrendingUp,
  PauseCircle,
  Store,
  AlertTriangle,
  Zap,
  Loader2,
  CheckCircle2,
  RefreshCw,
  Receipt,
  ArrowRight,
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
} from "@/lib/api";
import { formatFCFA, formatDate, formatDateTime } from "@/lib/format";
import { ViewHeader } from "./view-header";

interface KpiCardProps {
  icon: React.ComponentType<{ className?: string }>;
  label: string;
  value: string;
  hint?: string;
  tone: "brand" | "amber" | "blue" | "rose";
}

const toneClasses: Record<KpiCardProps["tone"], string> = {
  brand: "bg-brand/10 text-brand",
  amber: "bg-amber-accent/15 text-amber-accent",
  blue: "bg-blue-500/10 text-blue-600 dark:text-blue-400",
  rose: "bg-rose-500/10 text-rose-600 dark:text-rose-400",
};

function KpiCard({ icon: Icon, label, value, hint, tone }: KpiCardProps) {
  return (
    <Card>
      <CardContent className="flex items-start justify-between gap-3 py-5">
        <div className="min-w-0">
          <div className="text-xs font-medium text-muted-foreground">{label}</div>
          <div className="mt-1 text-2xl font-bold tracking-tight tabular-nums">
            {value}
          </div>
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
  );
}

/** Badge de statut de paiement (aligné sur le lifecycle Go). */
function paymentBadge(status: string) {
  switch (status) {
    case "active":
      return (
        <Badge className="bg-brand/10 text-brand hover:bg-brand/20">
          <CheckCircle2 className="size-3" /> À jour
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
          En grâce
        </Badge>
      );
    case "suspended":
      return <Badge variant="destructive">Suspendu</Badge>;
    case "terminated":
      return <Badge variant="outline">Résilié</Badge>;
    default:
      return <Badge variant="outline">{status}</Badge>;
  }
}

export function AdminRevenueView() {
  const [loading, setLoading] = React.useState(true);
  const [revenue, setRevenue] = React.useState<RevenueStats | null>(null);
  const [late, setLate] = React.useState<AdminSubscriptionItem[]>([]);
  const [recent, setRecent] = React.useState<AdminSubscriptionItem[]>([]);
  const [runningCron, setRunningCron] = React.useState(false);

  const load = React.useCallback(async () => {
    setLoading(true);
    try {
      const [rev, lateSubs, subs] = await Promise.all([
        adminApi.getRevenue(),
        adminApi.listLateSubscriptions(),
        adminApi.listSubscriptions({ limit: 200 }),
      ]);
      setRevenue(rev);
      setLate(lateSubs?.items ?? []);
      setRecent(subs?.items ?? []);
    } catch (err) {
      const msg =
        err instanceof ApiError
          ? err.message
          : "Impossible de charger les données de revenu.";
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
        description: "Rappels de paiement et cycle de vie exécutés.",
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

  // Total agrégé (pour info) — somme des revenus par plan.
  const totalByPlan = React.useMemo(() => {
    if (!revenue?.by_plan) return 0;
    return revenue.by_plan.reduce((sum, p) => sum + (p.total_revenue || 0), 0);
  }, [revenue]);

  // Map plan_name → monthly_price (pour estimer le montant des paiements récents).
  const priceByPlan = React.useMemo(() => {
    const m = new Map<string, number>();
    for (const p of revenue?.by_plan ?? []) {
      if (p.plan_name) m.set(p.plan_name, p.monthly_price ?? 0);
    }
    return m;
  }, [revenue]);

  // Paiements récents : abonnements triés par started_at / created_at desc.
  const recentPayments = React.useMemo(() => {
    const sorted = [...recent].sort((a, b) => {
      const ta = new Date(a.started_at || a.created_at || 0).getTime();
      const tb = new Date(b.started_at || b.created_at || 0).getTime();
      return tb - ta;
    });
    return sorted.slice(0, 10);
  }, [recent]);

  return (
    <div className="space-y-5">
      <ViewHeader
        title="Administration — Revenus"
        description="MRR, revenu total et répartition par plan d'abonnement sur la plateforme."
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
            <Button size="sm" onClick={handleRunCron} disabled={runningCron}>
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

      {/* KPI cards */}
      <div className="grid grid-cols-2 gap-3 sm:gap-4 lg:grid-cols-4">
        {loading ? (
          Array.from({ length: 4 }).map((_, i) => (
            <Card key={i}>
              <CardContent className="py-5">
                <Skeleton className="h-4 w-24" />
                <Skeleton className="mt-3 h-7 w-24" />
                <Skeleton className="mt-2 h-3 w-32" />
              </CardContent>
            </Card>
          ))
        ) : (
          <>
            <KpiCard
              icon={TrendingUp}
              label="MRR"
              value={formatFCFA(revenue?.mrr ?? 0)}
              hint="Revenu mensuel récurrent"
              tone="brand"
            />
            <KpiCard
              icon={Banknote}
              label="Revenu total"
              value={formatFCFA(revenue?.total_revenue ?? 0)}
              hint="Cumul des paiements"
              tone="amber"
            />
            <KpiCard
              icon={Store}
              label="Boutiques actives"
              value={String(revenue?.active_count ?? 0)}
              hint="Essai + actif + en retard"
              tone="blue"
            />
            <KpiCard
              icon={PauseCircle}
              label="Boutiques suspendues"
              value={String(revenue?.suspended_count ?? 0)}
              hint="Accès bloqué (paiement en attente)"
              tone="rose"
            />
          </>
        )}
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        {/* Répartition par plan */}
        <Card className="lg:col-span-2">
          <CardHeader className="border-b pb-3">
            <CardTitle className="flex items-center gap-2 text-sm font-semibold">
              <Banknote className="size-4 text-brand" /> Répartition par plan
            </CardTitle>
          </CardHeader>
          <CardContent className="py-2">
            {loading ? (
              <div className="space-y-2 py-3">
                {Array.from({ length: 3 }).map((_, i) => (
                  <Skeleton key={i} className="h-10 w-full" />
                ))}
              </div>
            ) : !revenue?.by_plan || revenue.by_plan.length === 0 ? (
              <div className="py-10 text-center text-sm text-muted-foreground">
                Aucune donnée de revenu par plan disponible.
              </div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Plan</TableHead>
                    <TableHead className="text-right">Boutiques actives</TableHead>
                    <TableHead className="text-right">Prix mensuel</TableHead>
                    <TableHead className="text-right">Revenu total</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {revenue.by_plan.map((p) => (
                    <TableRow key={p.plan_name}>
                      <TableCell className="font-medium">{p.plan_name}</TableCell>
                      <TableCell className="text-right tabular-nums">
                        {p.active_count}
                      </TableCell>
                      <TableCell className="text-right tabular-nums text-muted-foreground">
                        {formatFCFA(p.monthly_price)}
                      </TableCell>
                      <TableCell className="text-right tabular-nums font-semibold">
                        {formatFCFA(p.total_revenue)}
                      </TableCell>
                    </TableRow>
                  ))}
                  {revenue.by_plan.length > 1 && (
                    <TableRow className="bg-muted/40">
                      <TableCell className="font-semibold">Total</TableCell>
                      <TableCell className="text-right tabular-nums font-semibold">
                        {revenue.by_plan.reduce(
                          (s, p) => s + (p.active_count || 0),
                          0,
                        )}
                      </TableCell>
                      <TableCell className="text-right">—</TableCell>
                      <TableCell className="text-right tabular-nums font-bold">
                        {formatFCFA(totalByPlan)}
                      </TableCell>
                    </TableRow>
                  )}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>

        {/* Abonnements en retard */}
        <Card>
          <CardHeader className="border-b pb-3">
            <CardTitle className="flex items-center gap-2 text-sm font-semibold">
              <AlertTriangle className="size-4 text-amber-accent" /> Abonnements en retard
            </CardTitle>
          </CardHeader>
          <CardContent className="py-4">
            {loading ? (
              <div className="space-y-2">
                {Array.from({ length: 3 }).map((_, i) => (
                  <Skeleton key={i} className="h-10 w-full" />
                ))}
              </div>
            ) : late.length === 0 ? (
              <div className="flex items-center gap-2 rounded-lg border border-brand/20 bg-brand/5 p-3 text-sm text-brand">
                <CheckCircle2 className="size-4 shrink-0" />
                Aucun retard. Tous les paiements sont à jour.
              </div>
            ) : (
              <>
                <Alert variant="default" className="mb-3 border-amber-accent/40 bg-amber-accent/5">
                  <AlertTriangle className="text-amber-accent" />
                  <AlertTitle className="text-amber-accent">
                    {late.length} abonnement{late.length > 1 ? "s" : ""} à risque
                  </AlertTitle>
                  <AlertDescription className="text-xs">
                    En retard ou en période de grâce. Le cron peut les faire
                    passer en suspension automatiquement.
                  </AlertDescription>
                </Alert>
                <ul className="max-h-72 space-y-1.5 overflow-y-auto pr-1 scrollbar-thin">
                  {late.map((s) => (
                    <li
                      key={s.id}
                      className="flex items-center justify-between gap-2 rounded-lg border p-2.5"
                    >
                      <div className="min-w-0">
                        <div className="truncate text-sm font-medium">
                          {s.shop_name || s.shop_slug || "Boutique"}
                        </div>
                        <div className="text-[11px] text-muted-foreground">
                          {s.plan_name || "—"} ·{" "}
                          {s.next_billing_at
                            ? `échéance ${formatDate(s.next_billing_at)}`
                            : "—"}
                        </div>
                      </div>
                      <Badge className="bg-amber-accent/15 text-amber-accent hover:bg-amber-accent/25">
                        {s.status === "grace_period" ? "Grâce" : "Retard"}
                      </Badge>
                    </li>
                  ))}
                </ul>
              </>
            )}
          </CardContent>
        </Card>
      </div>

      {/* Paiements récents */}
      <Card>
        <CardHeader className="border-b pb-3">
          <CardTitle className="flex items-center gap-2 text-sm font-semibold">
            <Receipt className="size-4 text-brand" /> Paiements récents
          </CardTitle>
        </CardHeader>
        <CardContent className="py-2">
          {loading ? (
            <div className="space-y-2 py-3">
              {Array.from({ length: 4 }).map((_, i) => (
                <Skeleton key={i} className="h-9 w-full" />
              ))}
            </div>
          ) : recentPayments.length === 0 ? (
            <div className="py-10 text-center text-sm text-muted-foreground">
              Aucun paiement enregistré pour le moment.
            </div>
          ) : (
            <>
              <div className="overflow-x-auto">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Boutique</TableHead>
                      <TableHead>Formule</TableHead>
                      <TableHead className="text-right">Montant</TableHead>
                      <TableHead className="text-right">Date</TableHead>
                      <TableHead>Statut</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {recentPayments.map((s) => {
                      const amount =
                        priceByPlan.get(s.plan_name || "") ?? 0;
                      return (
                        <TableRow key={s.id}>
                          <TableCell className="font-medium">
                            {s.shop_name || s.shop_slug || "—"}
                          </TableCell>
                          <TableCell>{s.plan_name || "—"}</TableCell>
                          <TableCell className="text-right tabular-nums">
                            {amount > 0 ? (
                              formatFCFA(amount)
                            ) : (
                              <span className="text-muted-foreground">—</span>
                            )}
                          </TableCell>
                          <TableCell className="text-right tabular-nums text-muted-foreground">
                            {s.started_at
                              ? formatDateTime(s.started_at)
                              : s.created_at
                                ? formatDateTime(s.created_at)
                                : "—"}
                          </TableCell>
                          <TableCell>{paymentBadge(s.status)}</TableCell>
                        </TableRow>
                      );
                    })}
                  </TableBody>
                </Table>
              </div>
              <div className="flex items-start gap-2 px-2 py-2 text-[11px] text-muted-foreground">
                <ArrowRight className="size-3.5 shrink-0" />
                <span>
                  Estimation issue des abonnements actifs (montant = prix mensuel
                  du plan). Le détail des paiements réels (Wave/Moov/Orange/MTN)
                  sera disponible dès l’exposition de l’endpoint{" "}
                  <code className="rounded bg-muted px-1 py-0.5">
                    /api/admin/subscriptions/payments
                  </code>
                  .
                </span>
              </div>
            </>
          )}
        </CardContent>
      </Card>

      {/* Note explicative */}
      <div className="flex items-start gap-2 rounded-lg border border-dashed bg-muted/30 p-3 text-xs text-muted-foreground">
        <Banknote className="size-4 shrink-0 text-muted-foreground" />
        <div>
          <strong className="text-foreground">MRR :</strong> somme des prix de
          plan des abonnements actifs (essai + actif + retard + grâce).{" "}
          <strong className="text-foreground">Revenu total :</strong> cumul des
          paiements enregistrés via{" "}
          <code className="rounded bg-muted px-1 py-0.5 text-[10px]">subscription_payments.amount</code>.
          Les valeurs proviennent de{" "}
          <code className="rounded bg-muted px-1 py-0.5 text-[10px]">
            GET /api/admin/subscriptions/revenue
          </code>{" "}
          (backend Go).
        </div>
      </div>
    </div>
  );
}
