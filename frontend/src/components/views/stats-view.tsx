"use client";
import { BarChart3 } from "lucide-react";
import { Card } from "@/components/ui/card";

export function StatsView() {
  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-bold">Statistiques</h1>
      <Card className="p-8 text-center">
        <BarChart3 className="mx-auto mb-4 size-12 text-muted-foreground" />
        <p className="text-muted-foreground">Aucune statistique disponible pour le moment.</p>
        <p className="mt-2 text-sm text-muted-foreground">
          Les statistiques apparaîtront dès que vous aurez des commandes.
        </p>
      </Card>
    </div>
  );
}
