"use client";

import * as React from "react";
import {
  ScrollText,
  Search,
  Filter,
  CalendarRange,
  ShieldCheck,
  CheckCircle2,
  XCircle,
  Sparkles,
} from "lucide-react";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ViewHeader } from "./view-header";

// ====== Types (modèle de données prévu pour le futur endpoint) ======

type AuditAction =
  | "shop.suspend"
  | "shop.reactivate"
  | "shop.create"
  | "shop.update"
  | "shop.terminate"
  | "subscription.update"
  | "subscription.payment"
  | "user.role_change"
  | "user.login"
  | "config.update"
  | string;

type AuditResult = "success" | "failure";

interface AuditEntry {
  id: string;
  actor: string;
  actorRole?: string;
  timestamp: string; // ISO UTC
  objectType: string; // shop, subscription, user, config…
  objectId?: string;
  objectLabel?: string;
  action: AuditAction;
  reason?: string | null;
  result: AuditResult;
  ip?: string;
}

// ====== Helpers ======

function actionBadge(action: AuditAction) {
  const actionStr = String(action);
  if (actionStr.startsWith("shop.suspend"))
    return (
      <Badge variant="destructive" className="gap-1">
        {action}
      </Badge>
    );
  if (actionStr.startsWith("shop.terminate"))
    return (
      <Badge variant="outline" className="gap-1 text-muted-foreground">
        {action}
      </Badge>
    );
  if (actionStr.startsWith("shop.reactivate"))
    return (
      <Badge className="bg-brand/10 text-brand hover:bg-brand/20">{action}</Badge>
    );
  if (actionStr.startsWith("subscription.payment"))
    return (
      <Badge className="bg-amber-accent/15 text-amber-accent hover:bg-amber-accent/25">
        {action}
      </Badge>
    );
  return <Badge variant="outline">{action}</Badge>;
}

function resultBadge(r: AuditResult) {
  if (r === "success")
    return (
      <Badge className="bg-emerald-500/10 text-emerald-700 hover:bg-emerald-500/20 dark:text-emerald-400">
        <CheckCircle2 className="size-3" /> Succès
      </Badge>
    );
  return (
    <Badge variant="destructive">
      <XCircle className="size-3" /> Échec
    </Badge>
  );
}

// ====== Vue ======

