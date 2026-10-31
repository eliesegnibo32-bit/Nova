"use client";

import * as React from "react";
import {
  LayoutDashboard,
  Store,
  Users,
  Banknote,
  Activity,
  LifeBuoy,
  ScrollText,
  Settings,
  LogOut,
  Menu,
  Shield,
  Bell,
  Moon,
  Sun,
} from "lucide-react";
import { useTheme } from "next-themes";
import { toast } from "sonner";
import { cn } from "@/lib/utils";
import { NovaLogo } from "@/components/ui/nova-logo";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import {
  Sheet,
  SheetContent,
  SheetTrigger,
} from "@/components/ui/sheet";
import { useAuthStore } from "@/stores/auth-store";
import { useUIStore, type ViewKey } from "@/stores/ui-store";
import { ViewErrorBoundary } from "../dashboard/view-error-boundary";
import { AdminOverviewView } from "@/components/views/admin-overview-view";
import { AdminShopsView } from "@/components/views/admin-shops-view";
import { AdminUsersView } from "@/components/views/admin-users-view";
import { AdminRevenueView } from "@/components/views/admin-revenue-view";
import { AdminMonitoringView } from "@/components/views/admin-monitoring-view";
import { AdminSupportView } from "@/components/views/admin-support-view";
import { AdminAuditView } from "@/components/views/admin-audit-view";
import { AdminSettingsView } from "@/components/views/admin-settings-view";
import { initials } from "@/lib/format";

const ADMIN_NAV: { key: ViewKey; label: string; icon: React.ComponentType<{ className?: string }> }[] = [
  { key: "admin-overview", label: "Vue d'ensemble", icon: LayoutDashboard },
  { key: "admin-shops", label: "Boutiques", icon: Store },
  { key: "admin-users", label: "Utilisateurs", icon: Users },
  { key: "admin-revenue", label: "Revenus", icon: Banknote },
  { key: "admin-monitoring", label: "Supervision", icon: Activity },
  { key: "admin-support", label: "Support", icon: LifeBuoy },
  { key: "admin-audit", label: "Journal d'audit", icon: ScrollText },
  { key: "admin-settings", label: "Paramètres", icon: Settings },
];

function AdminNavContent({
  currentView,
  onSelect,
  user,
  onLogout,
}: {
  currentView: ViewKey;
  onSelect: (v: ViewKey) => void;
  user: { fullName?: string; email?: string } | null;
  onLogout: () => void;
}) {
  return (
    <>
      {/* Logo */}
      <div className="flex items-center gap-2.5 border-b px-4 py-4">
        <NovaLogo size={32} withWordmark={false} />
        <div className="min-w-0">
          <div className="truncate text-sm font-bold">NOVA Admin</div>
          <div className="truncate text-[11px] text-muted-foreground">
            Console plateforme
          </div>
        </div>
      </div>

      {/* Navigation */}
      <nav className="flex-1 space-y-1 overflow-y-auto px-3 py-4 scrollbar-thin">
        <div className="px-2.5 pb-1 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
          Administration
        </div>
        {ADMIN_NAV.map((item) => {
          const active = currentView === item.key;
          return (
            <button
              key={item.key}
              onClick={() => onSelect(item.key)}
              aria-current={active ? "page" : undefined}
              className={cn(
                "group flex w-full items-center gap-2.5 rounded-lg px-2.5 py-2 text-sm font-medium transition-colors",
                active
                  ? "bg-brand text-brand-foreground"
                  : "text-muted-foreground hover:bg-accent hover:text-foreground",
              )}
            >
              <item.icon className="size-4 shrink-0" />
              {item.label}
            </button>
          );
        })}
      </nav>

      {/* User + logout */}
      <div className="border-t p-3">
        <div className="flex items-center gap-2.5 rounded-lg px-2 py-2">
          <Avatar className="size-8 shrink-0">
            <AvatarFallback className="bg-brand/10 text-xs font-semibold text-brand">
              {initials(user?.fullName)}
            </AvatarFallback>
          </Avatar>
          <div className="min-w-0 flex-1">
            <div className="truncate text-sm font-medium">
              {user?.fullName || "Admin"}
            </div>
            <div className="truncate text-[11px] text-muted-foreground">
              {user?.email}
            </div>
          </div>
          <Badge className="bg-brand/10 text-brand" variant="secondary">
            <Shield className="size-3" /> Admin
          </Badge>
        </div>
        <Button
          variant="ghost"
          size="sm"
          className="mt-2 w-full justify-start gap-2 text-muted-foreground"
          onClick={onLogout}
        >
          <LogOut className="size-4" /> Déconnexion
        </Button>
      </div>
    </>
  );
}

