"use client";

import * as React from "react";
import {
  Search,
  Eye,
  XCircle,
  CheckCircle2,
  Clock,
  ShoppingCart,
  Loader2,
  Check,
  Ban,
  ChefHat,
  AlarmClock,
} from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { Separator } from "@/components/ui/separator";
import { cn } from "@/lib/utils";
import {
  orders as allOrders,
  orderStatusLabel,
  type Order,
  type OrderStatus,
} from "@/lib/mock-data";
import { ordersV3Api } from "@/lib/api";
import { formatFCFA, formatDateTime } from "@/lib/format";
import { ViewHeader } from "./view-header";

// ====== V3 — onglets & badges ======

type V3Tab =
  | "all"
  | "en_attente_confirmation"
  | "en_attente_paiement"
  | "paiement_signalé"
  | "en_cours"
  | "prete"
  | "terminee"
  | "refusee"
  | "annulee";

const TABS: { key: V3Tab; label: string }[] = [
  { key: "all", label: "Toutes" },
  { key: "en_attente_confirmation", label: "En attente" },
  { key: "en_attente_paiement", label: "Attente paiement" },
  { key: "paiement_signalé", label: "Paiement signalé" },
  { key: "en_cours", label: "En cours" },
  { key: "prete", label: "Prête" },
  { key: "terminee", label: "Terminée" },
  { key: "refusee", label: "Refusée" },
  { key: "annulee", label: "Annulée" },
];

const STATUS_BADGE: Record<OrderStatus, { label: string; className: string }> = {
  pending: {
    label: orderStatusLabel.pending,
    className: "bg-amber-accent/15 text-amber-accent hover:bg-amber-accent/25",
  },
  confirmed: {
    label: orderStatusLabel.confirmed,
    className: "bg-brand/10 text-brand hover:bg-brand/20",
  },
  delivering: {
    label: orderStatusLabel.delivering,
    className: "bg-blue-500/10 text-blue-600 dark:text-blue-400 hover:bg-blue-500/20",
  },
  delivered: {
    label: orderStatusLabel.delivered,
    className: "bg-emerald-500/10 text-emerald-700 dark:text-emerald-400 hover:bg-emerald-500/20",
  },
  cancelled: {
    label: orderStatusLabel.cancelled,
    className: "bg-destructive/10 text-destructive hover:bg-destructive/20",
  },
  en_attente_confirmation: {
    label: orderStatusLabel.en_attente_confirmation,
    className: "bg-amber-accent/15 text-amber-accent hover:bg-amber-accent/25",
  },
  en_attente_paiement: {
    label: orderStatusLabel.en_attente_paiement,
    className: "bg-blue-500/10 text-blue-600 dark:text-blue-400 hover:bg-blue-500/20",
  },
  "paiement_signalé": {
    label: orderStatusLabel["paiement_signalé"],
    className: "bg-purple-500/10 text-purple-600 dark:text-purple-400 hover:bg-purple-500/20",
  },
  en_cours: {
    label: orderStatusLabel.en_cours,
    className: "bg-emerald-500/10 text-emerald-700 dark:text-emerald-400 hover:bg-emerald-500/20",
  },
  prete: {
    label: orderStatusLabel.prete,
    className: "bg-cyan-500/10 text-cyan-700 dark:text-cyan-400 hover:bg-cyan-500/20",
  },
  terminee: {
    label: orderStatusLabel.terminee,
    className: "bg-muted text-muted-foreground hover:bg-muted/80",
  },
  refusee: {
    label: orderStatusLabel.refusee,
    className: "bg-destructive/10 text-destructive hover:bg-destructive/20",
  },
  annulee: {
    label: orderStatusLabel.annulee,
    className: "bg-rose-900/10 text-rose-800 dark:text-rose-300 hover:bg-rose-900/20",
  },
};

function statusBadge(status: OrderStatus) {
  const c = STATUS_BADGE[status] ?? {
    label: status,
    className: "bg-muted text-muted-foreground",
  };
  return <Badge className={c.className}>{c.label}</Badge>;
}

