"use client";
import * as React from "react";
import { ShoppingCart } from "lucide-react";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { useAuthStore } from "@/stores/auth-store";
import { mockOrders } from "@/lib/mock-data";
import { formatFCFA } from "@/lib/format";

export function OrdersView() {
  const isDemo = useAuthStore((s) => s.isDemo);
  if (isDemo && mockOrders?.length > 0) {
    return (
      <div className="space-y-4">
        <h1 className="text-2xl font-bold">Commandes</h1>
        <Card className="overflow-hidden">
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="border-b bg-muted/50">
                <tr>
                  <th className="p-3 text-left">N°</th>
                  <th className="p-3 text-left">Client</th>
                  <th className="p-3 text-right">Total</th>
                  <th className="p-3 text-center">Statut</th>
                </tr>
              </thead>
              <tbody>
                {mockOrders.slice(0, 8).map((o: any, i: number) => (
                  <tr key={i} className="border-b last:border-0">
                    <td className="p-3">#{o.number || o.id || i + 1}</td>
                    <td className="p-3">{o.customerName || o.customer || "Client"}</td>
                    <td className="p-3 text-right">{formatFCFA(o.total || 0)}</td>
                    <td className="p-3 text-center"><Badge variant="secondary">{o.status || "pending"}</Badge></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Card>
      </div>
    );
  }
  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-bold">Commandes</h1>
      <Card className="p-8 text-center">
        <ShoppingCart className="mx-auto mb-4 size-12 text-muted-foreground" />
        <p className="text-muted-foreground">Aucune commande pour le moment.</p>
      </Card>
    </div>
  );
}
