"use client";

import * as React from "react";
import {
  Store,
  Search,
  AlertTriangle,
  CheckCircle2,
  PauseCircle,
  Loader2,
  RefreshCw,
  ExternalLink,
  MoreHorizontal,
  PlayCircle,
  XOctagon,
  Eye,
  Phone,
  Mail,
  User as UserIcon,
  MessageSquare,
  Cpu,
  History,
  AlertOctagon,
  ShieldCheck,
} from "lucide-react";
import { toast } from "sonner";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Skeleton } from "@/components/ui/skeleton";
import { Separator } from "@/components/ui/separator";
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
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  adminApi,
  ApiError,
  type AdminSubscriptionItem,
  type Shop,
} from "@/lib/api";
import { formatDate, formatDateTime } from "@/lib/format";
import { ViewHeader } from "./view-header";

const NA = "non disponible";

/**
 * Statut combiné boutique + abonnement (aligné sur le cahier des charges).
 * Retourne { label, tone } où tone ∈ { brand, amber, blue, rose, muted }.
 */
function combinedStatus(shop: Shop, sub?: AdminSubscriptionItem) {
  // Suspendue prioritaire (action admin)
  if (shop.status === "suspended") return { label: "Suspendue", tone: "rose" as const };
  // Résiliée (abonnement terminated)
  if (sub?.status === "terminated") return { label: "Résiliée", tone: "muted" as const };
  // En grâce (abonnement grace_period)
  if (sub?.status === "grace_period") return { label: "En grâce", tone: "blue" as const };
  // En essai
  if (sub?.status === "trial") return { label: "Essai", tone: "blue" as const };
  // En retard
  if (sub?.status === "late") return { label: "En retard", tone: "amber" as const };
  // En attente (pending / draft)
  if (shop.status === "pending" || shop.status === "draft")
    return { label: "En attente", tone: "amber" as const };
  // Active
  if (shop.status === "active" && (!sub || sub.status === "active"))
    return { label: "Active", tone: "brand" as const };
  return { label: shop.status || "—", tone: "muted" as const };
}

const statusToneClasses: Record<string, string> = {
  brand: "bg-brand/10 text-brand hover:bg-brand/20",
  amber: "bg-amber-accent/15 text-amber-accent hover:bg-amber-accent/25",
  blue: "bg-blue-500/10 text-blue-600 hover:bg-blue-500/20 dark:text-blue-400",
  rose: "bg-rose-500/10 text-rose-600 hover:bg-rose-500/20 dark:text-rose-400",
  muted: "bg-muted text-muted-foreground",
};

function StatusBadge({ shop, sub }: { shop: Shop; sub?: AdminSubscriptionItem }) {
  const { label, tone } = combinedStatus(shop, sub);
  return (
    <Badge className={statusToneClasses[tone]} variant="secondary">
      {label}
    </Badge>
  );
}

/** État de paiement (basé sur l'abonnement). */
function paymentStateBadge(sub?: AdminSubscriptionItem) {
  if (!sub) return <Badge variant="outline">—</Badge>;
  switch (sub.status) {
    case "active":
      return (
        <Badge className="bg-brand/10 text-brand hover:bg-brand/20">À jour</Badge>
      );
    case "trial":
      return (
        <Badge className="bg-blue-500/10 text-blue-600 hover:bg-blue-500/20 dark:text-blue-400">
          Essai
        </Badge>
      );
    case "late":
      return (
        <Badge className="bg-amber-accent/15 text-amber-accent hover:bg-amber-accent/25">
          <AlertTriangle className="size-3" /> En retard
        </Badge>
      );
    case "grace_period":
      return (
        <Badge className="bg-amber-accent/15 text-amber-accent hover:bg-amber-accent/25">
          En grâce
        </Badge>
      );
    case "suspended":
      return <Badge variant="destructive">Suspendu</Badge>;
    case "terminated":
      return <Badge variant="outline">Résilié</Badge>;
    default:
      return <Badge variant="outline">{sub.status}</Badge>;
  }
}