function paymentBadge(p: Order["payment"]) {
  if (p === "paid")
    return <Badge variant="secondary">Payé</Badge>;
  if (p === "unpaid")
    return (
      <Badge className="bg-amber-accent/15 text-amber-accent hover:bg-amber-accent/25">
        Impayé
      </Badge>
    );
  return <Badge variant="outline">Remboursé</Badge>;
}

// ====== V3 — actions selon le statut ======

interface V3Action {
  key: string;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
  variant?: "default" | "outline" | "destructive" | "secondary";
  run: (orderId: string) => Promise<void>;
}

function actionsForStatus(status: OrderStatus): V3Action[] {
  switch (status) {
    case "en_attente_confirmation":
      return [
        {
          key: "merchant-confirm",
          label: "Confirmer",
          icon: Check,
          run: async (id) => {
            await ordersV3Api.merchantConfirm(id);
          },
        },
        {
          key: "cancel",
          label: "Refuser",
          icon: XCircle,
          variant: "destructive",
          run: async (id) => {
            await ordersV3Api.cancel(id);
          },
        },
      ];
    case "en_attente_paiement":
      return [
        {
          key: "cancel",
          label: "Annuler",
          icon: Ban,
          variant: "outline",
          run: async (id) => {
            await ordersV3Api.cancel(id);
          },
        },
      ];
    case "paiement_signalé":
      return [
        {
          key: "confirm-payment",
          label: "Confirmer paiement",
          icon: CheckCircle2,
          run: async (id) => {
            await ordersV3Api.confirmPayment(id);
          },
        },
        {
          key: "refuse-payment",
          label: "Refuser paiement",
          icon: XCircle,
          variant: "destructive",
          run: async (id) => {
            await ordersV3Api.refusePayment(id);
          },
        },
      ];
    case "en_cours":
      return [
        {
          key: "ready",
          label: "Marquer prête",
          icon: ChefHat,
          run: async (id) => {
            await ordersV3Api.markReady(id);
          },
        },
        {
          key: "cancel",
          label: "Annuler",
          icon: Ban,
          variant: "outline",
          run: async (id) => {
            await ordersV3Api.cancel(id);
          },
        },
      ];
    case "prete":
      return [
        {
          key: "complete",
          label: "Terminer",
          icon: CheckCircle2,
          run: async (id) => {
            await ordersV3Api.complete(id);
          },
        },
      ];
    default:
      return [];
  }
}

function statusIcon(status: OrderStatus) {
  switch (status) {
    case "en_attente_confirmation":
    case "pending":
      return <Clock className="size-4 text-amber-accent" />;
    case "en_attente_paiement":
      return <Clock className="size-4 text-blue-600" />;
    case "paiement_signalé":
      return <AlarmClock className="size-4 text-purple-600" />;
    case "en_cours":
    case "confirmed":
      return <CheckCircle2 className="size-4 text-brand" />;
    case "prete":
    case "delivering":
      return <ChefHat className="size-4 text-cyan-700" />;
    case "terminee":
    case "delivered":
      return <CheckCircle2 className="size-4 text-emerald-600" />;
    case "refusee":
    case "cancelled":
      return <XCircle className="size-4 text-destructive" />;
    case "annulee":
      return <Ban className="size-4 text-rose-800" />;
    default:
      return null;
  }
}

