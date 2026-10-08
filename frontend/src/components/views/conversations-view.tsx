"use client";

import * as React from "react";
import {
  Send,
  Sparkles,
  Hand,
  ChevronLeft,
  Search,
  Phone,
  MoreVertical,
} from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";
import {
  conversations as allConversations,
  type Conversation,
  type Message,
} from "@/lib/mock-data";
import { formatTime, timeAgo, initials } from "@/lib/format";
import { ViewHeader } from "./view-header";

function statusBadge(c: Conversation) {
  if (c.status === "ai")
    return (
      <Badge className="bg-brand/10 text-brand hover:bg-brand/20">
        <Sparkles className="size-3" /> IA
      </Badge>
    );
  if (c.status === "human")
    return (
      <Badge className="bg-amber-accent/15 text-amber-accent hover:bg-amber-accent/25">
        <Hand className="size-3" /> Humain
      </Badge>
    );
  return <Badge variant="outline">Clôturée</Badge>;
}

export function ConversationsView() {
  const [list, setList] = React.useState<Conversation[]>(allConversations);
  const [selectedId, setSelectedId] = React.useState<string | null>(
    allConversations[0]?.id ?? null,
  );
  const [filterTakeover, setFilterTakeover] = React.useState(false);
  const [query, setQuery] = React.useState("");
  const [draft, setDraft] = React.useState("");
  // True = la main est au commerçant, false = rendue à NOVA
  const [humanMode, setHumanMode] = React.useState(false);

  const selected = list.find((c) => c.id === selectedId) || null;

  const filtered = list.filter((c) => {
    if (filterTakeover && !c.needsTakeover) return false;
    if (query && !c.customerName.toLowerCase().includes(query.toLowerCase()))
      return false;
    return true;
  });

  function selectConversation(id: string) {
    setSelectedId(id);
    setList((prev) =>
      prev.map((c) => (c.id === id ? { ...c, unread: 0 } : c)),
    );
    const conv = list.find((c) => c.id === id);
    setHumanMode(conv?.status === "human");
  }

  function sendMessage() {
    if (!selected || !draft.trim()) return;
    const msg: Message = {
      id: `m_${Date.now()}`,
      from: "merchant",
      text: draft.trim(),
      at: new Date().toISOString(),
    };
    setList((prev) =>
      prev.map((c) =>
        c.id === selected.id
          ? {
              ...c,
              messages: [...c.messages, msg],
              lastMessage: msg.text,
              lastAt: msg.at,
            }
          : c,
      ),
    );
    setDraft("");
  }

  function toggleTakeover() {
    if (!selected) return;
    const next = !humanMode;
    setHumanMode(next);
    setList((prev) =>
      prev.map((c) =>
        c.id === selected.id
          ? {
              ...c,
              status: next ? "human" : "ai",
              needsTakeover: next ? false : c.needsTakeover,
            }
          : c,
      ),
    );
    toast.success(
      next
        ? `Vous avez pris la main sur ${selected.customerName}`
        : `Conversation rendue à NOVA`,
    );
  }

  return (
    <div className="flex h-[calc(100vh-220px)] min-h-[520px] flex-col gap-3">
      <ViewHeader
        title="Conversations"
        description="Tous les échanges WhatsApp — gérés par NOVA ou repris par vos soins."
        actions={
          <Button
            variant={filterTakeover ? "default" : "outline"}
            size="sm"
            onClick={() => setFilterTakeover((v) => !v)}
          >
            <Hand className="size-4" />
            À reprendre ({list.filter((c) => c.needsTakeover).length})
          </Button>
        }
      />

      <div className="grid flex-1 grid-cols-1 overflow-hidden rounded-xl border bg-card md:grid-cols-[320px_1fr]">
        {/* Liste */}
        <div
          className={cn(
            "flex flex-col border-r",
            selected && "hidden md:flex",
          )}
        >
          <div className="border-b p-2.5">
            <div className="relative">
              <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                placeholder="Rechercher un client…"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                className="h-9 pl-9"
              />
            </div>
          </div>
          <ScrollArea className="flex-1">
            <div className="divide-y">
              {filtered.length === 0 ? (
                <div className="p-6 text-center text-sm text-muted-foreground">
                  Aucune conversation.
                </div>
              ) : (
                filtered.map((c) => {
                  const active = c.id === selectedId;
                  return (
                    <button
                      key={c.id}
                      onClick={() => selectConversation(c.id)}
                      className={cn(
                        "flex w-full items-start gap-2.5 p-3 text-left transition-colors hover:bg-accent",
                        active && "bg-accent",
                      )}
                    >
                      <Avatar className="size-9 shrink-0">
                        <AvatarFallback
                          className={cn("text-xs font-semibold text-white", c.avatarColor)}
                        >
                          {initials(c.customerName)}
                        </AvatarFallback>
                      </Avatar>
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center justify-between gap-2">
                          <span className="truncate text-sm font-medium">
                            {c.customerName}
                          </span>
                          <span className="shrink-0 text-[10px] text-muted-foreground">
                            {timeAgo(c.lastAt)}
                          </span>
                        </div>
                        <div className="line-clamp-1 text-xs text-muted-foreground">
                          {c.lastMessage}
                        </div>
                        <div className="mt-1 flex items-center gap-1.5">
                          {statusBadge(c)}
                          {c.unread > 0 && (
                            <Badge className="bg-brand text-brand-foreground">
                              {c.unread}
                            </Badge>
                          )}
                        </div>
                      </div>
                    </button>
                  );
                })
              )}
            </div>
          </ScrollArea>
        </div>

        {/* Thread */}
        {selected ? (
          <div className="flex flex-col">
            {/* Header thread */}
            <div className="flex items-center justify-between gap-2 border-b p-3">
              <div className="flex min-w-0 items-center gap-2.5">
                <Button
                  variant="ghost"
                  size="icon"
                  className="size-8 md:hidden"
                  onClick={() => setSelectedId(null)}
                  aria-label="Retour à la liste"
                >
                  <ChevronLeft className="size-4" />
                </Button>
                <Avatar className="size-9">
                  <AvatarFallback
                    className={cn(
                      "text-xs font-semibold text-white",
                      selected.avatarColor,
                    )}
                  >
                    {initials(selected.customerName)}
                  </AvatarFallback>
                </Avatar>
                <div className="min-w-0">
                  <div className="truncate text-sm font-semibold">
                    {selected.customerName}
                  </div>
                  <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                    <Phone className="size-3" />
                    {selected.customerPhone}
                  </div>
                </div>
              </div>
              <div className="flex items-center gap-1.5">
                <Button
                  size="sm"
                  variant={humanMode ? "default" : "outline"}
                  onClick={toggleTakeover}
                  className="gap-1.5"
                >
                  {humanMode ? (
                    <>
                      <Sparkles className="size-3.5" /> Rendre à NOVA
                    </>
                  ) : (
                    <>
                      <Hand className="size-3.5" /> Prendre la main
                    </>
                  )}
                </Button>
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="size-8"
                      aria-label="Plus d'actions"
                    >
                      <MoreVertical className="size-4" />
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end">
                    <DropdownMenuItem onClick={() => toast.info("Marquer comme lue")}>
                      Marquer comme lue
                    </DropdownMenuItem>
                    <DropdownMenuItem onClick={() => toast.info("Clôturer")}>
                      Clôturer la conversation
                    </DropdownMenuItem>
                    <DropdownMenuItem
                      className="text-destructive focus:text-destructive"
                      onClick={() => toast.error("Bloquer le client ?")}
                    >
                      Bloquer le client
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              </div>
            </div>

            {/* Messages */}
            <ScrollArea className="flex-1 bg-muted/20">
              <div className="flex flex-col gap-2 p-3">
                {selected.messages.map((m) => (
                  <MessageBubble key={m.id} message={m} customerName={selected.customerName} />
                ))}
              </div>
            </ScrollArea>

            {/* Composer */}
            <div className="border-t p-2.5">
              {humanMode ? (
                <form
                  onSubmit={(e) => {
                    e.preventDefault();
                    sendMessage();
                  }}
                  className="flex items-center gap-2"
                >
                  <Input
                    placeholder="Écrivez votre message…"
                    value={draft}
                    onChange={(e) => setDraft(e.target.value)}
                  />
                  <Button type="submit" size="icon" aria-label="Envoyer">
                    <Send className="size-4" />
                  </Button>
                </form>
              ) : (
                <div className="flex items-center gap-2 rounded-lg border border-dashed bg-muted/30 p-2.5 text-xs text-muted-foreground">
                  <Sparkles className="size-4 text-brand" />
                  NOVA gère cette conversation. Cliquez sur « Prendre la main »
                  pour répondre vous-même.
                </div>
              )}
            </div>
          </div>
        ) : (
          <div className="hidden flex-1 items-center justify-center text-sm text-muted-foreground md:flex">
            Sélectionnez une conversation.
          </div>
        )}
      </div>
    </div>
  );
}

