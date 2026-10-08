"use client";

import * as React from "react";
import {
  Search,
  Download,
  MoreHorizontal,
  Phone,
  Eye,
  ShoppingBag,
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  customers as allCustomers,
  customerStatusLabel,
  type CustomerStatus,
} from "@/lib/mock-data";
import { formatFCFA, formatDate, initials } from "@/lib/format";
import { useUIStore } from "@/stores/ui-store";
import { ViewHeader } from "./view-header";

function statusBadge(s: CustomerStatus) {
  if (s === "regular")
    return (
      <Badge className="bg-brand/10 text-brand hover:bg-brand/20">Récurrent</Badge>
    );
  if (s === "client")
    return <Badge variant="secondary">Client</Badge>;
  return <Badge variant="outline">Prospect</Badge>;
}

export function CustomersView() {
  const [query, setQuery] = React.useState("");
  const [status, setStatus] = React.useState<string>("all");
  const setCurrentView = useUIStore((s) => s.setCurrentView);

  const filtered = allCustomers.filter((c) => {
    if (status !== "all" && c.status !== status) return false;
    if (query) {
      const q = query.toLowerCase();
      if (
        !c.name.toLowerCase().includes(q) &&
        !c.phone.toLowerCase().includes(q)
      )
        return false;
    }
    return true;
  });

  const totalSpent = filtered.reduce((sum, c) => sum + c.totalSpent, 0);
  const totalOrders = filtered.reduce((sum, c) => sum + c.ordersCount, 0);

  return (
    <div className="space-y-5">
      <ViewHeader
        title="Clients"
        description="Tous les contacts issus de vos conversations WhatsApp."
        actions={
          <Button
            variant="outline"
            size="sm"
            onClick={() => toast.success("Export CSV en préparation")}
          >
            <Download className="size-4" /> Exporter
          </Button>
        }
      />

      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
        <Card className="p-4">
          <div className="text-xs text-muted-foreground">Total clients</div>
          <div className="text-2xl font-bold">{filtered.length}</div>
        </Card>
        <Card className="p-4">
          <div className="text-xs text-muted-foreground">Commandes cumulées</div>
          <div className="text-2xl font-bold">{totalOrders}</div>
        </Card>
        <Card className="col-span-2 p-4 sm:col-span-1">
          <div className="text-xs text-muted-foreground">Panier cumulé</div>
          <div className="text-2xl font-bold">{formatFCFA(totalSpent)}</div>
        </Card>
      </div>

      <Card className="overflow-hidden">
        <div className="flex flex-col gap-2 border-b p-3 sm:flex-row sm:items-center">
          <div className="relative flex-1">
            <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              placeholder="Rechercher nom ou téléphone…"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              className="pl-9"
              aria-label="Rechercher un client"
            />
          </div>
          <Select value={status} onValueChange={setStatus}>
            <SelectTrigger className="w-full sm:w-44">
              <SelectValue placeholder="Statut" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">Tous les statuts</SelectItem>
              <SelectItem value="prospect">Prospect</SelectItem>
              <SelectItem value="client">Client</SelectItem>
              <SelectItem value="regular">Récurrent</SelectItem>
            </SelectContent>
          </Select>
        </div>

        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Client</TableHead>
              <TableHead className="hidden md:table-cell">Téléphone</TableHead>
              <TableHead className="text-right">Commandes</TableHead>
              <TableHead className="text-right">Total dépensé</TableHead>
              <TableHead className="hidden text-right sm:table-cell">
                Dernière commande
              </TableHead>
              <TableHead>Statut</TableHead>
              <TableHead className="w-[60px]"></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {filtered.length === 0 ? (
              <TableRow>
                <TableCell
                  colSpan={7}
                  className="py-8 text-center text-sm text-muted-foreground"
                >
                  Aucun client.
                </TableCell>
              </TableRow>
            ) : (
              filtered.map((c) => (
                <TableRow key={c.id}>
                  <TableCell>
                    <div className="flex items-center gap-3">
                      <span className="flex size-9 shrink-0 items-center justify-center rounded-full bg-brand/10 text-xs font-semibold text-brand">
                        {initials(c.name)}
                      </span>
                      <div className="min-w-0">
                        <div className="truncate font-medium">{c.name}</div>
                        <div className="text-xs text-muted-foreground md:hidden">
                          {c.phone}
                        </div>
                      </div>
                    </div>
                  </TableCell>
                  <TableCell className="hidden text-muted-foreground md:table-cell">
                    <span className="inline-flex items-center gap-1.5">
                      <Phone className="size-3.5" />
                      {c.phone}
                    </span>
                  </TableCell>
                  <TableCell className="text-right font-medium tabular-nums">
                    {c.ordersCount}
                  </TableCell>
                  <TableCell className="text-right font-semibold tabular-nums">
                    {formatFCFA(c.totalSpent)}
                  </TableCell>
                  <TableCell className="hidden text-right text-xs text-muted-foreground sm:table-cell">
                    {formatDate(c.lastOrderAt)}
                  </TableCell>
                  <TableCell>{statusBadge(c.status)}</TableCell>
                  <TableCell>
                    <DropdownMenu>
                      <DropdownMenuTrigger asChild>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="size-8"
                          aria-label="Actions client"
                        >
                          <MoreHorizontal className="size-4" />
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end">
                        <DropdownMenuItem
                          onClick={() => setCurrentView("conversations")}
                        >
                          <Eye className="size-4" /> Voir conversations
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          onClick={() => setCurrentView("orders")}
                        >
                          <ShoppingBag className="size-4" /> Voir commandes
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          onClick={() => toast.info(`Appeler ${c.name}`)}
                        >
                          <Phone className="size-4" /> Appeler
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </Card>
    </div>
  );
}