export function AdminShell() {
  const currentView = useUIStore((s) => s.currentView);
  const setCurrentView = useUIStore((s) => s.setCurrentView);
  const user = useAuthStore((s) => s.user);
  const logout = useAuthStore((s) => s.logout);
  const [mobileOpen, setMobileOpen] = React.useState(false);

  // S'assure qu'on est sur une vue admin
  React.useEffect(() => {
    if (!currentView.startsWith("admin-")) {
      setCurrentView("admin-overview");
    }
  }, [currentView, setCurrentView]);

  const renderView = () => {
    if (currentView === "admin-overview") return <AdminOverviewView />;
    if (currentView === "admin-shops") return <AdminShopsView />;
    if (currentView === "admin-users") return <AdminUsersView />;
    if (currentView === "admin-revenue") return <AdminRevenueView />;
    if (currentView === "admin-monitoring") return <AdminMonitoringView />;
    if (currentView === "admin-support") return <AdminSupportView />;
    if (currentView === "admin-audit") return <AdminAuditView />;
    if (currentView === "admin-settings") return <AdminSettingsView />;
    return <AdminOverviewView />;
  };

  const handleLogout = async () => {
    await logout();
    toast.success("Déconnexion");
  };

  const handleSelect = (v: ViewKey) => {
    setCurrentView(v);
    setMobileOpen(false);
  };

  return (
    <div className="flex flex-1 flex-col overflow-hidden">
      {/* Header admin */}
      <header className="flex h-14 items-center justify-between border-b bg-background px-4">
        <div className="flex items-center gap-3">
          {/* Mobile menu */}
          <Sheet open={mobileOpen} onOpenChange={setMobileOpen}>
            <SheetTrigger asChild>
              <Button variant="ghost" size="icon" className="md:hidden">
                <Menu className="size-5" />
              </Button>
            </SheetTrigger>
            <SheetContent side="left" className="w-72 p-0">
              <div className="flex h-full flex-col bg-card">
                <AdminNavContent
                  currentView={currentView}
                  onSelect={handleSelect}
                  user={user}
                  onLogout={handleLogout}
                />
              </div>
            </SheetContent>
          </Sheet>
          <h1 className="text-lg font-bold">Console Admin — NOVA</h1>
        </div>
        <div className="flex items-center gap-2">
          <ThemeToggle />
          <Button variant="ghost" size="icon" aria-label="Notifications">
            <Bell className="size-5" />
          </Button>
        </div>
      </header>

      <div className="flex flex-1 overflow-hidden">
        {/* Sidebar desktop */}
        <aside className="hidden w-64 flex-col border-r bg-card md:flex">
          <AdminNavContent
            currentView={currentView}
            onSelect={handleSelect}
            user={user}
            onLogout={handleLogout}
          />
        </aside>

        {/* Main */}
        <div className="flex min-w-0 flex-1 flex-col">
          <main
            className="flex-1 overflow-y-auto bg-muted/30 scrollbar-thin"
            id="nova-admin-main"
          >
            <div className="mx-auto w-full max-w-[1400px] px-4 py-5 sm:px-6 sm:py-6">
              <React.Suspense
                fallback={<div className="p-8 text-sm text-muted-foreground">Chargement…</div>}
              >
                <ViewErrorBoundary viewName={currentView}>
                  {renderView()}
                </ViewErrorBoundary>
              </React.Suspense>
            </div>
          </main>
          <footer className="border-t px-4 py-3 text-center text-xs text-muted-foreground">
            NOVA Admin v1.0 — © 2026 · Console de gestion plateforme
          </footer>
        </div>
      </div>
    </div>
  );
}

function ThemeToggle() {
  const { theme, setTheme } = useTheme();
  return (
    <Button
      variant="ghost"
      size="icon"
      aria-label="Changer de thème"
      onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
    >
      <Sun className="size-5 rotate-0 scale-100 transition-all dark:-rotate-90 dark:scale-0" />
      <Moon className="absolute size-5 rotate-90 scale-0 transition-all dark:rotate-0 dark:scale-100" />
    </Button>
  );
}
