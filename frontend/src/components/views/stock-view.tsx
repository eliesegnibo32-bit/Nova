"use client";

import * as React from "react";
import {
  ArrowLeftRight,
  SlidersHorizontal,
  Package,
  Boxes,
  AlertTriangle,
  XCircle,
  CheckCircle2,
  Loader2,
  History,
} from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { stockRows as mockStockRows, stockStatus, type StockRow } from "@/lib/mock-data";
import {
  stockApi,
  ApiError,
  type InventoryItem,
  type StockMovement,
} from "@/lib/api";
import { formatDateTime } from "@/lib/format";
import { ViewHeader } from "./view-header";

// ====== Helpers ======

/** Map une ligne d'inventaire API vers le type d'affichage local (StockRow). */
function inventoryToRow(it: InventoryItem): StockRow {
  const name = it.product_name ?? "Produit";
  return {
    id: it.variant_id ?? it.id,
    productId: it.product_id ?? "",
    productName: name,
    variantLabel: it.variant_label ?? "—",
    onHand: it.on_hand ?? 0,
    reserved: it.reserved ?? 0,
    available: it.available ?? Math.max(0, (it.on_hand ?? 0) - (it.reserved ?? 0)),
    threshold: it.threshold ?? 5,
    thumbnailColor: "bg-zinc-900",
  };
}

function StatusBadge({ s }: { s: StockRow }) {
  const status = stockStatus(s);
  if (status === "out") return <Badge variant="destructive">Rupture</Badge>;
  if (status === "low")
    return (
      <Badge className="bg-amber-accent/15 text-amber-accent hover:bg-amber-accent/25">
        Faible
      </Badge>
    );
  return (
    <Badge className="bg-brand/10 text-brand hover:bg-brand/20">OK</Badge>
  );
}

function SummaryCards({ rows }: { rows: StockRow[] }) {
  const total = rows.length;
  const inStock = rows.filter((s) => stockStatus(s) === "ok").length;
  const out = rows.filter((s) => stockStatus(s) === "out").length;
  const low = rows.filter((s) => stockStatus(s) === "low").length;

  const items = [
    {
      label: "Total références",
      value: total,
      icon: Package,
      tone: "text-foreground bg-muted",
    },
    {
      label: "En stock",
      value: inStock,
      icon: CheckCircle2,
      tone: "text-brand bg-brand/10",
    },
    {
      label: "Sous le seuil",
      value: low,
      icon: AlertTriangle,
      tone: "text-amber-accent bg-amber-accent/15",
    },
    {
      label: "En rupture",
      value: out,
      icon: XCircle,
      tone: "text-destructive bg-destructive/10",
    },
  ];

  return (
    <div className="grid grid-cols-2 gap-3 sm:gap-4 lg:grid-cols-4">
      {items.map((it) => (
        <Card key={it.label}>
          <CardContent className="flex items-center justify-between gap-3 py-4">
            <div>
              <div className="text-xs text-muted-foreground">{it.label}</div>
              <div className="text-2xl font-bold">{it.value}</div>
            </div>
            <span
              className={cn(
                "flex size-9 items-center justify-center rounded-lg",
                it.tone,
              )}
            >
              <it.icon className="size-5" />
            </span>
          </CardContent>
        </Card>
      ))}
    </div>
  );
}

// ====== Dialog Ajustement (delta signé + raison) ======

