"use client";
import { Users } from "lucide-react";
import { Card } from "@/components/ui/card";
import { useAuthStore } from "@/stores/auth-store";
import { mockCustomers } from "@/lib/mock-data";

export function CustomersView() {
  const isDemo = useAuthStore((s) => s.isDemo);
  if (isDemo && mockCustomers?.length > 0) {
    return (
      <div className="space-y-4">
        <h1 className="text-2xl font-bold">Clients</h1>
        <Card className="overflow-hidden">
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="border-b bg-muted/50"><tr><th className="p-3 text-left">Nom</th><th className="p-3 text-left">Téléphone</th><th className="p-3 text-right">Commandes</th></tr></thead>
              <tbody>
                {mockCustomers.slice(0, 8).map((c: any, i: number) => (
                  <tr key={i} className="border-b last:border-0"><td className="p-3">{c.name || c.fullName || "Client"}</td><td className="p-3">{c.phone || "—"}</td><td className="p-3 text-right">{c.orders || c.totalOrders || 0}</td></tr>
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
      <h1 className="text-2xl font-bold">Clients</h1>
      <Card className="p-8 text-center">
        <Users className="mx-auto mb-4 size-12 text-muted-foreground" />
        <p className="text-muted-foreground">Aucun client pour le moment.</p>
      </Card>
    </div>
  );
}
