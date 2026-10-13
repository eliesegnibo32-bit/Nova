"use client";
import { CreditCard } from "lucide-react";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";

export function SubscriptionView() {
  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-bold">Abonnement</h1>
      <Card className="p-6">
        <div className="flex items-center justify-between">
          <div>
            <h3 className="text-lg font-semibold">Plan Essentiel</h3>
            <p className="text-sm text-muted-foreground">10 000 FCFA / mois</p>
          </div>
          <Badge className="bg-emerald-500">Période d'essai</Badge>
        </div>
        <div className="mt-4 rounded-lg bg-muted p-3 text-sm text-muted-foreground">
          <div className="flex items-center gap-2">
            <CreditCard className="size-4" />
            <span>Période d'essai de 14 jours</span>
          </div>
        </div>
      </Card>
    </div>
  );
}
