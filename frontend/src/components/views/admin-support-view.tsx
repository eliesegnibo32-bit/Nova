"use client";

import * as React from "react";
import {
  LifeBuoy,
  Search,
  AlertOctagon,
  Clock,
  CheckCircle2,
  Inbox,
  Sparkles,
} from "lucide-react";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
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
import { cn } from "@/lib/utils";
import { ViewHeader } from "./view-header";

// ====== Types (modèle de données prévu pour le futur endpoint) ======

type TicketPriority = "P1" | "P2" | "P3" | "P4";
type TicketState =
  | "nouveau"
  | "en_cours"
  | "attente_commercant"
  | "attente_fournisseur"
  | "resolu"
  | "ferme";

interface SupportTicket {
  id: string;
  requester: string;
  shop?: string;
  subject: string;
  priority: TicketPriority;
  state: TicketState;
  createdAt: string;
  assignee?: string | null;
  slaFirstResponseMin?: number | null;
  overdue?: boolean;
}

// ====== Badges ======

function priorityBadge(p: TicketPriority) {
  switch (p) {
    case "P1":
      return (
        <Badge variant="destructive">
          <AlertOctagon className="size-3" /> P1 Critique
        </Badge>
      );
    case "P2":
      return (
        <Badge className="bg-orange-500/10 text-orange-600 hover:bg-orange-500/20 dark:text-orange-400">
          P2 Haute
        </Badge>
      );
    case "P3":
      return (
        <Badge className="bg-blue-500/10 text-blue-600 hover:bg-blue-500/20 dark:text-blue-400">
          P3 Normale
        </Badge>
      );
    case "P4":
      return <Badge variant="outline">P4 Faible</Badge>;
    default:
      return <Badge variant="outline">{p}</Badge>;
  }
}

function stateBadge(s: TicketState) {
  switch (s) {
    case "nouveau":
      return (
        <Badge className="bg-blue-500/10 text-blue-600 hover:bg-blue-500/20 dark:text-blue-400">
          Nouveau
        </Badge>
      );
    case "en_cours":
      return (
        <Badge className="bg-brand/10 text-brand hover:bg-brand/20">En cours</Badge>
      );
    case "attente_commercant":
      return (
        <Badge className="bg-amber-accent/15 text-amber-accent hover:bg-amber-accent/25">
          Attente commerçant
        </Badge>
      );
    case "attente_fournisseur":
      return (
        <Badge variant="outline" className="border-amber-accent/40 text-amber-accent">
          Attente fournisseur
        </Badge>
      );
    case "resolu":
      return (
        <Badge className="bg-emerald-500/10 text-emerald-700 hover:bg-emerald-500/20 dark:text-emerald-400">
          <CheckCircle2 className="size-3" /> Résolu
        </Badge>
      );
    case "ferme":
      return <Badge variant="outline">Fermé</Badge>;
    default:
      return <Badge variant="outline">{s}</Badge>;
  }
}

// ====== KPI cards ======

interface KpiProps {
  icon: React.ComponentType<{ className?: string }>;
  label: string;
  value: string;
  tone: "brand" | "amber" | "rose" | "blue";
}

const kpiTones: Record<KpiProps["tone"], string> = {
  brand: "bg-brand/10 text-brand",
  amber: "bg-amber-accent/15 text-amber-accent",
  rose: "bg-rose-500/10 text-rose-600 dark:text-rose-400",
  blue: "bg-blue-500/10 text-blue-600 dark:text-blue-400",
};

function KpiCard({ icon: Icon, label, value, tone }: KpiProps) {
  return (
    <Card>
      <CardContent className="flex items-start justify-between gap-3 py-5">
        <div className="min-w-0">
          <div className="text-xs font-medium text-muted-foreground">{label}</div>
          <div className="mt-1 text-2xl font-bold tracking-tight tabular-nums">{value}</div>
        </div>
        <span
          className={cn(
            "flex size-10 shrink-0 items-center justify-center rounded-lg",
            kpiTones[tone],
          )}
        >
          <Icon className="size-5" />
        </span>
      </CardContent>
    </Card>
  );
}

// ====== Vue ======