export function AdminAuditView() {
  const [query, setQuery] = React.useState("");
  const [action, setAction] = React.useState<string>("all");
  const [from, setFrom] = React.useState<string>("");
  const [to, setTo] = React.useState<string>("");

  // Le backend n'expose pas encore d'endpoint /api/admin/audit — placeholder.
  const entries: AuditEntry[] = [];

  const filtered = React.useMemo(() => {
    return entries.filter((e) => {
      if (action !== "all" && !String(e.action).startsWith(action)) return false;
      if (query) {
        const q = query.toLowerCase();
        const hay = `${e.actor} ${e.objectType} ${e.objectLabel ?? ""} ${e.objectId ?? ""}`.toLowerCase();
        if (!hay.includes(q)) return false;
      }
      if (from) {
        const fromTs = new Date(from).getTime();
        if (new Date(e.timestamp).getTime() < fromTs) return false;
      }
      if (to) {
        const toTs = new Date(to).getTime();
        if (new Date(e.timestamp).getTime() > toTs) return false;
      }
      return true;
    });
  }, [entries, query, action, from, to]);

  return (
    <div className="space-y-5">
      <ViewHeader
        title="Journal d'audit"
        description="Trace complète des actions administratives (suspensions, résiliations, paiements, changements de rôle)."
      />

      {/* Filtres */}
      <Card>
        <CardHeader className="border-b pb-3">
          <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
            <CardTitle className="flex items-center gap-2 text-sm font-semibold">
              <Filter className="size-4 text-brand" /> Filtres
            </CardTitle>
            <div className="flex flex-wrap items-center gap-2">
              <div className="relative">
                <Search className="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                <Input
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  placeholder="Rechercher (acteur, objet)…"
                  className="h-9 w-full pl-8 sm:w-[260px]"
                  aria-label="Rechercher dans le journal d'audit"
                />
              </div>
              <Select value={action} onValueChange={setAction}>
                <SelectTrigger className="h-9 w-[200px]" aria-label="Filtrer par type d'action">
                  <SelectValue placeholder="Toutes les actions" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">Toutes les actions</SelectItem>
                  <SelectItem value="shop.suspend">shop.suspend</SelectItem>
                  <SelectItem value="shop.reactivate">shop.reactivate</SelectItem>
                  <SelectItem value="shop.terminate">shop.terminate</SelectItem>
                  <SelectItem value="shop.create">shop.create</SelectItem>
                  <SelectItem value="shop.update">shop.update</SelectItem>
                  <SelectItem value="subscription.payment">subscription.payment</SelectItem>
                  <SelectItem value="subscription.update">subscription.update</SelectItem>
                  <SelectItem value="user.role_change">user.role_change</SelectItem>
                  <SelectItem value="config.update">config.update</SelectItem>
                </SelectContent>
              </Select>
              <div className="flex items-center gap-1">
                <CalendarRange className="size-4 text-muted-foreground" />
                <Input
                  type="date"
                  value={from}
                  onChange={(e) => setFrom(e.target.value)}
                  className="h-9 w-[150px]"
                  aria-label="Date de début"
                />
                <span className="text-xs text-muted-foreground">→</span>
                <Input
                  type="date"
                  value={to}
                  onChange={(e) => setTo(e.target.value)}
                  className="h-9 w-[150px]"
                  aria-label="Date de fin"
                />
              </div>
            </div>
          </div>
        </CardHeader>
        <CardContent className="py-2">
          {filtered.length === 0 ? (
            <div className="flex flex-col items-center justify-center gap-3 py-12 text-center">
              <span className="flex size-14 items-center justify-center rounded-full bg-brand/10 text-brand">
                <ScrollText className="size-7" />
              </span>
              <div className="text-base font-semibold">
                Journal d&apos;audit — disponible prochainement
              </div>
              <p className="max-w-md text-sm text-muted-foreground">
                L&apos;endpoint <code className="rounded bg-muted px-1.5 py-0.5 text-xs">GET /api/admin/audit</code> n&apos;est pas encore
                exposé par le backend. Une fois disponible, chaque action
                administrative (suspension, réactivation, résiliation, changement
                de rôle, mise à jour de config) sera tracée ici avec acteur,
                horodatage UTC, objet, motif et résultat.
              </p>
              <Badge variant="outline" className="gap-1">
                <Sparkles className="size-3 text-amber-accent" />
                Bientôt
              </Badge>
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Acteur</TableHead>
                  <TableHead className="text-right">Date/heure (UTC)</TableHead>
                  <TableHead>Objet</TableHead>
                  <TableHead>Action</TableHead>
                  <TableHead>Motif</TableHead>
                  <TableHead>Résultat</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filtered.map((e) => (
                  <TableRow key={e.id}>
                    <TableCell>
                      <div className="font-medium">{e.actor}</div>
                      {e.actorRole && (
                        <div className="text-[11px] text-muted-foreground">{e.actorRole}</div>
                      )}
                    </TableCell>
                    <TableCell className="text-right tabular-nums text-muted-foreground">
                      {e.timestamp}
                    </TableCell>
                    <TableCell>
                      <div className="font-medium">{e.objectLabel || e.objectType}</div>
                      {e.objectId && (
                        <div className="text-[11px] text-muted-foreground">{e.objectId}</div>
                      )}
                    </TableCell>
                    <TableCell>{actionBadge(e.action)}</TableCell>
                    <TableCell className="text-muted-foreground">
                      {e.reason || "—"}
                    </TableCell>
                    <TableCell>{resultBadge(e.result)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      {/* Aperçu des fonctionnalités à venir */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
        <Card>
          <CardHeader className="border-b pb-3">
            <CardTitle className="flex items-center gap-2 text-sm font-semibold">
              <ShieldCheck className="size-4 text-brand" /> Traçabilité complète
            </CardTitle>
          </CardHeader>
          <CardContent className="py-4 text-sm text-muted-foreground">
            Chaque mutation sensible (suspension, résiliation, paiement, rôle)
            est journalisée avec acteur, horodatage UTC, objet, motif et résultat.
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="border-b pb-3">
            <CardTitle className="flex items-center gap-2 text-sm font-semibold">
              <Filter className="size-4 text-brand" /> Recherche avancée
            </CardTitle>
          </CardHeader>
          <CardContent className="py-4 text-sm text-muted-foreground">
            Filtres par type d&apos;action, plage de dates, recherche plein texte
            sur l&apos;acteur ou l&apos;objet. Export CSV à venir pour conformité.
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="border-b pb-3">
            <CardTitle className="flex items-center gap-2 text-sm font-semibold">
              <ScrollText className="size-4 text-brand" /> Conservation longue
            </CardTitle>
          </CardHeader>
          <CardContent className="py-4 text-sm text-muted-foreground">
            Les entrées d&apos;audit seront conservées 12 mois minimum,
            immuables, avec hachage horodaté pour preuve d&apos;intégrité.
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
