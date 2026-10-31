"use client";

import * as React from "react";
import {
  Settings,
  Tag,
  Gauge,
  Megaphone,
  ShieldCheck,
  Pencil,
  Check,
  Lock,
} from "lucide-react";
import { toast } from "sonner";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";
import { formatFCFA } from "@/lib/format";
import { ViewHeader } from "./view-header";

/**
 * Vue Admin — Paramètres globaux.
 *
 * Le backend Go n'expose pas encore d'endpoint de configuration plateforme
 * (/api/admin/settings). On présente donc une vue en lecture seule avec des
 * valeurs par défaut alignées sur le cahier des charges, et des boutons
 * « Modifier » qui toastent « À implémenter » en attendant le branchement.
 */
export function AdminSettingsView() {
  const handleModify = (label: string) => {
    toast.info("À implémenter", {
      description: `La modification de « ${label} » sera disponible dès le déploiement de l'endpoint /api/admin/settings.`,
    });
  };

  return (
    <div className="space-y-5">
      <ViewHeader
        title="Paramètres globaux"
        description="Formules, tarifs, quotas IA, communications et sécurité de la plateforme NOVA."
      />

      {/* Bandeau read-only */}
      <div className="flex items-start gap-2 rounded-lg border border-amber-accent/40 bg-amber-accent/5 p-3 text-xs text-amber-accent">
        <Lock className="size-4 shrink-0" />
        <div>
          Les paramètres sont actuellement en <strong>lecture seule</strong>.
          Chaque section propose un bouton « Modifier » qui vous permettra de
          les ajuster dès que l&apos;endpoint{" "}
          <code className="rounded bg-amber-accent/10 px-1 py-0.5">/api/admin/settings</code>{" "}
          sera déployé côté backend.
        </div>
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        {/* Formules & tarifs */}
        <Card>
          <CardHeader className="border-b pb-3">
            <div className="flex items-center justify-between gap-2">
              <CardTitle className="flex items-center gap-2 text-sm font-semibold">
                <Tag className="size-4 text-brand" /> Formules et tarifs
              </CardTitle>
              <Button
                variant="outline"
                size="sm"
                className="h-8 gap-1 text-xs"
                onClick={() => handleModify("Formules et tarifs")}
              >
                <Pencil className="size-3.5" /> Modifier
              </Button>
            </div>
          </CardHeader>
          <CardContent className="space-y-3 py-4">
            <PriceRow label="Formule Essentiel" value={formatFCFA(10000)} period="/ mois" />
            <PriceRow label="Frais d'installation (1 fois)" value={formatFCFA(20000)} />
            <Separator />
            <div className="rounded-lg border border-dashed bg-muted/30 p-3 text-xs text-muted-foreground">
              Le catalogue des formules (Essentiel, Business, Premium) et leurs
              quotas inclus seront éditables ici : prix mensuel, frais
              d&apos;installation, fonctionnalités incluses.
            </div>
          </CardContent>
        </Card>

        {/* Quotas */}
        <Card>
          <CardHeader className="border-b pb-3">
            <div className="flex items-center justify-between gap-2">
              <CardTitle className="flex items-center gap-2 text-sm font-semibold">
                <Gauge className="size-4 text-brand" /> Quotas IA
              </CardTitle>
              <Button
                variant="outline"
                size="sm"
                className="h-8 gap-1 text-xs"
                onClick={() => handleModify("Quotas IA")}
              >
                <Pencil className="size-3.5" /> Modifier
              </Button>
            </div>
          </CardHeader>
          <CardContent className="space-y-3 py-4">
            <ConfigRow label="Messages IA par boutique (Essentiel)" value="1 000 / mois" />
            <ConfigRow label="Messages IA par boutique (Business)" value="5 000 / mois" />
            <ConfigRow label="Messages IA par boutique (Premium)" value="20 000 / mois" />
            <Separator />
            <ConfigRow label="Plafond global plateforme" value="500 000 / mois" tone="amber" />
            <div className="rounded-lg border border-dashed bg-muted/30 p-3 text-xs text-muted-foreground">
              Dépassement : alerte email à l&apos;admin + blocage soft (les
              conversations basculent en mode humain).
            </div>
          </CardContent>
        </Card>

        {/* Communications */}
        <Card>
          <CardHeader className="border-b pb-3">
            <div className="flex items-center justify-between gap-2">
              <CardTitle className="flex items-center gap-2 text-sm font-semibold">
                <Megaphone className="size-4 text-brand" /> Communications
              </CardTitle>
              <Button
                variant="outline"
                size="sm"
                className="h-8 gap-1 text-xs"
                onClick={() => handleModify("Communications")}
              >
                <Pencil className="size-3.5" /> Modifier
              </Button>
            </div>
          </CardHeader>
          <CardContent className="space-y-3 py-4">
            <div className="text-xs text-muted-foreground">
              Annonces ciblées par statut de boutique et par formule.
            </div>
            <div className="space-y-2">
              <ToggleRow
                label="Annonce aux boutiques en essai"
                description="Rappel de fin d'essai + lien de souscription"
                defaultChecked
                onToggle={() => handleModify("Annonce essai")}
              />
              <ToggleRow
                label="Annonce aux boutiques en retard"
                description="Rappel de paiement + délai de grâce"
                defaultChecked
                onToggle={() => handleModify("Annonce retard")}
              />
              <ToggleRow
                label="Annonce aux boutiques suspendues"
                description="Procédure de réactivation"
                onToggle={() => handleModify("Annonce suspendues")}
              />
              <ToggleRow
                label="Newsletter mensuelle (toutes formules)"
                description="Nouveautés produit + bonnes pratiques"
                onToggle={() => handleModify("Newsletter")}
              />
            </div>
          </CardContent>
        </Card>

        {/* Sécurité */}
        <Card>
          <CardHeader className="border-b pb-3">
            <div className="flex items-center justify-between gap-2">
              <CardTitle className="flex items-center gap-2 text-sm font-semibold">
                <ShieldCheck className="size-4 text-brand" /> Sécurité
              </CardTitle>
              <Button
                variant="outline"
                size="sm"
                className="h-8 gap-1 text-xs"
                onClick={() => handleModify("Sécurité")}
              >
                <Pencil className="size-3.5" /> Modifier
              </Button>
            </div>
          </CardHeader>
          <CardContent className="space-y-3 py-4">
            <ToggleRow
              label="MFA obligatoire (admins)"
              description="TOTP obligatoire pour super_admin et admin"
              defaultChecked
              onToggle={() => handleModify("MFA obligatoire")}
            />
            <Separator />
            <ConfigRow label="Durée de session (admin)" value="8 heures" />
            <ConfigRow label="Tentatives de connexion max" value="5 / 15 min" tone="amber" />
            <ConfigRow label="Verrouillage après échecs" value="30 minutes" tone="rose" />
            <ConfigRow label="Rotation des mots de passe" value="90 jours" />
          </CardContent>
        </Card>
      </div>

      {/* Récapitulatif */}
      <Card>
        <CardHeader className="border-b pb-3">
          <CardTitle className="flex items-center gap-2 text-sm font-semibold">
            <Settings className="size-4 text-brand" /> État du déploiement
          </CardTitle>
        </CardHeader>
        <CardContent className="py-4">
          <ul className="space-y-2 text-sm">
            <DeployRow label="Formules et tarifs" available={false} />
            <DeployRow label="Quotas IA" available={false} />
            <DeployRow label="Communications ciblées" available={false} />
            <DeployRow label="Politique de sécurité" available={false} />
          </ul>
        </CardContent>
      </Card>
    </div>
  );
}

