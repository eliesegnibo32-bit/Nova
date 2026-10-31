"use client";

import * as React from "react";
import { Users, ShieldCheck, Bell, Sparkles } from "lucide-react";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { useAuthStore } from "@/stores/auth-store";
import { ViewHeader } from "./view-header";

/**
 * Vue Admin — Utilisateurs.
 *
 * Le backend Go n'expose pas encore d'endpoint dédié à la liste des
 * utilisateurs (la table `users` est lue par /api/auth/me et /api/team mais
 * pas par une route admin globale). On présente donc un état vide soigné,
 * avec un appel à l'action pour la suite.
 *
 * Dès que l'endpoint `GET /api/admin/users` sera déployé côté Go, il faudra
 * remplacer le contenu ci-dessous par un vrai tableau (cf. admin-shops-view
 * pour le modèle de table + filtre + skeleton).
 */
export function AdminUsersView() {
  const user = useAuthStore((s) => s.user);
  const isAdmin = user?.role === "super_admin";

  return (
    <div className="space-y-5">
      <ViewHeader
        title="Administration — Utilisateurs"
        description="Gérez les comptes utilisateurs, rôles et accès à la plateforme NOVA."
      />

      <Card className="border-dashed">
        <CardContent className="flex flex-col items-center justify-center gap-4 px-6 py-16 text-center">
          <span className="flex size-14 items-center justify-center rounded-full bg-brand/10 text-brand">
            <Users className="size-7" />
          </span>
          <div className="text-lg font-semibold">
            Gestion des utilisateurs — disponible prochainement
          </div>
          <p className="max-w-md text-sm text-muted-foreground">
            L&apos;endpoint <code className="rounded bg-muted px-1.5 py-0.5 text-xs">GET /api/admin/users</code>{" "}
            n&apos;est pas encore exposé par le backend. Une fois disponible, vous
            pourrez ici lister tous les comptes, modifier les rôles (owner,
            employee, admin, super_admin), suspendre un accès et consulter
            l&apos;activité par utilisateur.
          </p>

          <div className="mt-2 flex flex-wrap items-center justify-center gap-2">
            <Badge variant="outline" className="gap-1">
              <ShieldCheck className="size-3 text-brand" />
              {isAdmin ? "Rôle : super_admin" : "Rôle : admin"}
            </Badge>
            <Badge variant="outline" className="gap-1">
              <Sparkles className="size-3 text-amber-accent" />
              Bientôt
            </Badge>
          </div>

          <Button
            variant="outline"
            size="sm"
            className="mt-2 gap-2"
            onClick={() => {
              if (typeof window !== "undefined") {
                window.localStorage.setItem(
                  "nova-admin-users-notify",
                  "1",
                );
              }
            }}
          >
            <Bell className="size-4" />
            Prévenez-moi quand c&apos;est prêt
          </Button>
        </CardContent>
      </Card>

      {/* Aperçu des fonctionnalités à venir */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
        <Card>
          <CardHeader className="border-b pb-3">
            <CardTitle className="flex items-center gap-2 text-sm font-semibold">
              <ShieldCheck className="size-4 text-brand" /> Rôles & permissions
            </CardTitle>
          </CardHeader>
          <CardContent className="py-4 text-sm text-muted-foreground">
            Gestion fine des rôles plateforme (super_admin, admin) et boutique
            (owner, manager, employee) avec matrice de permissions.
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="border-b pb-3">
            <CardTitle className="flex items-center gap-2 text-sm font-semibold">
              <Users className="size-4 text-brand" /> Comptes & accès
            </CardTitle>
          </CardHeader>
          <CardContent className="py-4 text-sm text-muted-foreground">
            Liste paginée des utilisateurs, recherche, suspension, réactivation
            et reset de mot de passe en un clic.
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="border-b pb-3">
            <CardTitle className="flex items-center gap-2 text-sm font-semibold">
              <Bell className="size-4 text-amber-accent" /> Audit & activité
            </CardTitle>
          </CardHeader>
          <CardContent className="py-4 text-sm text-muted-foreground">
            Historique des connexions, 2FA activée, dernière action, et
            notifications de sécurité par e-mail.
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
