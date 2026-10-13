/**
 * NOVA — Logo SVG.
 * Un "N" stylisé avec une étincelle IA, en emerald. Variante pour
 * les fonds sombres (sidebar) et clairs (login, header).
 */

import { cn } from "@/lib/utils";

interface NovaLogoProps {
  className?: string;
  size?: number;
  withWordmark?: boolean;
  /** Force la couleur du logo (par défaut emerald du thème) */
  variant?: "default" | "white";
}

export function NovaLogo({
  className,
  size = 32,
  withWordmark = false,
  variant = "default",
}: NovaLogoProps) {
  const main = variant === "white" ? "#ffffff" : "var(--brand, #0e8a5f)";
  const accent = "var(--amber-accent, #f59e0b)";

  return (
    <div className={cn("flex items-center gap-2.5", className)}>
      <svg
        width={size}
        height={size}
        viewBox="0 0 48 48"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
        aria-hidden="true"
        role="img"
        aria-label="Logo NOVA"
      >
        {/* Rounded square background */}
        <rect width="48" height="48" rx="12" fill={main} />
        {/* "N" stylisé */}
        <path
          d="M14 34V14h4.4l11.6 13.4V14H35v20h-4.4L19 20.6V34H14Z"
          fill="white"
        />
        {/* Étincelle IA (amber) */}
        <path
          d="M34.5 6.5c.4 1.7 1.6 2.9 3.3 3.3-1.7.4-2.9 1.6-3.3 3.3-.4-1.7-1.6-2.9-3.3-3.3 1.7-.4 2.9-1.6 3.3-3.3Z"
          fill={accent}
        />
        <circle cx="11" cy="11" r="1.6" fill={accent} opacity="0.7" />
      </svg>
      {withWordmark && (
        <div className="flex flex-col leading-none">
          <span
            className={cn(
              "text-lg font-bold tracking-tight",
              variant === "white" ? "text-white" : "text-foreground",
            )}
          >
            NOVA
          </span>
          <span
            className={cn(
              "text-[10px] font-medium uppercase tracking-wider",
              variant === "white" ? "text-white/70" : "text-muted-foreground",
            )}
          >
            Commerce IA
          </span>
        </div>
      )}
    </div>
  );
}
