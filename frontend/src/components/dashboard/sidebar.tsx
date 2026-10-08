"use client";

import * as React from "react";
import Link from "next/link";
import {
  Home,
  Package,
  Boxes,
  ShoppingCart,
  MessageCircle,
  Users,
  Wallet,
  BarChart3,
  CreditCard,
  UserCog,
  Store,
  ChevronsUpDown,
  LogOut,
  User,
  Sparkles,
  Plus,
} from "lucide-react";

import { cn } from "@/lib/utils";
import { NovaLogo } from "@/components/ui/nova-logo";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Sheet,
  SheetContent,
  SheetTrigger,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useUIStore, type ViewKey } from "@/stores/ui-store";
import { useAuthStore } from "@/stores/auth-store";
import { demoShop } from "@/lib/mock-data";
import { initials } from "@/lib/format";
import { CreateShopDialog } from "./create-shop-dialog";

interface NavItem {
  key: ViewKey;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
  badge?: number;
}

interface NavGroup {
  label: string;
  items: NavItem[];
}

const NAV_GROUPS: NavGroup[] = [
  {
    label: "Commerce",
    items: [
      { key: "home", label: "Accueil", icon: Home },
      { key: "products", label: "Produits", icon: Package },
      { key: "stock", label: "Stock", icon: Boxes, badge: 8 },
      { key: "orders", label: "Commandes", icon: ShoppingCart, badge: 4 },
    ],
  },
  {
    label: "Relation client",
    items: [
      { key: "conversations", label: "Conversations", icon: MessageCircle, badge: 5 },
      { key: "customers", label: "Clients", icon: Users },
    ],
  },
  {
    label: "Gestion",
    items: [
      { key: "payment", label: "Paiement", icon: Wallet },
      { key: "stats", label: "Statistiques", icon: BarChart3 },
      { key: "subscription", label: "Abonnement", icon: CreditCard },
      { key: "team", label: "Équipe", icon: UserCog },
    ],
  },
];

function ShopSwitcher() {
  const [open, setOpen] = React.useState(false);
  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <button
            className="flex w-full items-center gap-2.5 rounded-lg border bg-card px-2.5 py-2 text-left transition-colors hover:bg-accent"
            aria-label="Changer de boutique"
          >
            <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-brand text-brand-foreground">
              <Store className="size-4" />
            </span>
            <div className="min-w-0 flex-1">
              <div className="truncate text-sm font-semibold">{demoShop.name}</div>
              <div className="truncate text-[11px] text-muted-foreground">
                {demoShop.plan} · {demoShop.country}
              </div>
            </div>
            <ChevronsUpDown className="size-4 shrink-0 text-muted-foreground" />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="w-[232px]">
          <DropdownMenuLabel className="text-xs text-muted-foreground">
            Boutique active
          </DropdownMenuLabel>
          <DropdownMenuItem className="gap-2.5">
            <span className="flex size-7 items-center justify-center rounded-md bg-brand text-brand-foreground">
              <Store className="size-3.5" />
            </span>
            <div className="min-w-0 flex-1">
              <div className="truncate text-sm font-medium">{demoShop.name}</div>
              <div className="truncate text-[10px] text-muted-foreground">
                {demoShop.slug}.nova.ci
              </div>
            </div>
            <span className="size-2 rounded-full bg-emerald-500" />
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem
            onSelect={(e) => {
              e.preventDefault();
              setOpen(true);
            }}
            className="gap-2 text-muted-foreground"
          >
            <Plus className="size-4" /> Créer une boutique
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <CreateShopDialog open={open} onOpenChange={setOpen} />
    </>
  );
}

