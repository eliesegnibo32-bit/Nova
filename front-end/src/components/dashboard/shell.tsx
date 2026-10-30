"use client";

import * as React from "react";
import { MobileSidebar, Sidebar } from "./sidebar";
import { Header } from "./header";
import { Footer } from "./footer";
import { ViewErrorBoundary } from "./view-error-boundary";
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

  const renderView = () => {
    if (currentView === "home") return <HomeView />;
    if (currentView === "products") return <ProductsView />;
    if (currentView === "stock") return <StockView />;
    if (currentView === "orders") return <OrdersView />;
    if (currentView === "conversations") return <ConversationsView />;
    if (currentView === "customers") return <CustomersView />;
    if (currentView === "payment") return <PaymentConfigView />;
    if (currentView === "stats") return <StatsView />;
    if (currentView === "subscription") return <SubscriptionView />;
    if (currentView === "team") return <TeamView />;
    if (currentView === "profile") return <ProfileView />;
    return null;
  };

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
                <ViewErrorBoundary viewName={currentView}>
                  {renderView()}
                </ViewErrorBoundary>
              </React.Suspense>
            </div>
          </main>
          <Footer />
        </div>
      </div>
    </div>
  );
}