function MessageBubble({
  message,
  customerName,
}: {
  message: Message;
  customerName: string;
}) {
  const inbound = message.from === "customer";
  const ai = message.from === "ai";
  const merchant = message.from === "merchant";

  return (
    <div
      className={cn(
        "flex w-full",
        inbound ? "justify-start" : "justify-end",
      )}
    >
      <div
        className={cn(
          "max-w-[78%] rounded-2xl px-3 py-2 text-sm shadow-sm",
          inbound && "rounded-tl-sm bg-muted text-foreground",
          ai && "rounded-tr-sm bg-brand text-brand-foreground",
          merchant && "rounded-tr-sm bg-brand text-brand-foreground",
        )}
      >
        {inbound && (
          <div className="mb-0.5 text-[10px] font-semibold text-muted-foreground">
            {customerName}
          </div>
        )}
        {ai && (
          <div className="mb-0.5 flex items-center gap-1 text-[10px] font-semibold text-brand-foreground/80">
            <Sparkles className="size-2.5" /> NOVA
          </div>
        )}
        {merchant && (
          <div className="mb-0.5 text-[10px] font-semibold text-brand-foreground/80">
            Vous
          </div>
        )}
        <div className="whitespace-pre-wrap break-words">{message.text}</div>
        <div
          className={cn(
            "mt-1 text-[10px]",
            inbound ? "text-muted-foreground" : "text-brand-foreground/70",
          )}
        >
          {formatTime(message.at)}
        </div>
      </div>
    </div>
  );
}
