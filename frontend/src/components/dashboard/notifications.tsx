"use client";

import * as React from "react";
import {
  Bell,
  MessageCircle,
  ShoppingCart,
  Boxes,
  CreditCard,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuTrigger,
  DropdownMenuLabel,
  DropdownMenuSeparator,
} from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";
import { notifications as mockNotifs } from "@/lib/mock-data";
import { timeAgo } from "@/lib/format";
import { useUIStore, type ViewKey } from "@/stores/ui-store";

const iconFor: Record<
  string,
  { icon: React.ComponentType<{ className?: string }>; color: string }
> = {
  order: { icon: ShoppingCart, color: "text-amber-accent bg-amber-accent/10" },
  conversation: { icon: MessageCircle, color: "text-brand bg-brand/10" },
  stock: { icon: Boxes, color: "text-orange-500 bg-orange-50 dark:bg-orange-950/30" },
  subscription: { icon: CreditCard, color: "text-purple-500 bg-purple-50 dark:bg-purple-950/30" },
};

const viewForType: Record<string, ViewKey> = {
  order: "orders",
  conversation: "conversations",
  stock: "stock",
  subscription: "subscription",
};

export function Notifications() {
  const [items, setItems] = React.useState(mockNotifs);
  const unread = items.filter((n) => !n.read).length;
  const setCurrentView = useUIStore((s) => s.setCurrentView);

  function markAll() {
    setItems((prev) => prev.map((n) => ({ ...n, read: true })));
  }

  function onClick(type: string) {
    const v = viewForType[type];
    if (v) setCurrentView(v);
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          className="relative"
          aria-label={`Notifications${unread ? ` (${unread} non lues)` : ""}`}
        >
          <Bell className="size-4" />
          {unread > 0 && (
            <span className="absolute -right-0.5 -top-0.5 flex size-4 items-center justify-center rounded-full bg-amber-accent text-[10px] font-bold text-amber-accent-foreground">
              {unread}
            </span>
          )}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-80 p-0">
        <div className="flex items-center justify-between p-3">
          <DropdownMenuLabel className="p-0 text-sm font-semibold">
            Notifications
          </DropdownMenuLabel>
          {unread > 0 && (
            <button
              onClick={markAll}
              className="text-xs text-muted-foreground hover:text-foreground hover:underline"
            >
              Tout marquer lu
            </button>
          )}
        </div>
        <DropdownMenuSeparator className="m-0" />
        <div className="max-h-96 overflow-y-auto scrollbar-thin">
          {items.length === 0 ? (
            <div className="p-6 text-center text-sm text-muted-foreground">
              Aucune notification.
            </div>
          ) : (
            items.map((n) => {
              const cfg = iconFor[n.type] || iconFor.order;
              return (
                <button
                  key={n.id}
                  onClick={() => onClick(n.type)}
                  className={cn(
                    "flex w-full items-start gap-3 border-b px-3 py-2.5 text-left transition-colors last:border-b-0 hover:bg-accent",
                    !n.read && "bg-accent/40",
                  )}
                >
                  <span
                    className={cn(
                      "flex size-8 shrink-0 items-center justify-center rounded-full",
                      cfg.color,
                    )}
                  >
                    <cfg.icon className="size-4" />
                  </span>
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="truncate text-sm font-medium">
                        {n.title}
                      </span>
                      {!n.read && (
                        <span className="size-1.5 shrink-0 rounded-full bg-brand" />
                      )}
                    </div>
                    <div className="line-clamp-2 text-xs text-muted-foreground">
                      {n.description}
                    </div>
                    <div className="mt-0.5 text-[10px] text-muted-foreground">
                      {timeAgo(n.at)}
                    </div>
                  </div>
                </button>
              );
            })
          )}
        </div>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export function NotificationsBadge() {
  // Small inline badge for header CTA "Prendre la main"
  const count = mockNotifs.filter(
    (n) => n.type === "conversation" && !n.read,
  ).length;
  if (count === 0) return null;
  return (
    <Badge
      variant="secondary"
      className="ml-1.5 bg-amber-accent/15 text-amber-accent"
    >
      {count}
    </Badge>
  );
}
