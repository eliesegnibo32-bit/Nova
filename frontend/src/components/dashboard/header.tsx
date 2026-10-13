"use client";

import * as React from "react";
import { Menu, Search } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useUIStore, viewLabels } from "@/stores/ui-store";
import { ThemeToggle } from "./theme-toggle";
import { Notifications, NotificationsBadge } from "./notifications";
import { AiHelperCTA } from "./sidebar";

export function Header() {
  const toggleSidebar = useUIStore((s) => s.toggleSidebar);
  const currentView = useUIStore((s) => s.currentView);
  const title = viewLabels[currentView];

  return (
    <header className="sticky top-0 z-30 flex h-16 items-center gap-2 border-b bg-background/95 px-4 backdrop-blur supports-[backdrop-filter]:bg-background/80 sm:px-6">
      {/* Burger mobile */}
      <Button
        variant="ghost"
        size="icon"
        className="md:hidden"
        onClick={toggleSidebar}
        aria-label="Ouvrir le menu"
      >
        <Menu className="size-5" />
      </Button>

      {/* Titre desktop */}
      <div className="hidden md:block">
        <h1 className="text-lg font-semibold tracking-tight">{title}</h1>
      </div>

      {/* Titre mobile (compact) */}
      <div className="md:hidden">
        <h1 className="text-base font-semibold tracking-tight">{title}</h1>
      </div>

      <div className="flex flex-1 items-center justify-end gap-2">
        {/* Recherche desktop */}
        <div className="relative hidden md:block">
          <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            type="search"
            placeholder="Rechercher commande, client, produit…"
            className="h-9 w-64 pl-9 lg:w-80"
            aria-label="Recherche"
          />
        </div>

        <AiHelperCTA />

        {/* Mobile: "Prendre la main" icone-only */}
        <Button
          variant="ghost"
          size="icon"
          className="relative sm:hidden"
          aria-label="Conversations à reprendre"
        >
          <NotificationsBadge />
        </Button>

        <Notifications />
        <ThemeToggle />
      </div>
    </header>
  );
}
