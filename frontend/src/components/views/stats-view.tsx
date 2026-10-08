"use client";

import * as React from "react";
import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  Cell,
} from "recharts";
import {
  Banknote,
  ShoppingCart,
  MessageCircle,
  TrendingUp,
  Wallet,
  Info,
} from "lucide-react";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import {
  dailySales,
  topProducts,
  cancellationReasons,
  homeKpis,
} from "@/lib/mock-data";
import { formatFCFA, formatNumber } from "@/lib/format";
import { ViewHeader } from "./view-header";

interface KpiProps {
  label: string;
  value: string;
  hint?: string;
  icon: React.ComponentType<{ className?: string }>;
  tone: string;
}

function KpiCard({ label, value, hint, icon: Icon, tone }: KpiProps) {
  return (
    <Card>
      <CardContent className="flex items-start justify-between gap-3 py-4">
        <div className="min-w-0">
          <div className="text-xs text-muted-foreground">{label}</div>
          <div className="mt-1 text-xl font-bold tabular-nums sm:text-2xl">
            {value}
          </div>
          {hint && (
            <div className="mt-0.5 text-xs text-muted-foreground">{hint}</div>
          )}
        </div>
        <span
          className={cn(
            "flex size-9 shrink-0 items-center justify-center rounded-lg",
            tone,
          )}
        >
          <Icon className="size-5" />
        </span>
      </CardContent>
    </Card>
  );
}

function ChartTooltip({ active, payload, label }: any) {
  if (!active || !payload?.length) return null;
  const item = payload[0].payload;
  return (
    <div className="rounded-lg border bg-popover p-2.5 text-xs shadow-md">
      <div className="font-semibold">{label}</div>
      <div className="mt-1 text-brand">
        CA : {formatFCFA(item.ca)}
      </div>
      <div className="text-muted-foreground">
        {item.orders} commande{item.orders > 1 ? "s" : ""}
      </div>
    </div>
  );
}

export function StatsView() {
  const avgBasket = Math.round(
    homeKpis.sales / Math.max(1, homeKpis.orders),
  );

  const kpis: KpiProps[] = [
    {
      label: "CA du mois",
      value: formatFCFA(homeKpis.sales),
      hint: "Octobre 2026",
      icon: Banknote,
      tone: "bg-brand/10 text-brand",
    },
    {
      label: "Commandes",
      value: formatNumber(homeKpis.orders),
      hint: "Ce mois",
      icon: ShoppingCart,
      tone: "bg-amber-accent/15 text-amber-accent",
    },
    {
      label: "Conversations",
      value: formatNumber(homeKpis.conversations.total),
      hint: `${homeKpis.conversations.inProgress} en cours`,
      icon: MessageCircle,
      tone: "bg-blue-500/10 text-blue-600 dark:text-blue-400",
    },
    {
      label: "Taux de conversion",
      value: `${homeKpis.conversionRate}%`,
      hint: "Conversations → commandes",
      icon: TrendingUp,
      tone: "bg-purple-500/10 text-purple-600 dark:text-purple-400",
    },
    {
      label: "Panier moyen",
      value: formatFCFA(avgBasket),
      hint: "CA / commande",
      icon: Wallet,
      tone: "bg-emerald-500/10 text-emerald-700 dark:text-emerald-400",
    },
  ];

  return (
    <div className="space-y-5">
      <ViewHeader
        title="Statistiques"
        description="Aperçu de l'activité de votre boutique — exports avancés à venir."
      />

      <div className="grid grid-cols-2 gap-3 sm:gap-4 lg:grid-cols-5">
        {kpis.map((k) => (
          <KpiCard key={k.label} {...k} />
        ))}
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        {/* Graphique CA */}
        <Card className="lg:col-span-2">
          <CardHeader className="border-b pb-3">
            <CardTitle className="text-sm font-semibold">
              Ventes des 7 derniers jours
            </CardTitle>
          </CardHeader>
          <CardContent className="py-4">
            <div className="h-64 w-full">
              <ResponsiveContainer width="100%" height="100%">
                <BarChart
                  data={dailySales}
                  margin={{ top: 8, right: 8, bottom: 0, left: -8 }}
                >
                  <CartesianGrid
                    strokeDasharray="3 3"
                    stroke="var(--border)"
                    vertical={false}
                  />
                  <XAxis
                    dataKey="label"
                    tick={{ fontSize: 12, fill: "var(--muted-foreground)" }}
                    axisLine={false}
                    tickLine={false}
                  />
                  <YAxis
                    tick={{ fontSize: 11, fill: "var(--muted-foreground)" }}
                    axisLine={false}
                    tickLine={false}
                    tickFormatter={(v) => `${Math.round(v / 1000)}k`}
                  />
                  <Tooltip
                    cursor={{ fill: "var(--muted)", opacity: 0.3 }}
                    content={<ChartTooltip />}
                  />
                  <Bar dataKey="ca" radius={[6, 6, 0, 0]}>
                    {dailySales.map((entry, i) => (
                      <Cell
                        key={i}
                        fill={i === dailySales.length - 1 ? "var(--brand)" : "var(--brand)"}
                        opacity={i === dailySales.length - 1 ? 1 : 0.5 + (i / dailySales.length) * 0.5}
                      />
                    ))}
                  </Bar>
                </BarChart>
              </ResponsiveContainer>
            </div>
          </CardContent>
        </Card>

        {/* Top produits */}
        <Card>
          <CardHeader className="border-b pb-3">
            <CardTitle className="text-sm font-semibold">
              Produits les plus demandés
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-3 py-4">
            {topProducts.map((p, i) => (
              <div key={p.name} className="flex items-center gap-3">
                <span className="flex size-6 shrink-0 items-center justify-center rounded-full bg-brand/10 text-xs font-bold text-brand">
                  {i + 1}
                </span>
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-medium">{p.name}</div>
                  <div className="text-xs text-muted-foreground">
                    {p.units} vendus · {formatFCFA(p.revenue)}
                  </div>
                </div>
              </div>
            ))}
          </CardContent>
        </Card>
      </div>

      {/* Raisons d'annulation */}
      <Card>
        <CardHeader className="border-b pb-3">
          <CardTitle className="text-sm font-semibold">
            Raisons d'annulation
          </CardTitle>
        </CardHeader>
        <CardContent className="grid gap-3 py-4 sm:grid-cols-3">
          {cancellationReasons.map((r) => (
            <div
              key={r.reason}
              className="flex items-center justify-between rounded-lg border p-3"
            >
              <span className="text-sm">{r.reason}</span>
              <Badge variant="secondary">{r.count}</Badge>
            </div>
          ))}
        </CardContent>
      </Card>

      <div className="flex items-start gap-2 rounded-lg border border-dashed bg-muted/30 p-3 text-xs text-muted-foreground">
        <Info className="size-4 shrink-0 text-muted-foreground" />
        <div>
          <strong className="text-foreground">Statistiques avancées :</strong>{" "}
          exports CSV/PDF, segmentation client, courbes de tendance et prévisions
          IA seront disponibles prochainement.
        </div>
      </div>
    </div>
  );
}
