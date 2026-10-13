"use client";
import * as React from "react";
import { MessageCircle } from "lucide-react";
import { Card } from "@/components/ui/card";
import { useAuthStore } from "@/stores/auth-store";
import { mockConversations } from "@/lib/mock-data";

export function ConversationsView() {
  const isDemo = useAuthStore((s) => s.isDemo);
  if (isDemo && mockConversations?.length > 0) {
    return (
      <div className="space-y-4">
        <h1 className="text-2xl font-bold">Conversations</h1>
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
          {mockConversations.slice(0, 6).map((c: any, i: number) => (
            <Card key={i} className="p-4">
              <div className="flex items-center justify-between">
                <div className="font-medium">{c.customerName || c.name || "Client"}</div>
                <span className="text-xs text-muted-foreground">{c.time || ""}</span>
              </div>
              <p className="mt-1 text-sm text-muted-foreground truncate">{c.lastMessage || c.preview || ""}</p>
            </Card>
          ))}
        </div>
      </div>
    );
  }
  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-bold">Conversations</h1>
      <Card className="p-8 text-center">
        <MessageCircle className="mx-auto mb-4 size-12 text-muted-foreground" />
        <p className="text-muted-foreground">Aucune conversation pour le moment.</p>
      </Card>
    </div>
  );
}
