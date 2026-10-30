"use client";

import * as React from "react";
import {
  MapPin,
  Truck,
  Phone,
  CheckCircle2,
  XCircle,
  Clock,
  Calendar,
  Eye,
} from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
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
import { cn } from "@/lib/utils";
import {
  deliveries as allDeliveries,
  deliveryStatusLabel,
  type DeliveryStatus,
} from "@/lib/mock-data";
import { formatDateTime, formatDate } from "@/lib/format";
import { ViewHeader } from "./view-header";

const TABS: { key: DeliveryStatus | "all"; label: string }[] = [
  { key: "all", label: "Toutes" },
  { key: "in_progress", label: "En cours" },
  { key: "scheduled", label: "Planifiées" },
  { key: "delivered", label: "Livrées" },
  { key: "failed", label: "Échecs" },
];

function statusBadge(s: DeliveryStatus) {
  const map: Record<DeliveryStatus, { cls: string; icon: React.ReactNode }> = {
    in_progress: {
      cls: "bg-blue-500/10 text-blue-600 dark:text-blue-400 hover:bg-blue-500/20",
      icon: <Truck className="size-3" />,
    },
    scheduled: {
      cls: "bg-amber-accent/15 text-amber-accent hover:bg-amber-accent/25",
      icon: <Calendar className="size-3" />,
    },
    delivered: {
      cls: "bg-emerald-500/10 text-emerald-700 dark:text-emerald-400 hover:bg-emerald-500/20",
      icon: <CheckCircle2 className="size-3" />,
    },
    failed: {
      cls: "bg-destructive/10 text-destructive hover:bg-destructive/20",
      icon: <XCircle className="size-3" />,
    },
  };
  const c = map[s];
  return (
    <Badge className={c.cls}>
      {c.icon}
      {deliveryStatusLabel[s]}
    </Badge>
  );
}

export function DeliveriesView() {
  const [tab, setTab] = React.useState<string>("all");

  const filtered = allDeliveries.filter((d) => tab === "all" || d.status === tab);

  const stats = {
    inProgress: allDeliveries.filter((d) => d.status === "in_progress").length,
    scheduled: allDeliveries.filter((d) => d.status === "scheduled").length,
    delivered: allDeliveries.filter((d) => d.status === "delivered").length,
    failed: allDeliveries.filter((d) => d.status === "failed").length,
  };

  return (
    <div className="space-y-5">
      <ViewHeader
        title="Livraisons"
        description="Suivi des livraisons par zone et livreur à Abidjan."
        actions={null}
      />

      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <Card className="p-4">
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <Truck className="size-3.5 text-blue-600" /> En cours
          </div>
          <div className="mt-1 text-2xl font-bold">{stats.inProgress}</div>
        </Card>
        <Card className="p-4">
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <Calendar className="size-3.5 text-amber-accent" /> Planifiées
          </div>
          <div className="mt-1 text-2xl font-bold">{stats.scheduled}</div>
        </Card>
        <Card className="p-4">
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <CheckCircle2 className="size-3.5 text-emerald-600" /> Livrées
          </div>
          <div className="mt-1 text-2xl font-bold">{stats.delivered}</div>
        </Card>
        <Card className="p-4">
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <XCircle className="size-3.5 text-destructive" /> Échecs
          </div>
          <div className="mt-1 text-2xl font-bold">{stats.failed}</div>
        </Card>
      </div>

      <Card className="overflow-hidden">
        <div className="border-b p-3">
          <Tabs value={tab} onValueChange={setTab}>
            <TabsList className="flex h-9 flex-wrap justify-start">
              {TABS.map((t) => (
                <TabsTrigger key={t.key} value={t.key} className="text-xs">
                  {t.label}
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
        </div>

        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>N° commande</TableHead>
              <TableHead>Client</TableHead>
              <TableHead className="hidden md:table-cell">Zone</TableHead>
              <TableHead className="hidden lg:table-cell">Adresse</TableHead>
              <TableHead className="hidden sm:table-cell">Livreur</TableHead>
              <TableHead className="hidden text-right lg:table-cell">Prévue le</TableHead>
              <TableHead>Statut</TableHead>
              <TableHead className="w-[60px]"></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {filtered.length === 0 ? (
              <TableRow>
                <TableCell
                  colSpan={8}
                  className="py-8 text-center text-sm text-muted-foreground"
                >
                  Aucune livraison.
                </TableCell>
              </TableRow>
            ) : (
              filtered.map((d) => (
                <TableRow key={d.id}>
                  <TableCell className="font-medium">{d.orderNumber}</TableCell>
                  <TableCell>
                    <div className="font-medium">{d.customerName}</div>
                    <div className="text-xs text-muted-foreground md:hidden">
                      {d.zone}
                    </div>
                  </TableCell>
                  <TableCell className="hidden md:table-cell">
                    <Badge variant="outline">{d.zone}</Badge>
                  </TableCell>
                  <TableCell className="hidden max-w-[200px] text-xs text-muted-foreground lg:table-cell">
                    <span className="inline-flex items-start gap-1">
                      <MapPin className="size-3 shrink-0 translate-y-0.5" />
                      <span className="truncate">{d.address}</span>
                    </span>
                  </TableCell>
                  <TableCell className="hidden sm:table-cell">
                    {d.driver === "—" ? (
                      <span className="text-xs text-muted-foreground">Non assigné</span>
                    ) : (
                      <span className="text-sm">{d.driver}</span>
                    )}
                  </TableCell>
                  <TableCell className="hidden text-right text-xs text-muted-foreground lg:table-cell">
                    {d.scheduledAt ? (
                      <span className="inline-flex items-center gap-1">
                        <Clock className="size-3" />
                        {formatDate(d.scheduledAt)}
                      </span>
                    ) : (
                      "—"
                    )}
                  </TableCell>
                  <TableCell>{statusBadge(d.status)}</TableCell>
                  <TableCell>
                    <Button
                      size="sm"
                      variant="ghost"
                      className="size-8 p-0"
                      aria-label="Voir détail"
                      onClick={() =>
                        toast.info(`Détail livraison ${d.orderNumber}`, {
                          description: `${d.customerName} — ${d.address}`,
                        })
                      }
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
    </div>
  );
}
