"use client";

import { NovaLogo } from "@/components/ui/nova-logo";

export function Footer() {
  return (
    <footer
      className="mt-auto flex flex-col items-center justify-between gap-2 border-t bg-background px-4 py-3 text-xs text-muted-foreground sm:flex-row sm:px-6"
      role="contentinfo"
    >
      <div className="flex items-center gap-2">
        <NovaLogo size={18} />
        <span>
          NOVA <span className="text-foreground/70">v1.0</span> — © 2026
        </span>
      </div>
      <div className="flex items-center gap-3">
        <span className="inline-flex items-center gap-1.5">
          <span className="size-1.5 animate-pulse rounded-full bg-emerald-500" />
          API connectée
        </span>
        <span className="hidden sm:inline">·</span>
        <span className="hidden sm:inline">
          Fait avec ❤️ à Abidjan pour les commerçants de Côte d'Ivoire
        </span>
      </div>
    </footer>
  );
}