export function OrdersView() {
  const [tab, setTab] = React.useState<string>("all");
  const [query, setQuery] = React.useState("");
  const [selected, setSelected] = React.useState<Order | null>(null);
  const [orders, setOrders] = React.useState<Order[]>(allOrders);
  const [pendingId, setPendingId] = React.useState<string | null>(null);

  const filtered = React.useMemo(() => {
    return orders.filter((o) => {
      if (tab !== "all" && o.status !== tab) return false;
      if (query) {
        const q = query.toLowerCase();
        if (
          !o.number.toLowerCase().includes(q) &&
          !o.customerName.toLowerCase().includes(q)
        )
          return false;
      }
      return true;
    });
  }, [orders, tab, query]);

  async function runAction(order: Order, action: V3Action) {
    setPendingId(order.id);
    try {
      await action.run(order.id);
      // Mise à jour optimiste du statut local (mapping approximatif).
      const nextStatus = nextStatusAfterAction(action.key);
      if (nextStatus) {
        setOrders((prev) =>
          prev.map((o) =>
            o.id === order.id ? { ...o, status: nextStatus } : o,
          ),
        );
        setSelected((prev) =>
          prev && prev.id === order.id
            ? { ...prev, status: nextStatus }
            : prev,
        );
      }
      toast.success(`${order.number} — ${action.label}`, {
        description: nextStatus
          ? `Nouveau statut : ${orderStatusLabel[nextStatus]}`
          : undefined,
      });
    } catch (err) {
      const message =
        err instanceof Error ? err.message : "Une erreur est survenue.";
      toast.error(`Échec — ${action.label}`, { description: message });
    } finally {
      setPendingId(null);
    }
  }

  return (
    <div className="space-y-5">
      <ViewHeader
        title="Commandes"
        description="Suivez le cycle de vie des commandes (confirmation, paiement, préparation, livraison)."
        actions={null}
      />

      <Card className="overflow-hidden">
        <div className="flex flex-col gap-3 border-b p-3 lg:flex-row lg:items-center lg:justify-between">
          <Tabs value={tab} onValueChange={setTab} className="w-full lg:w-auto">
            <TabsList className="flex h-9 w-full flex-wrap justify-start gap-1 lg:w-auto">
              {TABS.map((t) => (
                <TabsTrigger key={t.key} value={t.key} className="text-xs">
                  {t.label}
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
          <div className="relative w-full lg:w-64">
            <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              placeholder="Rechercher N° commande, client…"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              className="pl-9"
              aria-label="Rechercher une commande"
            />
          </div>
        </div>

        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>N° commande</TableHead>
              <TableHead>Client</TableHead>
              <TableHead className="hidden text-right lg:table-cell">Total</TableHead>
              <TableHead className="hidden md:table-cell">Paiement</TableHead>
              <TableHead>Statut</TableHead>
              <TableHead className="hidden text-right sm:table-cell">Date</TableHead>
              <TableHead className="w-[100px]"></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {filtered.length === 0 ? (
              <TableRow>
                <TableCell
                  colSpan={7}
                  className="py-8 text-center text-sm text-muted-foreground"
                >
                  Aucune commande.
                </TableCell>
              </TableRow>
            ) : (
              filtered.map((o) => (
                <TableRow
                  key={o.id}
                  className="cursor-pointer"
                  onClick={() => setSelected(o)}
                >
                  <TableCell className="font-medium">{o.number}</TableCell>
                  <TableCell>
                    <div className="font-medium">{o.customerName}</div>
                    <div className="text-xs text-muted-foreground">
                      {o.customerZone}
                    </div>
                  </TableCell>
                  <TableCell className="hidden text-right font-semibold tabular-nums lg:table-cell">
                    {formatFCFA(o.total)}
                  </TableCell>
                  <TableCell className="hidden md:table-cell">
                    {paymentBadge(o.payment)}
                  </TableCell>
                  <TableCell>{statusBadge(o.status)}</TableCell>
                  <TableCell className="hidden text-right text-xs text-muted-foreground sm:table-cell">
                    {formatDateTime(o.createdAt)}
                  </TableCell>
                  <TableCell onClick={(e) => e.stopPropagation()}>
                    <Button
                      size="sm"
                      variant="outline"
                      className="h-7 w-7 p-0"
                      aria-label="Voir détail"
                      onClick={() => setSelected(o)}
                    >
                      <Eye className="size-3.5" />
                    </Button>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </Card>

      <OrderDetailDialog
        order={selected}
        onClose={() => setSelected(null)}
        pendingId={pendingId}
        onAction={runAction}
      />
    </div>
  );
}

function nextStatusAfterAction(actionKey: string): OrderStatus | null {
  switch (actionKey) {
    case "merchant-confirm":
      return "en_attente_paiement";
    case "confirm-payment":
      return "en_cours";
    case "refuse-payment":
      return "en_attente_paiement";
    case "ready":
      return "prete";
    case "complete":
      return "terminee";
    case "cancel":
      return "annulee";
    default:
      return null;
  }
}

function OrderDetailDialog({
  order,
  onClose,
  onAction,
  pendingId,
}: {
  order: Order | null;
  onClose: () => void;
  onAction: (order: Order, action: V3Action) => void;
  pendingId: string | null;
}) {
  const actions = order ? actionsForStatus(order.status) : [];
  const isPending = order ? pendingId === order.id : false;

  return (
    <Dialog open={!!order} onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="sm:max-w-lg">
        {order && (
          <>
            <DialogHeader>
              <DialogTitle className="flex items-center gap-2">
                <ShoppingCart className="size-5 text-brand" />
                {order.number}
              </DialogTitle>
              <DialogDescription>
                Passée le {formatDateTime(order.createdAt)} · {order.customerZone}
              </DialogDescription>
            </DialogHeader>

            <div className="space-y-3">
              <div className="rounded-lg border bg-muted/30 p-3">
                <div className="text-xs font-semibold text-muted-foreground">
                  Client
                </div>
                <div className="mt-0.5 font-medium">{order.customerName}</div>
                <div className="text-sm text-muted-foreground">
                  {order.customerPhone}
                </div>
                <Separator className="my-2" />
                <div className="text-xs font-semibold text-muted-foreground">
                  Adresse de livraison
                </div>
                <div className="text-sm">{order.deliveryAddress}</div>
              </div>

              <div>
                <div className="mb-1.5 text-xs font-semibold text-muted-foreground">
                  Articles
                </div>
                <div className="space-y-2">
                  {order.items.map((it, i) => (
                    <div
                      key={i}
                      className="flex items-center justify-between gap-3 rounded-md border p-2 text-sm"
                    >
                      <div className="min-w-0">
                        <div className="truncate font-medium">
                          {it.productName}
                        </div>
                        <div className="text-xs text-muted-foreground">
                          {it.variantLabel} × {it.quantity}
                        </div>
                      </div>
                      <div className="font-semibold tabular-nums">
                        {formatFCFA(it.unitPrice * it.quantity)}
                      </div>
                    </div>
                  ))}
                </div>
              </div>

              <div className="space-y-1 rounded-lg border p-3 text-sm">
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Sous-total</span>
                  <span className="tabular-nums">{formatFCFA(order.subtotal)}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Livraison</span>
                  <span className="tabular-nums">{formatFCFA(order.deliveryFee)}</span>
                </div>
                <Separator className="my-1" />
                <div className="flex justify-between font-semibold">
                  <span>Total</span>
                  <span className="tabular-nums">{formatFCFA(order.total)}</span>
                </div>
              </div>

              <div className="flex items-center justify-between rounded-lg border p-3 text-sm">
                <div className="flex items-center gap-2">
                  {statusIcon(order.status)}
                  <span>
                    Statut : <strong>{orderStatusLabel[order.status]}</strong>
                  </span>
                </div>
                {paymentBadge(order.payment)}
              </div>
            </div>

            <DialogFooter className="flex-col gap-2 sm:flex-row sm:justify-between">
              <Button
                variant="outline"
                size="sm"
                className="text-destructive"
                disabled={isPending}
                onClick={() => {
                  onClose();
                }}
              >
                <XCircle className="size-4" /> Fermer
              </Button>
              <div className="flex flex-wrap gap-2">
                {actions.map((a) => (
                  <Button
                    key={a.key}
                    size="sm"
                    variant={a.variant ?? "default"}
                    disabled={isPending}
                    onClick={() => onAction(order, a)}
                  >
                    {isPending ? (
                      <Loader2 className="size-4 animate-spin" />
                    ) : (
                      <a.icon className="size-4" />
                    )}
                    {a.label}
                  </Button>
                ))}
                {actions.length === 0 && (
                  <span className="text-xs text-muted-foreground">
                    Aucune action disponible pour ce statut.
                  </span>
                )}
              </div>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