function AdjustDialog({
  open,
  onOpenChange,
  row,
  onConfirm,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  row: StockRow | null;
  onConfirm: (delta: number, reason: string) => Promise<void>;
}) {
  const [delta, setDelta] = React.useState("");
  const [reason, setReason] = React.useState("");
  const [saving, setSaving] = React.useState(false);

  React.useEffect(() => {
    if (open) {
      setDelta("");
      setReason("");
    }
  }, [open, row]);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!row) return;
    const d = Number(delta);
    if (isNaN(d) || d === 0) {
      toast.error("Delta invalide", { description: "Entrez un nombre non nul." });
      return;
    }
    if (!reason.trim()) {
      toast.error("La raison est obligatoire");
      return;
    }
    setSaving(true);
    try {
      await onConfirm(d, reason.trim());
      onOpenChange(false);
    } catch {
      // toast déjà émis par le parent
    } finally {
      setSaving(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Ajuster le stock</DialogTitle>
          <DialogDescription>
            Corrigez un écart de stock (casse, perte, erreur d&apos;inventaire…).
            Le delta peut être positif ou négatif.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-1.5">
            <Label>Variante</Label>
            <div className="rounded-md border bg-muted/30 px-3 py-2 text-sm">
              <div className="font-medium">{row?.productName}</div>
              <div className="text-xs text-muted-foreground">
                {row?.variantLabel} · stock actuel : {row?.onHand}
              </div>
            </div>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="adj-delta">Delta *</Label>
              <Input
                id="adj-delta"
                type="number"
                value={delta}
                onChange={(e) => setDelta(e.target.value)}
                placeholder="ex : -2 ou +5"
                required
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="adj-reason">Raison *</Label>
              <Input
                id="adj-reason"
                value={reason}
                onChange={(e) => setReason(e.target.value)}
                placeholder="Casse, perte, inventaire…"
                required
              />
            </div>
          </div>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => onOpenChange(false)}
              disabled={saving}
            >
              Annuler
            </Button>
            <Button type="submit" disabled={saving}>
              {saving && <Loader2 className="size-4 animate-spin" />}
              Appliquer
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// ====== Dialog Réappro (quantité positive + raison) ======

function ReceiveDialog({
  open,
  onOpenChange,
  row,
  onConfirm,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  row: StockRow | null;
  onConfirm: (qty: number, reason: string) => Promise<void>;
}) {
  const [qty, setQty] = React.useState("");
  const [reason, setReason] = React.useState("Réapprovisionnement");
  const [saving, setSaving] = React.useState(false);

  React.useEffect(() => {
    if (open) {
      setQty("");
      setReason("Réapprovisionnement");
    }
  }, [open, row]);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!row) return;
    const q = Number(qty);
    if (isNaN(q) || q <= 0) {
      toast.error("Quantité invalide", { description: "Entrez un nombre positif." });
      return;
    }
    if (!reason.trim()) {
      toast.error("La raison est obligatoire");
      return;
    }
    setSaving(true);
    try {
      await onConfirm(q, reason.trim());
      onOpenChange(false);
    } catch {
      // toast déjà émis par le parent
    } finally {
      setSaving(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Réapprovisionner</DialogTitle>
          <DialogDescription>
            Enregistrez une entrée de stock (livraison fournisseur, retour client…).
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-1.5">
            <Label>Variante</Label>
            <div className="rounded-md border bg-muted/30 px-3 py-2 text-sm">
              <div className="font-medium">{row?.productName}</div>
              <div className="text-xs text-muted-foreground">
                {row?.variantLabel} · stock actuel : {row?.onHand}
              </div>
            </div>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="rec-qty">Quantité *</Label>
              <Input
                id="rec-qty"
                type="number"
                min="1"
                value={qty}
                onChange={(e) => setQty(e.target.value)}
                placeholder="ex : 20"
                required
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="rec-reason">Raison</Label>
              <Input
                id="rec-reason"
                value={reason}
                onChange={(e) => setReason(e.target.value)}
                placeholder="Réapprovisionnement"
              />
            </div>
          </div>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => onOpenChange(false)}
              disabled={saving}
            >
              Annuler
            </Button>
            <Button type="submit" disabled={saving}>
              {saving && <Loader2 className="size-4 animate-spin" />}
              Réapprovisionner
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// ====== Dialog Mouvements (historique) ======

function MovementsDialog({
  open,
  onOpenChange,
  rows,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  rows: StockRow[];
}) {
  const [variantId, setVariantId] = React.useState<string>("all");
  const [movements, setMovements] = React.useState<StockMovement[]>([]);
  const [loading, setLoading] = React.useState(false);

  const loadMovements = React.useCallback(async () => {
    setLoading(true);
    try {
      const res = await stockApi.movements(
        variantId === "all" ? undefined : variantId,
      );
      setMovements(res?.movements ?? []);
    } catch {
      setMovements([]);
    } finally {
      setLoading(false);
    }
  }, [variantId]);

  React.useEffect(() => {
    if (open) loadMovements();
  }, [open, loadMovements]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Mouvements de stock</DialogTitle>
          <DialogDescription>
            Historique des entrées, sorties et ajustements par variante.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          <div className="flex items-center gap-2">
            <History className="size-4 text-muted-foreground" />
            <Select value={variantId} onValueChange={setVariantId}>
              <SelectTrigger className="w-full sm:w-72">
                <SelectValue placeholder="Toutes les variantes" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">Toutes les variantes</SelectItem>
                {rows.map((r) => (
                  <SelectItem key={r.id} value={r.id}>
                    {r.productName} — {r.variantLabel}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="max-h-[400px] overflow-auto rounded-md border">
            <Table>
              <TableHeader className="sticky top-0 bg-card">
                <TableRow>
                  <TableHead className="text-xs">Date</TableHead>
                  <TableHead className="text-xs">Type</TableHead>
                  <TableHead className="text-right text-xs">Qté</TableHead>
                  <TableHead className="text-xs">Raison</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {loading ? (
                  Array.from({ length: 4 }).map((_, i) => (
                    <TableRow key={`sk-${i}`}>
                      <TableCell colSpan={4}>
                        <Skeleton className="h-6 w-full" />
                      </TableCell>
                    </TableRow>
                  ))
                ) : movements.length === 0 ? (
                  <TableRow>
                    <TableCell
                      colSpan={4}
                      className="py-6 text-center text-xs text-muted-foreground"
                    >
                      Aucun mouvement enregistré.
                    </TableCell>
                  </TableRow>
                ) : (
                  movements.map((m) => (
                    <TableRow key={m.id}>
                      <TableCell className="text-xs text-muted-foreground">
                        {formatDateTime(m.created_at)}
                      </TableCell>
                      <TableCell>
                        <Badge variant="outline" className="text-[10px]">
                          {m.type}
                        </Badge>
                      </TableCell>
                      <TableCell
                        className={cn(
                          "text-right text-xs font-semibold tabular-nums",
                          m.quantity > 0
                            ? "text-brand"
                            : m.quantity < 0
                              ? "text-destructive"
                              : "",
                        )}
                      >
                        {m.quantity > 0 ? "+" : ""}
                        {m.quantity}
                      </TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        {m.reason ?? "—"}
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>
        </div>
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
          >
            Fermer
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ====== Vue principale ======

export function StockView() {
  const [rows, setRows] = React.useState<StockRow[]>(mockStockRows);
  const [loading, setLoading] = React.useState(true);

  // Dialogs
  const [adjustOpen, setAdjustOpen] = React.useState(false);
  const [adjustRow, setAdjustRow] = React.useState<StockRow | null>(null);

  const [receiveOpen, setReceiveOpen] = React.useState(false);
  const [receiveRow, setReceiveRow] = React.useState<StockRow | null>(null);

  const [movementsOpen, setMovementsOpen] = React.useState(false);

  // Chargement initial — silencieux, fallback mock.
  const load = React.useCallback(async () => {
    setLoading(true);
    try {
      const res = await stockApi.list();
      const items = res?.items;
      if (Array.isArray(items) && items.length > 0) {
        setRows(items.map(inventoryToRow));
      } else {
        setRows(mockStockRows);
      }
    } catch {
      // Pas de toast — on retombe silencieusement sur le mock.
      setRows(mockStockRows);
    } finally {
      setLoading(false);
    }
  }, []);

  React.useEffect(() => {
    load();
  }, [load]);

  // ====== Mutations ======

  async function handleAdjust(delta: number, reason: string) {
    if (!adjustRow) return;
    const row = adjustRow;
    // Optimistic : on applique le delta localement.
    const previous = rows;
    setRows((prev) =>
      prev.map((r) =>
        r.id === row.id
          ? {
              ...r,
              onHand: r.onHand + delta,
              available: Math.max(0, r.available + delta),
            }
          : r,
      ),
    );
    try {
      await stockApi.adjust(row.id, delta, reason);
      toast.success("Stock ajusté", {
        description: `${row.productName} — delta ${delta > 0 ? "+" : ""}${delta}.`,
      });
      // Recharger pour synchroniser avec le backend.
      load();
    } catch (err) {
      setRows(previous);
      const msg = err instanceof ApiError ? err.message : "Échec de l'ajustement";
      toast.error("Ajustement échoué", { description: msg });
    }
  }

  async function handleReceive(qty: number, reason: string) {
    if (!receiveRow) return;
    const row = receiveRow;
    const previous = rows;
    setRows((prev) =>
      prev.map((r) =>
        r.id === row.id
          ? {
              ...r,
              onHand: r.onHand + qty,
              available: r.available + qty,
            }
          : r,
      ),
    );
    try {
      await stockApi.receive(row.id, qty, reason);
      toast.success("Réapprovisionnement enregistré", {
        description: `${row.productName} — +${qty} unités.`,
      });
      load();
    } catch (err) {
      setRows(previous);
      const msg = err instanceof ApiError ? err.message : "Échec du réappro";
      toast.error("Réappro échoué", { description: msg });
    }
  }

  return (
    <div className="space-y-5">
      <ViewHeader
        title="Stock"
        description="Quantités physiques, réservées et disponibles. Mouvements tracés pour audit."
        actions={
          <>
            <Button
              variant="outline"
              size="sm"
              onClick={() => setMovementsOpen(true)}
            >
              <ArrowLeftRight className="size-4" /> Mouvement
            </Button>
            <Button
              size="sm"
              onClick={() => {
                setAdjustRow(null);
                setAdjustOpen(true);
              }}
            >
              <SlidersHorizontal className="size-4" /> Ajustement
            </Button>
          </>
        }
      />

      <SummaryCards rows={rows} />

      <Card className="overflow-hidden">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Produit</TableHead>
              <TableHead className="hidden text-right md:table-cell">Physique</TableHead>
              <TableHead className="hidden text-right md:table-cell">Réservé</TableHead>
              <TableHead className="text-right">Disponible</TableHead>
              <TableHead className="hidden text-right sm:table-cell">Seuil</TableHead>
              <TableHead>Statut</TableHead>
              <TableHead className="w-[180px]"></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {loading ? (
              Array.from({ length: 5 }).map((_, i) => (
                <TableRow key={`sk-${i}`}>
                  <TableCell colSpan={7}>
                    <Skeleton className="h-8 w-full" />
                  </TableCell>
                </TableRow>
              ))
            ) : rows.length === 0 ? (
              <TableRow>
                <TableCell
                  colSpan={7}
                  className="py-8 text-center text-sm text-muted-foreground"
                >
                  Aucun stock. Ajoutez des produits pour voir les lignes apparaître.
                </TableCell>
              </TableRow>
            ) : (
              rows.map((s) => {
                const status = stockStatus(s);
                return (
                  <TableRow key={s.id}>
                    <TableCell>
                      <div className="flex items-center gap-3">
                        <span
                          className={cn(
                            "flex size-9 shrink-0 items-center justify-center rounded-md text-white",
                            s.thumbnailColor,
                          )}
                        >
                          <Boxes className="size-4" />
                        </span>
                        <div className="min-w-0">
                          <div className="truncate font-medium">{s.productName}</div>
                          <div className="text-xs text-muted-foreground">
                            {s.variantLabel}
                          </div>
                        </div>
                      </div>
                    </TableCell>
                    <TableCell className="hidden text-right tabular-nums md:table-cell">
                      {s.onHand}
                    </TableCell>
                    <TableCell className="hidden text-right tabular-nums text-muted-foreground md:table-cell">
                      {s.reserved > 0 ? (
                        <span className="text-amber-accent">{s.reserved}</span>
                      ) : (
                        "—"
                      )}
                    </TableCell>
                    <TableCell
                      className={cn(
                        "text-right font-semibold tabular-nums",
                        status === "out" && "text-destructive",
                        status === "low" && "text-amber-accent",
                      )}
                    >
                      {s.available}
                    </TableCell>
                    <TableCell className="hidden text-right tabular-nums text-muted-foreground sm:table-cell">
                      {s.threshold}
                    </TableCell>
                    <TableCell>
                      <StatusBadge s={s} />
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center justify-end gap-1">
                        <Button
                          size="sm"
                          variant="outline"
                          className="h-7"
                          onClick={() => {
                            setAdjustRow(s);
                            setAdjustOpen(true);
                          }}
                        >
                          Ajuster
                        </Button>
                        <Button
                          size="sm"
                          variant="outline"
                          className="h-7"
                          onClick={() => {
                            setReceiveRow(s);
                            setReceiveOpen(true);
                          }}
                        >
                          Réappro
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                );
              })
            )}
          </TableBody>
        </Table>
      </Card>

      <AdjustDialog
        open={adjustOpen}
        onOpenChange={(v) => {
          setAdjustOpen(v);
          if (!v) setAdjustRow(null);
        }}
        row={adjustRow}
        onConfirm={handleAdjust}
      />

      <ReceiveDialog
        open={receiveOpen}
        onOpenChange={(v) => {
          setReceiveOpen(v);
          if (!v) setReceiveRow(null);
        }}
        row={receiveRow}
        onConfirm={handleReceive}
      />

      <MovementsDialog
        open={movementsOpen}
        onOpenChange={setMovementsOpen}
        rows={rows}
      />
    </div>
  );
}
