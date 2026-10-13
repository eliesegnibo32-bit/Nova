/**
 * Helpers de formatage NOVA (langue française, devise FCFA).
 */

/** Formate un montant en FCFA avec séparateur de milliers (espace insécable). */
export function formatFCFA(n: number): string {
  const rounded = Math.round(n);
  const withSpaces = rounded
    .toString()
    .replace(/\B(?=(\d{3})+(?!\d))/g, "\u00A0");
  return `${withSpaces}\u00A0FCFA`;
}

/** Formate un nombre avec séparateur de milliers (sans devise). */
export function formatNumber(n: number): string {
  return n.toString().replace(/\B(?=(\d{3})+(?!\d))/g, "\u00A0");
}

/** Renvoie les initiales (max 2 lettres) d'un nom complet. */
export function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  if (parts.length === 0) return "?";
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
  return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase();
}

/** Formate une date ISO en "12 oct. 2026" (français). */
export function formatDate(iso: string | Date): string {
  const d = typeof iso === "string" ? new Date(iso) : iso;
  if (isNaN(d.getTime())) return "—";
  return d.toLocaleDateString("fr-FR", {
    day: "2-digit",
    month: "short",
    year: "numeric",
  });
}

/** Formate une date ISO en "12 oct. 2026, 14:32". */
export function formatDateTime(iso: string | Date): string {
  const d = typeof iso === "string" ? new Date(iso) : iso;
  if (isNaN(d.getTime())) return "—";
  return d.toLocaleDateString("fr-FR", {
    day: "2-digit",
    month: "short",
    hour: "2-digit",
    minute: "2-digit",
  });
}

/** Heure courte "14:32". */
export function formatTime(iso: string | Date): string {
  const d = typeof iso === "string" ? new Date(iso) : iso;
  if (isNaN(d.getTime())) return "—";
  return d.toLocaleTimeString("fr-FR", { hour: "2-digit", minute: "2-digit" });
}

/** Renvoie une chaîne relative courte "il y a 5 min", "hier", etc. */
export function timeAgo(iso: string | Date): string {
  const d = typeof iso === "string" ? new Date(iso) : iso;
  if (isNaN(d.getTime())) return "—";
  const diffMs = Date.now() - d.getTime();
  const sec = Math.floor(diffMs / 1000);
  if (sec < 60) return "à l'instant";
  const min = Math.floor(sec / 60);
  if (min < 60) return `il y a ${min} min`;
  const h = Math.floor(min / 60);
  if (h < 24) return `il y a ${h} h`;
  const days = Math.floor(h / 24);
  if (days === 1) return "hier";
  if (days < 7) return `il y a ${days} j`;
  return formatDate(d);
}
