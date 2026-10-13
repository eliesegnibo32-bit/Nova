"use client";

import * as React from "react";
import {
  Boxes,
  AlertTriangle,
  PackageX,
  CheckCircle2,
  SlidersHorizontal,
  ArrowLeftRight,
  X,
} from "lucide-react";
import { Card } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { useAuthStore } from "@/stores/auth-store";
import { stockRows as mockStockRows, stockStatus, type StockRow } from "@/lib/mock-data";
import { stockApi, ApiError } from "@/lib/api";
import { toast } from "sonner";
import { formatFCFA } from "@/lib/format";

export function StockView() {
  const isDemo = useAuthStore((s) => s.isDemo);
  const [rows, setRows] = React.useState<StockRow[]>(isDemo ? mockStockRows : []);
  const [loading, setLoading] = React.useState(!isDemo);
  const [adjustRow, setAdjustRow] = React.useState<StockRow | null>(null);
  const [receiveRow, setReceiveRow] = React.useState<StockRow | null>(null);
  const [showMovements, setShowMovements] = React.useState(false);

  const load = React.useCallback(async () => {
    if (isDemo) { setRows(mockStockRows); setLoading(false); return; }
    setLoading(true);
    try {
      const res = await stockApi.list();
      if (res?.items && Array.isArray(res.items)) {
        setRows(res.items.map((item: any) => ({
          id: item.variant_id || item.id,
          productName: item.product_name || item.name || "Produit",
          sku: item.sku || "",
          size: item.size || "",
          color: item.color || "",
          onHand: item.on_hand || 0,
          reserved: item.reserved || 0,
          available: item.available !== undefined ? item.available : (item.on_hand || 0) - (item.reserved || 0),
          threshold: item.alert_threshold || 5,
          price: item.price || 0,
        })));
      } else {
        setRows([]);
      }
    } catch {
      setRows([]);
    } finally {
      setLoading(false);
    }
  }, [isDemo]);

  React.useEffect(() => { load(); }, [load]);

  async function handleAdjust(delta: number, reason: string) {
    if (!adjustRow || isDemo) { setAdjustRow(null); return; }
    const previous = rows;
    setRows((prev) => prev.map((r) => r.id === adjustRow.id ? { ...r, onHand: Math.max(0, r.onHand + delta), available: Math.max(0, r.onHand + delta - r.reserved) } : r));
    try {
      await stockApi.adjust(adjustRow.id, delta, reason);
      toast.success("Stock ajusté");
    } catch (err) {
      setRows(previous);
      toast.error("Ajustement échoué");
    }
    setAdjustRow(null);
  }

  async function handleReceive(qty: number, reason: string) {
    if (!receiveRow || isDemo) { setReceiveRow(null); return; }
    const previous = rows;
    setRows((prev) => prev.map((r) => r.id === receiveRow.id ? { ...r, onHand: r.onHand + qty, available: r.available + qty } : r));
    try {
      await stockApi.receive(receiveRow.id, qty, reason);
      toast.success("Stock réapprovisionné");
    } catch (err) {
      setRows(previous);
      toast.error("Réappro échoué");
    }
    setReceiveRow(null);
  }

  const totalRefs = rows.length;
  const inStock = rows.filter((r) => r.available > 0 && r.available > r.threshold).length;
  const lowStock = rows.filter((r) => r.available > 0 && r.available <= r.threshold).length;
  const outStock = rows.filter((r) => r.available <= 0).length;

  if (loading) {
    return <div className="p-8 text-center text-muted-foreground">Chargement du stock…</div>;
  }

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-2xl font-bold">Stock</h1>
        <p className="text-sm text-muted-foreground">Quantités physiques, réservées et disponibles.</p>
      </div>

      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <Card className="p-4"><div className="flex items-center gap-3"><div className="flex size-10 items-center justify-center rounded-lg bg-muted"><Boxes className="size-5 text-blue-600" /></div><div><div className="text-2xl font-bold">{totalRefs}</div><div className="text-xs text-muted-foreground">Total références</div></div></div></Card>
        <Card className="p-4"><div className="flex items-center gap-3"><div className="flex size-10 items-center justify-center rounded-lg bg-muted"><CheckCircle2 className="size-5 text-emerald-600" /></div><div><div className="text-2xl font-bold">{inStock}</div><div className="text-xs text-muted-foreground">En stock</div></div></div></Card>
        <Card className="p-4"><div className="flex items-center gap-3"><div className="flex size-10 items-center justify-center rounded-lg bg-muted"><AlertTriangle className="size-5 text-amber-600" /></div><div><div className="text-2xl font-bold">{lowStock}</div><div className="text-xs text-muted-foreground">Sous le seuil</div></div></div></Card>
        <Card className="p-4"><div className="flex items-center gap-3"><div className="flex size-10 items-center justify-center rounded-lg bg-muted"><PackageX className="size-5 text-red-600" /></div><div><div className="text-2xl font-bold">{outStock}</div><div className="text-xs text-muted-foreground">En rupture</div></div></div></Card>
      </div>

      {rows.length === 0 ? (
        <Card className="p-8 text-center">
          <Boxes className="mx-auto mb-4 size-12 text-muted-foreground" />
          <p className="text-muted-foreground">Aucun produit en stock pour le moment.</p>
        </Card>
      ) : (
        <Card className="overflow-hidden">
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="border-b bg-muted/50">
                <tr>
                  <th className="p-3 text-left font-medium">Produit</th>
                  <th className="p-3 text-right font-medium">Physique</th>
                  <th className="p-3 text-right font-medium">Réservé</th>
                  <th className="p-3 text-right font-medium">Disponible</th>
                  <th className="p-3 text-right font-medium">Seuil</th>
                  <th className="p-3 text-center font-medium">Statut</th>
                  <th className="p-3 text-center font-medium">Actions</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((row) => {
                  const status = stockStatus(row);
                  return (
                    <tr key={row.id} className="border-b last:border-0">
                      <td className="p-3">
                        <div className="font-medium">{row.productName}</div>
                        <div className="text-xs text-muted-foreground">{[row.color, row.size].filter(Boolean).join(" / ") || "Unique"}</div>
                      </td>
                      <td className="p-3 text-right">{row.onHand}</td>
                      <td className="p-3 text-right">{row.reserved > 0 ? row.reserved : "—"}</td>
                      <td className="p-3 text-right font-medium">{row.available}</td>
                      <td className="p-3 text-right text-muted-foreground">{row.threshold}</td>
                      <td className="p-3 text-center">
                        {status === "ok" && <Badge className="bg-emerald-500">OK</Badge>}
                        {status === "low" && <Badge className="bg-amber-500">Faible</Badge>}
                        {status === "out" && <Badge variant="destructive">Rupture</Badge>}
                      </td>
                      <td className="p-3 text-center">
                        <div className="flex justify-center gap-1">
                          <Button size="sm" variant="ghost" onClick={() => setAdjustRow(row)}>Ajuster</Button>
                          <Button size="sm" variant="ghost" onClick={() => setReceiveRow(row)}>Réappro</Button>
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </Card>
      )}

      {/* Dialog Ajustement */}
      <AdjustDialog row={adjustRow} onClose={() => setAdjustRow(null)} onSubmit={handleAdjust} />
      {/* Dialog Réappro */}
      <ReceiveDialog row={receiveRow} onClose={() => setReceiveRow(null)} onSubmit={handleReceive} />
    </div>
  );
}

function AdjustDialog({ row, onClose, onSubmit }: { row: StockRow | null; onClose: () => void; onSubmit: (delta: number, reason: string) => void }) {
  const [delta, setDelta] = React.useState("");
  const [reason, setReason] = React.useState("");
  if (!row) return null;
  return (
    <Dialog open={!!row} onOpenChange={(v) => !v && onClose()}>
      <DialogContent>
        <DialogHeader><DialogTitle>Ajuster le stock</DialogTitle><DialogDescription>{row.productName}</DialogDescription></DialogHeader>
        <div className="space-y-3">
          <div><Label>Delta (positif ou négatif)</Label><Input type="number" value={delta} onChange={(e) => setDelta(e.target.value)} placeholder="ex: -2 ou +5" /></div>
          <div><Label>Raison</Label><Input value={reason} onChange={(e) => setReason(e.target.value)} placeholder="Casse, inventaire, erreur…" /></div>
          <div className="flex gap-2"><Button variant="outline" className="flex-1" onClick={onClose}>Annuler</Button><Button className="flex-1" onClick={() => onSubmit(Number(delta) || 0, reason || "Ajustement")}>Valider</Button></div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function ReceiveDialog({ row, onClose, onSubmit }: { row: StockRow | null; onClose: () => void; onSubmit: (qty: number, reason: string) => void }) {
  const [qty, setQty] = React.useState("");
  if (!row) return null;
  return (
    <Dialog open={!!row} onOpenChange={(v) => !v && onClose()}>
      <DialogContent>
        <DialogHeader><DialogTitle>Réapprovisionner</DialogTitle><DialogDescription>{row.productName}</DialogDescription></DialogHeader>
        <div className="space-y-3">
          <div><Label>Quantité</Label><Input type="number" value={qty} onChange={(e) => setQty(e.target.value)} placeholder="ex: 10" /></div>
          <div className="flex gap-2"><Button variant="outline" className="flex-1" onClick={onClose}>Annuler</Button><Button className="flex-1" onClick={() => onSubmit(Number(qty) || 0, "Réapprovisionnement")}>Valider</Button></div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