export function AdminSupportView() {
  const [query, setQuery] = React.useState("");
  const [priority, setPriority] = React.useState<string>("all");
  const [state, setState] = React.useState<string>("all");

  // Le backend n'expose pas encore d'endpoint tickets — placeholder.
  const tickets: SupportTicket[] = [];

  const filtered = React.useMemo(() => {
    return tickets.filter((t) => {
      if (priority !== "all" && t.priority !== priority) return false;
      if (state !== "all" && t.state !== state) return false;
      if (query) {
        const q = query.toLowerCase();
        const hay = `${t.requester} ${t.subject} ${t.shop ?? ""} ${t.assignee ?? ""}`.toLowerCase();
        if (!hay.includes(q)) return false;
      }
      return true;
    });
  }, [tickets, query, priority, state]);

  return (
    <div className="space-y-5">
      <ViewHeader
        title="Support & incidents"
        description="Tickets clients, priorités, SLA de première réponse et de résolution."
      />

      {/* KPI cards */}
      <div className="grid grid-cols-2 gap-3 sm:gap-4 lg:grid-cols-4">
        <KpiCard icon={Inbox} label="Tickets ouverts" value="0" tone="blue" />
        <KpiCard icon={AlertOctagon} label="Urgents (P1)" value="0" tone="rose" />
        <KpiCard icon={Clock} label="En retard (SLA)" value="0" tone="amber" />
        <KpiCard icon={CheckCircle2} label="Résolus (30j)" value="0" tone="brand" />
      </div>

      {/* Table des tickets (structure prête) */}
      <Card>
        <CardHeader className="border-b pb-3">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <CardTitle className="text-sm font-semibold">
              Tickets ({filtered.length})
            </CardTitle>
            <div className="flex flex-wrap items-center gap-2">
              <div className="relative">
                <Search className="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                <Input
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  placeholder="Rechercher (demandeur, sujet)…"
                  className="h-9 w-full pl-8 sm:w-[240px]"
                  aria-label="Rechercher un ticket"
                />
              </div>
              <Select value={priority} onValueChange={setPriority}>
                <SelectTrigger className="h-9 w-[150px]" aria-label="Filtrer par priorité">
                  <SelectValue placeholder="Toutes priorités" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">Toutes priorités</SelectItem>
                  <SelectItem value="P1">P1 — Critique</SelectItem>
                  <SelectItem value="P2">P2 — Haute</SelectItem>
                  <SelectItem value="P3">P3 — Normale</SelectItem>
                  <SelectItem value="P4">P4 — Faible</SelectItem>
                </SelectContent>
              </Select>
              <Select value={state} onValueChange={setState}>
                <SelectTrigger className="h-9 w-[180px]" aria-label="Filtrer par état">
                  <SelectValue placeholder="Tous les états" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">Tous les états</SelectItem>
                  <SelectItem value="nouveau">Nouveau</SelectItem>
                  <SelectItem value="en_cours">En cours</SelectItem>
                  <SelectItem value="attente_commercant">Attente commerçant</SelectItem>
                  <SelectItem value="attente_fournisseur">Attente fournisseur</SelectItem>
                  <SelectItem value="resolu">Résolu</SelectItem>
                  <SelectItem value="ferme">Fermé</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>
        </CardHeader>
        <CardContent className="py-2">
          {filtered.length === 0 ? (
            <div className="flex flex-col items-center justify-center gap-3 py-12 text-center">
              <span className="flex size-14 items-center justify-center rounded-full bg-brand/10 text-brand">
                <LifeBuoy className="size-7" />
              </span>
              <div className="text-base font-semibold">
                Système de tickets — disponible prochainement
              </div>
              <p className="max-w-md text-sm text-muted-foreground">
                L&apos;endpoint <code className="rounded bg-muted px-1.5 py-0.5 text-xs">GET /api/admin/tickets</code> n&apos;est pas encore exposé
                par le backend. Une fois disponible, vous pourrez ici suivre les
                demandes support, les priorités (P1-P4), les SLA de première
                réponse et de résolution, et les assigner à votre équipe.
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
                  <TableHead>Demandeur</TableHead>
                  <TableHead>Sujet</TableHead>
                  <TableHead>Priorité</TableHead>
                  <TableHead>État</TableHead>
                  <TableHead className="text-right">Date</TableHead>
                  <TableHead>Responsable</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filtered.map((t) => (
                  <TableRow key={t.id}>
                    <TableCell>
                      <div className="font-medium">{t.requester}</div>
                      {t.shop && (
                        <div className="text-[11px] text-muted-foreground">{t.shop}</div>
                      )}
                    </TableCell>
                    <TableCell>{t.subject}</TableCell>
                    <TableCell>{priorityBadge(t.priority)}</TableCell>
                    <TableCell>{stateBadge(t.state)}</TableCell>
                    <TableCell className="text-right tabular-nums text-muted-foreground">
                      {t.createdAt}
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {t.assignee || "—"}
                    </TableCell>
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
              <AlertOctagon className="size-4 text-rose-600" /> Priorités & SLA
            </CardTitle>
          </CardHeader>
          <CardContent className="py-4 text-sm text-muted-foreground">
            Niveaux P1 à P4 avec cibles de première réponse et de résolution.
            Alertes automatiques en cas de dépassement de SLA.
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="border-b pb-3">
            <CardTitle className="flex items-center gap-2 text-sm font-semibold">
              <LifeBuoy className="size-4 text-brand" /> File d&apos;assignation
            </CardTitle>
          </CardHeader>
          <CardContent className="py-4 text-sm text-muted-foreground">
            Assignation manuelle ou automatique, transferts entre agents,
            commentaires internes et historique complet par ticket.
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="border-b pb-3">
            <CardTitle className="flex items-center gap-2 text-sm font-semibold">
              <CheckCircle2 className="size-4 text-brand" /> Reporting support
            </CardTitle>
          </CardHeader>
          <CardContent className="py-4 text-sm text-muted-foreground">
            Volume mensuel, taux de résolution, temps moyen de réponse et
            satisfaction client (CSAT) agrégés par période.
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