function UserMenu() {
  const user = useAuthStore((s) => s.user);
  const logout = useAuthStore((s) => s.logout);
  const setCurrentView = useUIStore((s) => s.setCurrentView);
  const setProfileTab = useUIStore((s) => s.setProfileTab);
  const name = user?.fullName || "Commerçant";
  const email = user?.email || "—";

  function goProfile(tab: "infos" | "parametres") {
    setProfileTab(tab);
    setCurrentView("profile");
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          className="flex w-full items-center gap-2.5 rounded-lg px-2 py-1.5 text-left transition-colors hover:bg-accent"
          aria-label="Menu utilisateur"
        >
          <Avatar className="size-8">
            <AvatarFallback className="bg-brand text-brand-foreground text-xs font-semibold">
              {initials(name)}
            </AvatarFallback>
          </Avatar>
          <div className="min-w-0 flex-1">
            <div className="truncate text-sm font-medium">{name}</div>
            <div className="truncate text-[11px] text-muted-foreground">
              {email}
            </div>
          </div>
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-[232px]">
        <DropdownMenuLabel className="flex flex-col">
          <span className="text-sm font-medium">{name}</span>
          <span className="truncate text-[11px] font-normal text-muted-foreground">
            {email}
          </span>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem onSelect={() => goProfile("infos")}>
          <User className="size-4" /> Mon profil
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => goProfile("parametres")}>
          <UserCog className="size-4" /> Paramètres compte
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          className="text-destructive focus:text-destructive"
          onClick={() => logout()}
        >
          <LogOut className="size-4" /> Déconnexion
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function NavList({ onNavigate }: { onNavigate?: () => void }) {
  const currentView = useUIStore((s) => s.currentView);
  const setCurrentView = useUIStore((s) => s.setCurrentView);

  return (
    <nav className="flex flex-1 flex-col gap-5 overflow-y-auto px-3 py-3 scrollbar-thin">
      {NAV_GROUPS.map((group) => (
        <div key={group.label} className="space-y-1">
          <div className="px-2.5 pb-1 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
            {group.label}
          </div>
          {group.items.map((item) => {
            const active = currentView === item.key;
            return (
              <button
                key={item.key}
                onClick={() => {
                  setCurrentView(item.key);
                  onNavigate?.();
                }}
                aria-current={active ? "page" : undefined}
                className={cn(
                  "group flex w-full items-center gap-2.5 rounded-lg px-2.5 py-2 text-sm font-medium transition-colors",
                  active
                    ? "bg-brand text-brand-foreground shadow-sm"
                    : "text-sidebar-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
                )}
              >
                <item.icon
                  className={cn(
                    "size-4 shrink-0",
                    !active && "text-muted-foreground group-hover:text-sidebar-accent-foreground",
                  )}
                />
                <span className="flex-1 truncate text-left">{item.label}</span>
                {item.badge ? (
                  <Badge
                    variant={active ? "secondary" : "outline"}
                    className={cn(
                      "h-5 px-1.5 text-[10px]",
                      active
                        ? "bg-white/20 text-brand-foreground border-transparent"
                        : "border-amber-accent/30 text-amber-accent bg-amber-accent/10",
                    )}
                  >
                    {item.badge}
                  </Badge>
                ) : null}
              </button>
            );
          })}
        </div>
      ))}
    </nav>
  );
}

function SidebarFooter() {
  return (
    <div className="space-y-2 border-t p-3">
      <ShopSwitcher />
      <UserMenu />
    </div>
  );
}

/** Sidebar desktop (md+) */
export function Sidebar() {
  return (
    <aside className="hidden w-[260px] shrink-0 flex-col border-r bg-sidebar md:flex">
      <div className="flex h-16 items-center border-b px-4">
        <NovaLogo withWordmark size={32} />
      </div>
      <NavList />
      <SidebarFooter />
    </aside>
  );
}

/** Sidebar mobile (Sheet drawer) */
export function MobileSidebar() {
  const open = useUIStore((s) => s.sidebarOpen);
  const setOpen = useUIStore((s) => s.setSidebarOpen);

  return (
    <Sheet open={open} onOpenChange={setOpen}>
      <SheetContent
        side="left"
        className="w-[280px] border-r p-0 sm:max-w-[280px]"
      >
        <SheetHeader className="h-16 flex-row items-center border-b px-4 space-y-0">
          <NovaLogo withWordmark size={32} />
          <SheetTitle className="sr-only">Menu NOVA</SheetTitle>
        </SheetHeader>
        <NavList onNavigate={() => setOpen(false)} />
        <SidebarFooter />
      </SheetContent>
    </Sheet>
  );
}

/** Cartouche promo IA dans le header mobile */
export function AiHelperCTA() {
  const setCurrentView = useUIStore((s) => s.setCurrentView);
  return (
    <Button
      variant="outline"
      size="sm"
      onClick={() => setCurrentView("conversations")}
      className="hidden items-center gap-2 sm:inline-flex"
    >
      <Sparkles className="size-3.5 text-brand" />
      Prendre la main
      <Badge className="bg-amber-accent/15 text-amber-accent hover:bg-amber-accent/25">
        5
      </Badge>
    </Button>
  );
}

export { NAV_GROUPS };
export type { NavItem };