export function AdminShopsView() {
  const [loading, setLoading] = React.useState(true);
  const [shops, setShops] = React.useState<Shop[]>([]);
  const [subs, setSubs] = React.useState<AdminSubscriptionItem[]>([]);
  const [query, setQuery] = React.useState("");
  const [status, setStatus] = React.useState<string>("all");

  // Dialogs d'action
  const [suspendTarget, setSuspendTarget] = React.useState<Shop | null>(null);
  const [suspendReason, setSuspendReason] = React.useState("");
  const [reactivateTarget, setReactivateTarget] = React.useState<Shop | null>(null);
  const [detailShop, setDetailShop] = React.useState<Shop | null>(null);
  const [acting, setActing] = React.useState(false);

  const load = React.useCallback(async () => {
    setLoading(true);
    try {
      const [shopsResp, subsResp] = await Promise.all([
        adminApi.listShops({ limit: 500 }),
        adminApi.listSubscriptions({ limit: 500 }),
      ]);
      setShops(shopsResp?.shops ?? []);
      setSubs(subsResp?.items ?? []);
    } catch (err) {
      const msg =
        err instanceof ApiError
          ? err.message
          : "Impossible de charger la liste des boutiques.";
      toast.error("Erreur de chargement", { description: msg });
    } finally {
      setLoading(false);
    }
  }, []);

  React.useEffect(() => {
    load();
  }, [load]);

  // Index des abonnements par shop_id
  const subByShopId = React.useMemo(() => {
    const map = new Map<string, AdminSubscriptionItem>();
    for (const s of subs) {
      if (s.shop_id) map.set(s.shop_id, s);
    }
    return map;
  }, [subs]);

  const filtered = React.useMemo(() => {
    return shops.filter((s) => {
      const sub = subByShopId.get(s.id);
      const combined = combinedStatus(s, sub);
      if (status !== "all") {
        // Filtre par statut combiné (label normalisé)
        const statusMap: Record<string, string> = {
          active: "Active",
          suspended: "Suspendue",
          pending: "En attente",
          grace: "En grâce",
          terminated: "Résiliée",
          trial: "Essai",
          late: "En retard",
        };
        if (combined.label !== statusMap[status]) return false;
      }
      if (query) {
        const q = query.toLowerCase();
        const name = (s.name || "").toLowerCase();
        const slug = (s.slug || "").toLowerCase();
        const owner = (s.ownerName || "").toLowerCase();
        const email = (s.owner_email || "").toLowerCase();
        if (!name.includes(q) && !slug.includes(q) && !owner.includes(q) && !email.includes(q))
          return false;
      }
      return true;
    });
  }, [shops, subByShopId, query, status]);

  const counts = React.useMemo(() => {
    const c = { active: 0, suspended: 0, pending: 0, terminated: 0, other: 0 };
    for (const s of shops) {
      const sub = subByShopId.get(s.id);
      const combined = combinedStatus(s, sub);
      if (combined.label === "Active") c.active++;
      else if (combined.label === "Suspendue") c.suspended++;
      else if (combined.label === "En attente" || combined.label === "Essai")
        c.pending++;
      else if (combined.label === "Résiliée") c.terminated++;
      else c.other++;
    }
    return c;
  }, [shops, subByShopId]);

  // ====== Actions ======

  async function handleSuspend() {
    if (!suspendTarget) return;
    if (!suspendReason.trim()) {
      toast.error("Motif requis", {
        description: "Veuillez indiquer un motif de suspension.",
      });
      return;
    }
    setActing(true);
    try {
      await adminApi.suspendShop(suspendTarget.id, suspendReason.trim());
      toast.success("Boutique suspendue", {
        description: `${suspendTarget.name} est maintenant suspendue.`,
      });
      setSuspendTarget(null);
      setSuspendReason("");
      await load();
    } catch (err) {
      const msg =
        err instanceof ApiError ? err.message : "Échec de la suspension.";
      toast.error("Échec suspension", { description: msg });
    } finally {
      setActing(false);
    }
  }

  async function handleReactivate() {
    if (!reactivateTarget) return;
    setActing(true);
    try {
      await adminApi.reactivateShop(reactivateTarget.id);
      toast.success("Boutique réactivée", {
        description: `${reactivateTarget.name} est de nouveau active.`,
      });
      setReactivateTarget(null);
      await load();
    } catch (err) {
      const msg =
        err instanceof ApiError ? err.message : "Échec de la réactivation.";
      toast.error("Échec réactivation", { description: msg });
    } finally {
      setActing(false);
    }
  }

  function handleTerminate(shop: Shop) {
    toast.info("À implémenter", {
      description: `La résiliation de ${shop.name} nécessite l'endpoint POST /api/shops/{id}/terminate (non encore exposé).`,
    });
  }

  return (
    <div className="space-y-5">
      <ViewHeader
        title="Administration — Boutiques"
        description="Toutes les boutiques de la plateforme NOVA (actives, suspendues, en brouillon, résiliées)."
        actions={
          <Button variant="outline" size="sm" onClick={load} disabled={loading}>
            {loading ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <RefreshCw className="size-4" />
            )}
            Actualiser
          </Button>
        }
      />

      {/* Mini-KPIs par statut */}
      <div className="grid grid-cols-2 gap-3 sm:gap-4 lg:grid-cols-4">
        <Card>
          <CardContent className="flex items-center gap-3 py-4">
            <span className="flex size-9 items-center justify-center rounded-lg bg-brand/10 text-brand">
              <Store className="size-5" />
            </span>
            <div>
              <div className="text-xs text-muted-foreground">Total</div>
              <div className="text-xl font-bold tabular-nums">{shops.length}</div>
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="flex items-center gap-3 py-4">
            <span className="flex size-9 items-center justify-center rounded-lg bg-brand/10 text-brand">
              <CheckCircle2 className="size-5" />
            </span>
            <div>
              <div className="text-xs text-muted-foreground">Actives</div>
              <div className="text-xl font-bold tabular-nums">{counts.active}</div>
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="flex items-center gap-3 py-4">
            <span className="flex size-9 items-center justify-center rounded-lg bg-amber-accent/15 text-amber-accent">
              <AlertTriangle className="size-5" />
            </span>
            <div>
              <div className="text-xs text-muted-foreground">En attente / essai</div>
              <div className="text-xl font-bold tabular-nums">{counts.pending}</div>
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="flex items-center gap-3 py-4">
            <span className="flex size-9 items-center justify-center rounded-lg bg-rose-500/10 text-rose-600 dark:text-rose-400">
              <PauseCircle className="size-5" />
            </span>
            <div>
              <div className="text-xs text-muted-foreground">Suspendues</div>
              <div className="text-xl font-bold tabular-nums">{counts.suspended}</div>
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Filtres + table */}
      <Card>
        <CardHeader className="border-b pb-3">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <CardTitle className="text-sm font-semibold">
              Boutiques ({filtered.length})
            </CardTitle>
            <div className="flex flex-wrap items-center gap-2">
              <div className="relative">
                <Search className="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                <Input
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  placeholder="Rechercher (nom, slug, propriétaire)…"
                  className="h-9 w-full pl-8 sm:w-[280px]"
                  aria-label="Rechercher une boutique"
                />
              </div>
              <Select value={status} onValueChange={setStatus}>
                <SelectTrigger className="h-9 w-[180px]" aria-label="Filtrer par statut">
                  <SelectValue placeholder="Tous les statuts" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">Tous les statuts</SelectItem>
                  <SelectItem value="active">Active</SelectItem>
                  <SelectItem value="pending">En attente</SelectItem>
                  <SelectItem value="trial">Essai</SelectItem>
                  <SelectItem value="late">En retard</SelectItem>
                  <SelectItem value="grace">En grâce</SelectItem>
                  <SelectItem value="suspended">Suspendue</SelectItem>
                  <SelectItem value="terminated">Résiliée</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>
        </CardHeader>
        <CardContent className="py-2">
          {loading ? (
            <div className="space-y-2 py-3">
              {Array.from({ length: 6 }).map((_, i) => (
                <Skeleton key={i} className="h-10 w-full" />
              ))}
            </div>
          ) : filtered.length === 0 ? (
            <div className="flex flex-col items-center justify-center gap-3 py-12 text-center">
              <Store className="size-10 text-muted-foreground/40" />
              <div className="text-base font-semibold">Aucune boutique trouvée</div>
              <p className="max-w-md text-sm text-muted-foreground">
                {shops.length === 0
                  ? "Aucune boutique n'a encore été créée sur la plateforme."
                  : "Aucune boutique ne correspond à votre recherche."}
              </p>
            </div>
          ) : (
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Boutique</TableHead>
                    <TableHead className="hidden md:table-cell">Propriétaire</TableHead>
                    <TableHead className="hidden lg:table-cell">Contacts</TableHead>
                    <TableHead className="hidden sm:table-cell">Statut</TableHead>
                    <TableHead>Formule</TableHead>
                    <TableHead className="hidden lg:table-cell">Paiement</TableHead>
                    <TableHead className="hidden md:table-cell text-right">Créée le</TableHead>
                    <TableHead className="w-[60px] text-right">Actions</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {filtered.map((s) => {
                    const sub = subByShopId.get(s.id);
                    return (
                      <TableRow
                        key={s.id}
                        className="cursor-pointer hover:bg-muted/40"
                        onClick={() => setDetailShop(s)}
                      >
                        <TableCell>
                          <div className="font-medium">{s.name || "—"}</div>
                          <div className="text-[11px] text-muted-foreground">
                            <a
                              href={`https://${s.slug}.nova.ci`}
                              target="_blank"
                              rel="noopener noreferrer"
                              className="inline-flex items-center gap-1 text-brand hover:underline"
                              onClick={(e) => e.stopPropagation()}
                            >
                              {s.slug || "—"}
                              <ExternalLink className="size-3" />
                            </a>
                            {s.commune && <span className="ml-2">· {s.commune}</span>}
                          </div>
                        </TableCell>
                        <TableCell className="hidden md:table-cell">
                          {s.ownerName ? (
                            <div>
                              <div className="text-sm font-medium">{s.ownerName}</div>
                              {s.owner_email && (
                                <div className="text-[11px] text-muted-foreground">
                                  {s.owner_email}
                                </div>
                              )}
                            </div>
                          ) : (
                            <span className="text-xs text-muted-foreground">—</span>
                          )}
                        </TableCell>
                        <TableCell className="hidden lg:table-cell">
                          {s.phone || s.whatsapp_number ? (
                            <div className="text-xs text-muted-foreground">
                              {s.phone && <div>📞 {s.phone}</div>}
                              {s.whatsapp_number && <div>💬 {s.whatsapp_number}</div>}
                            </div>
                          ) : (
                            <span className="text-xs text-muted-foreground">—</span>
                          )}
                        </TableCell>
                        <TableCell className="hidden sm:table-cell">
                          <StatusBadge shop={s} sub={sub} />
                        </TableCell>
                        <TableCell>
                          {sub?.plan_name ? (
                            <span className="text-sm">{sub.plan_name}</span>
                          ) : (
                            <span className="text-xs text-muted-foreground">—</span>
                          )}
                        </TableCell>
                        <TableCell className="hidden lg:table-cell">
                          {paymentStateBadge(sub)}
                        </TableCell>
                        <TableCell className="hidden md:table-cell text-right tabular-nums text-muted-foreground">
                          {s.createdAt ? formatDate(s.createdAt) : "—"}
                        </TableCell>
                        <TableCell className="text-right" onClick={(e) => e.stopPropagation()}>
                          <DropdownMenu>
                            <DropdownMenuTrigger asChild>
                              <Button
                                variant="ghost"
                                size="icon"
                                className="size-8"
                                aria-label="Actions boutique"
                              >
                                <MoreHorizontal className="size-4" />
                              </Button>
                            </DropdownMenuTrigger>
                            <DropdownMenuContent align="end">
                              <DropdownMenuLabel>Actions</DropdownMenuLabel>
                              <DropdownMenuItem onClick={() => setDetailShop(s)}>
                                <Eye className="size-4" /> Voir détails
                              </DropdownMenuItem>
                              <DropdownMenuSeparator />
                              {s.status === "suspended" ? (
                                <DropdownMenuItem
                                  onClick={() => setReactivateTarget(s)}
                                  className="text-brand focus:text-brand"
                                >
                                  <PlayCircle className="size-4" /> Réactiver
                                </DropdownMenuItem>
                              ) : (
                                <DropdownMenuItem
                                  onClick={() => {
                                    setSuspendTarget(s);
                                    setSuspendReason("");
                                  }}
                                  className="text-rose-600 focus:text-rose-600 dark:text-rose-400"
                                >
                                  <PauseCircle className="size-4" /> Suspendre…
                                </DropdownMenuItem>
                              )}
                              <DropdownMenuItem
                                onClick={() => handleTerminate(s)}
                                className="text-muted-foreground"
                              >
                                <XOctagon className="size-4" /> Résilier…
                              </DropdownMenuItem>
                            </DropdownMenuContent>
                          </DropdownMenu>
                        </TableCell>
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
            </div>
          )}
        </CardContent>
      </Card>

      {/* Dialog : Suspendre */}
      <Dialog
        open={!!suspendTarget}
        onOpenChange={(open) => {
          if (!open) {
            setSuspendTarget(null);
            setSuspendReason("");
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Suspendre la boutique</DialogTitle>
            <DialogDescription>
              La boutique{" "}
              <strong className="text-foreground">{suspendTarget?.name}</strong> sera
              suspendue immédiatement. L&apos;accès commerçant sera bloqué et les
              conversations WhatsApp mises en pause. Un motif est obligatoire
              (tracé dans le journal d&apos;audit).
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-2 py-2">
            <Label htmlFor="suspend-reason">Motif de suspension</Label>
            <Textarea
              id="suspend-reason"
              value={suspendReason}
              onChange={(e) => setSuspendReason(e.target.value)}
              placeholder="Ex : paiement en retard malgré 3 relances, fraude suspectée…"
              rows={4}
              maxLength={500}
            />
            <div className="text-right text-[11px] text-muted-foreground">
              {suspendReason.length}/500
            </div>
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => {
                setSuspendTarget(null);
                setSuspendReason("");
              }}
              disabled={acting}
            >
              Annuler
            </Button>
            <Button
              variant="destructive"
              onClick={handleSuspend}
              disabled={acting || !suspendReason.trim()}
            >
              {acting ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <PauseCircle className="size-4" />
              )}
              Confirmer la suspension
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Dialog : Réactiver */}
      <Dialog
        open={!!reactivateTarget}
        onOpenChange={(open) => {
          if (!open) setReactivateTarget(null);
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Réactiver la boutique</DialogTitle>
            <DialogDescription>
              La boutique{" "}
              <strong className="text-foreground">{reactivateTarget?.name}</strong>{" "}
              sera réactivée. L&apos;accès commerçant sera restauré et les
              conversations WhatsApp reprendront.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setReactivateTarget(null)}
              disabled={acting}
            >
              Annuler
            </Button>
            <Button onClick={handleReactivate} disabled={acting}>
              {acting ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <PlayCircle className="size-4" />
              )}
              Confirmer la réactivation
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Dialog : Détails boutique */}
      <Dialog
        open={!!detailShop}
        onOpenChange={(open) => {
          if (!open) setDetailShop(null);
        }}
      >
        <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
          {detailShop && (
            <ShopDetail
              shop={detailShop}
              sub={subByShopId.get(detailShop.id)}
              onSuspend={() => {
                setSuspendTarget(detailShop);
                setSuspendReason("");
                setDetailShop(null);
              }}
              onReactivate={() => {
                setReactivateTarget(detailShop);
                setDetailShop(null);
              }}
              onTerminate={() => handleTerminate(detailShop)}
            />
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}

// ====== Détail boutique ======

function ShopDetail({
  shop,
  sub,
  onSuspend,
  onReactivate,
  onTerminate,
}: {
  shop: Shop;
  sub?: AdminSubscriptionItem;
  onSuspend: () => void;
  onReactivate: () => void;
  onTerminate: () => void;
}) {
  const isSuspended = shop.status === "suspended";
  return (
    <>
      <DialogHeader>
        <DialogTitle className="flex items-center gap-2">
          {shop.name}
          <StatusBadge shop={shop} sub={sub} />
        </DialogTitle>
        <DialogDescription>
          Détails de la boutique{" "}
          <a
            href={`https://${shop.slug}.nova.ci`}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-1 text-brand hover:underline"
          >
            {shop.slug}.nova.ci <ExternalLink className="size-3" />
          </a>
        </DialogDescription>
      </DialogHeader>

      <div className="space-y-4 py-2">
        {/* Infos générales */}
        <DetailSection icon={Store} title="Informations générales">
          <DetailGrid>
            <DetailItem icon={UserIcon} label="Propriétaire" value={shop.ownerName || NA} />
            <DetailItem icon={Mail} label="E-mail" value={shop.owner_email || NA} />
            <DetailItem icon={Phone} label="Téléphone" value={shop.phone || NA} />
            <DetailItem
              icon={MessageSquare}
              label="WhatsApp"
              value={shop.whatsapp_number || NA}
            />
            <DetailItem
              icon={Store}
              label="Commune"
              value={shop.commune || NA}
            />
            <DetailItem
              icon={History}
              label="Créée le"
              value={shop.createdAt ? formatDateTime(shop.createdAt) : NA}
            />
          </DetailGrid>
        </DetailSection>

        {/* Abonnement */}
        <DetailSection icon={ShieldCheck} title="Abonnement">
          <DetailGrid>
            <DetailItem
              icon={ShieldCheck}
              label="Formule"
              value={sub?.plan_name || NA}
            />
            <DetailItem
              icon={CheckCircle2}
              label="État paiement"
              valueNode={paymentStateBadge(sub)}
            />
            <DetailItem
              icon={History}
              label="Démarré le"
              value={sub?.started_at ? formatDate(sub.started_at) : NA}
            />
            <DetailItem
              icon={AlertTriangle}
              label="Prochaine facturation"
              value={sub?.next_billing_at ? formatDate(sub.next_billing_at) : NA}
            />
            <DetailItem
              icon={AlertOctagon}
              label="Grâce jusqu'au"
              value={sub?.grace_until ? formatDate(sub.grace_until) : NA}
            />
          </DetailGrid>
        </DetailSection>

        {/* État WhatsApp */}
        <DetailSection icon={MessageSquare} title="État WhatsApp">
          <div className="rounded-lg border border-dashed bg-muted/30 p-3 text-xs text-muted-foreground">
            <MessageSquare className="mb-1 size-3.5" />
            État de la connexion WhatsApp Cloud API : {NA}. L&apos;endpoint
            d&apos;inspection par boutique sera exposé prochainement.
          </div>
        </DetailSection>

        {/* Consommation IA */}
        <DetailSection icon={Cpu} title="Consommation IA">
          <div className="rounded-lg border border-dashed bg-muted/30 p-3 text-xs text-muted-foreground">
            <Cpu className="mb-1 size-3.5" />
            Volume IA, tokens et coût estimé : {NA}. L&apos;endpoint
            /api/admin/ai/usage par boutique sera exposé prochainement.
          </div>
        </DetailSection>

        {/* Historique des changements */}
        <DetailSection icon={History} title="Historique des changements">
          <div className="rounded-lg border border-dashed bg-muted/30 p-3 text-xs text-muted-foreground">
            <History className="mb-1 size-3.5" />
            Historique des actions (suspensions, réactivations, changement de
            formule) : {NA}. Sera remonté depuis le journal d&apos;audit
            (/api/admin/audit) dès son déploiement.
          </div>
        </DetailSection>

        {/* Derniers incidents */}
        <DetailSection icon={AlertOctagon} title="Derniers incidents">
          <div className="rounded-lg border border-dashed bg-muted/30 p-3 text-xs text-muted-foreground">
            <AlertOctagon className="mb-1 size-3.5" />
            Aucun incident enregistré — flux non connecté.
          </div>
        </DetailSection>
      </div>

      <Separator />

      <DialogFooter className="gap-2 sm:gap-2">
        <Button variant="outline" onClick={onTerminate} size="sm">
          <XOctagon className="size-4" /> Résilier
        </Button>
        {isSuspended ? (
          <Button onClick={onReactivate} size="sm" className="ml-auto">
            <PlayCircle className="size-4" /> Réactiver
          </Button>
        ) : (
          <Button
            onClick={onSuspend}
            size="sm"
            variant="destructive"
            className="ml-auto"
          >
            <PauseCircle className="size-4" /> Suspendre
          </Button>
        )}
      </DialogFooter>
    </>
  );
}

function DetailSection({
  icon: Icon,
  title,
  children,
}: {
  icon: React.ComponentType<{ className?: string }>;
  title: string;
  children: React.ReactNode;
}) {
  return (
    <div>
      <div className="mb-2 flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
        <Icon className="size-3.5" /> {title}
      </div>
      {children}
    </div>
  );
}

function DetailGrid({ children }: { children: React.ReactNode }) {
  return <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">{children}</div>;
}

function DetailItem({
  icon: Icon,
  label,
  value,
  valueNode,
}: {
  icon: React.ComponentType<{ className?: string }>;
  label: string;
  value?: string;
  valueNode?: React.ReactNode;
}) {
  return (
    <div className="rounded-lg border bg-card p-2.5">
      <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
        <Icon className="size-3" /> {label}
      </div>
      <div className="mt-0.5 text-sm font-medium">
        {valueNode ?? (value === NA ? <span className="text-muted-foreground">{NA}</span> : value)}
      </div>
    </div>
  );
}
