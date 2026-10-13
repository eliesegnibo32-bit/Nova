"use client";
import { UserCog } from "lucide-react";
import { Card } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { useAuthStore } from "@/stores/auth-store";

export function TeamView() {
  const user = useAuthStore((s) => s.user);
  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Équipe</h1>
      </div>
      <Card className="p-6">
        <div className="flex items-center gap-3">
          <div className="flex size-10 items-center justify-center rounded-full bg-brand text-brand-foreground text-sm font-semibold">
            {user?.fullName?.[0] || "U"}
          </div>
          <div>
            <div className="font-medium">{user?.fullName || "Vous"}</div>
            <div className="text-sm text-muted-foreground">Propriétaire</div>
          </div>
        </div>
      </Card>
      <Card className="p-8 text-center">
        <UserCog className="mx-auto mb-4 size-12 text-muted-foreground" />
        <p className="text-muted-foreground">Aucun autre membre dans l'équipe.</p>
        <Button className="mt-4" variant="outline">Inviter un employé</Button>
      </Card>
    </div>
  );
}
