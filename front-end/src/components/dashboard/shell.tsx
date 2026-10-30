"use client";

import * as React from "react";
import { MobileSidebar, Sidebar } from "./sidebar";
import { Header } from "./header";
import { Footer } from "./footer";
import { useUIStore } from "@/stores/ui-store";
import { HomeView } from "@/components/views/home-view";
import { ProductsView } from "@/components/views/products-view";
import { StockView } from "@/components/views/stock-view";
import { OrdersView } from "@/components/views/orders-view";
import { ConversationsView } from "@/components/views/conversations-view";
import { CustomersView } from "@/components/views/customers-view";
import { PaymentConfigView } from "@/components/views/payment-config-view";
import { StatsView } from "@/components/views/stats-view";
import { SubscriptionView } from "@/components/views/subscription-view";
import { TeamView } from "@/components/views/team-view";
import { ProfileView } from "@/components/views/profile-view";

export function DashboardShell() {
  const currentView = useUIStore((s) => s.currentView);

  return (
    <div className="flex flex-1 flex-col overflow-hidden">
      <div className="flex flex-1 overflow-hidden">
        <Sidebar />
        <MobileSidebar />
        <div className="flex min-w-0 flex-1 flex-col">
          <Header />
          <main
            className="flex-1 overflow-y-auto bg-muted/30 scrollbar-thin"
            id="nova-main"
          >
            <div className="mx-auto w-full max-w-[1400px] px-4 py-5 sm:px-6 sm:py-6">
              <React.Suspense
                fallback={<div className="p-8 text-sm text-muted-foreground">Chargement…</div>}
              >
                {currentView === "home" && <HomeView />}
                {currentView === "products" && <ProductsView />}
                {currentView === "stock" && <StockView />}
                {currentView === "orders" && <OrdersView />}
                {currentView === "conversations" && <ConversationsView />}
                {currentView === "customers" && <CustomersView />}
                {currentView === "payment" && <PaymentConfigView />}
                {currentView === "stats" && <StatsView />}
                {currentView === "subscription" && <SubscriptionView />}
                {currentView === "team" && <TeamView />}
                {currentView === "profile" && <ProfileView />}
              </React.Suspense>
            </div>
          </main>
          <Footer />
        </div>
      </div>
    </div>
  );
}