// ====== Sous-composants ======

function PriceRow({
  label,
  value,
  period,
}: {
  label: string;
  value: string;
  period?: string;
}) {
  return (
    <div className="flex items-center justify-between gap-2 rounded-lg border bg-card p-2.5">
      <span className="text-sm text-muted-foreground">{label}</span>
      <span className="flex items-baseline gap-1">
        <span className="text-base font-semibold tabular-nums">{value}</span>
        {period && <span className="text-xs text-muted-foreground">{period}</span>}
      </span>
    </div>
  );
}

function ConfigRow({
  label,
  value,
  tone = "default",
}: {
  label: string;
  value: string;
  tone?: "default" | "amber" | "rose";
}) {
  const toneCls =
    tone === "amber"
      ? "text-amber-accent"
      : tone === "rose"
        ? "text-rose-600 dark:text-rose-400"
        : "text-foreground";
  return (
    <div className="flex items-center justify-between gap-2 px-1 py-1">
      <span className="text-sm text-muted-foreground">{label}</span>
      <span className={cn("text-sm font-semibold tabular-nums", toneCls)}>{value}</span>
    </div>
  );
}

function ToggleRow({
  label,
  description,
  defaultChecked,
  onToggle,
}: {
  label: string;
  description?: string;
  defaultChecked?: boolean;
  onToggle?: (checked: boolean) => void;
}) {
  return (
    <div className="flex items-center justify-between gap-2 rounded-lg border bg-card p-2.5">
      <div className="min-w-0">
        <div className="text-sm font-medium">{label}</div>
        {description && (
          <div className="text-[11px] text-muted-foreground">{description}</div>
        )}
      </div>
      <Switch
        defaultChecked={defaultChecked}
        onCheckedChange={(c) => {
          onToggle?.(c);
          if (c) toast.success("Activé", { description: label });
        }}
        aria-label={label}
      />
    </div>
  );
}

function DeployRow({ label, available }: { label: string; available: boolean }) {
  return (
    <li className="flex items-center justify-between gap-2 rounded-lg border bg-card p-2.5">
      <span className="text-sm">{label}</span>
      {available ? (
        <Badge className="bg-emerald-500/10 text-emerald-700 hover:bg-emerald-500/20 dark:text-emerald-400">
          <Check className="size-3" /> Disponible
        </Badge>
      ) : (
        <Badge variant="outline" className="text-muted-foreground">
          En attente backend
        </Badge>
      )}
    </li>
  );
}
