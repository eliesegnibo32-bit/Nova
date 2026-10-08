"use client";

import * as React from "react";
import {
  CreditCard,
  Sparkles,
  CheckCircle2,
  Lock,
  Receipt,
} from "lucide-react";
import { toast } from "sonner";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Progress } from "@/components/ui/progress";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { cn } from "@/lib/utils";
import {
  demoShop,
  subscriptionPayments,
  planUsage,
  homeKpis,
  type SubscriptionPayment,
} from "@/lib/mock-data";
import { formatFCFA, formatNumber, formatDate } from "@/lib/format";
import { ViewHeader } from "./view-header";

function paymentStatusBadge(s: SubscriptionPayment["status"]) {
  if (s === "success")
    return (
      <Badge className="bg-brand/10 text-brand hover:bg-brand/20">
        <CheckCircle2 className="size-3" /> Réussi
      </Badge>
    );
  if (s === "failed") return <Badge variant="destructive">Échec</Badge>;
  return (
    <Badge className="bg-amber-accent/15 text-amber-accent hover:bg-amber-accent/25">
      En attente
    </Badge>
  );
}

export function SubscriptionView() {
  const usagePercent = Math.round(
    (planUsage.messagesUsed / planUsage.messagesQuota) * 100,
  );

  return (
    <div className="space-y-5">
      <ViewHeader
        title="Abonnement"
        description="Plan, quotas de messagerie IA et historique de paiement."
        actions={
          <>
            <Button
              variant="outline"
              size="sm"
              disabled
              className="gap-1.5"
              title="Disponible prochainement"
            >
              <Lock className="size-3.5" /> Changer de plan
            </Button>
            <Button
              size="sm"
              className="gap-1.5"
              onClick={() =>
                toast.success("Paiement initié", {
                  description: "Wave / Orange Money — redirection.",
                })
              }
            >
              <CreditCard className="size-4" /> Payer maintenant
            </Button>
          </>
        }
      />

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        {/* Plan courant */}
        <Card className="lg:col-span-1">
          <CardHeader className="border-b pb-3">
            <CardTitle className="text-sm font-semibold">Plan courant</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3 py-4">
            <div className="flex items-center justify-between">
              <div>
                <div className="text-xs text-muted-foreground">Formule</div>
                <div className="text-xl font-bold">{demoShop.plan}</div>
              </div>
              <span className="flex size-10 items-center justify-center rounded-lg bg-brand text-brand-foreground">
                <Sparkles className="size-5" />
              </span>
            </div>
            <div className="flex items-end gap-1">
              <span className="text-3xl font-bold tabular-nums">
                {formatNumber(demoShop.planPrice)}
              </span>
              <span className="mb-1 text-sm text-muted-foreground">FCFA / mois</span>
            </div>
            <div className="flex items-center justify-between rounded-lg border p-3 text-sm">
              <div>
                <div className="text-xs text-muted-foreground">Statut</div>
                <div className="font-medium text-brand">Actif</div>
              </div>
              <div className="text-right">
                <div className="text-xs text-muted-foreground">
                  Prochain renouvellement
                </div>
                <div className="font-medium">{formatDate(demoShop.planRenewal)}</div>
              </div>
            </div>
            <div>
              <div className="mb-1 text-xs text-muted-foreground">
                Inclus dans le plan :
              </div>
              <ul className="space-y-1 text-sm">
                {[
                  "1 000 messages IA / mois",
                  "WhatsApp Business connecté",
                  "Catalogue & stock illimités",
                  "1 boutique · 3 employés",
                ].map((f) => (
                  <li key={f} className="flex items-center gap-2">
                    <CheckCircle2 className="size-3.5 text-brand" />
                    {f}
                  </li>
                ))}
              </ul>
            </div>
          </CardContent>
        </Card>

        {/* Conso du mois */}
        <Card className="lg:col-span-2">
          <CardHeader className="border-b pb-3">
            <CardTitle className="text-sm font-semibold">
              Consommation ce mois
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-4 py-4">
            <div className="space-y-1.5">
              <div className="flex items-baseline justify-between">
                <span className="text-sm font-medium">Messages IA</span>
                <span className="text-sm tabular-nums">
                  <span className="font-semibold">{planUsage.messagesUsed}</span>
                  <span className="text-muted-foreground">
                    {" "}
                    / {planUsage.messagesQuota}
                  </span>
                </span>
              </div>
              <Progress value={usagePercent} className="h-2.5" />
              <div className="flex items-center justify-between text-xs text-muted-foreground">
                <span>{usagePercent}% du quota utilisé</span>
                <span>
                  {planUsage.messagesQuota - planUsage.messagesUsed} restants
                </span>
              </div>
            </div>

            <div className="grid grid-cols-2 gap-3 border-t pt-3 sm:grid-cols-3">
              <UsageStat label="Conversations" value="43" />
              <UsageStat
                label="Coût IA estimé"
                value={formatFCFA(homeKpis.aiCostMonth)}
              />
              <UsageStat label="Employés" value="3 / 3" />
            </div>

            <div
              className={cn(
                "rounded-lg border p-3 text-xs",
                usagePercent > 80
                  ? "border-amber-accent/30 bg-amber-accent/10 text-amber-accent"
                  : "border-border bg-muted/30 text-muted-foreground",
              )}
            >
              {usagePercent > 80 ? (
                <>
                  ⚠️ Vous approchez du quota mensuel. Pensez à passer au plan
                  supérieur pour éviter toute interruption.
                </>
              ) : (
                <>
                  Consommation saine. Le quota se réinitialise le 15 de chaque
                  mois.
                </>
              )}
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Historique paiements */}
      <Card className="overflow-hidden">
        <CardHeader className="flex-row items-center justify-between border-b pb-3">
          <CardTitle className="flex items-center gap-2 text-sm font-semibold">
            <Receipt className="size-4 text-muted-foreground" />
            Historique des paiements
          </CardTitle>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => toast.info("Reçu PDF — bientôt")}
          >
            <Receipt className="size-4" /> Télécharger reçu
          </Button>
        </CardHeader>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Date</TableHead>
              <TableHead className="text-right">Montant</TableHead>
              <TableHead className="hidden sm:table-cell">Mode</TableHead>
              <TableHead className="hidden md:table-cell">Référence</TableHead>
              <TableHead>Statut</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {subscriptionPayments.map((p) => (
              <TableRow key={p.id}>
                <TableCell className="text-sm">{formatDate(p.date)}</TableCell>
                <TableCell className="text-right font-semibold tabular-nums">
                  {formatFCFA(p.amount)}
                </TableCell>
                <TableCell className="hidden sm:table-cell">{p.mode}</TableCell>
                <TableCell className="hidden text-xs text-muted-foreground md:table-cell">
                  {p.reference}
                </TableCell>
                <TableCell>{paymentStatusBadge(p.status)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Card>
    </div>
  );
}

function UsageStat({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border p-2.5">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="text-sm font-semibold">{value}</div>
    </div>
  );
}
