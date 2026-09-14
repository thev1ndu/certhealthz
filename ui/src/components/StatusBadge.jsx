import { cn } from "@/lib/utils";

// Semantic status → the CSS custom-property pair it renders with. Kept as
// inline styles (not Tailwind arbitrary classes) so the dot and text always
// share the exact same color token, including the "drift" cop-out.
const STATUS_COLOR = {
  ok: "var(--status-ok)",
  expiring: "var(--status-warning)",
  expired: "var(--status-error)",
  error: "var(--status-neutral)",
  drift: "var(--status-error)",
  "broken-chain": "var(--status-error)",
  "weak-crypto": "var(--status-warning)",
};

export const STATUS_LABEL = {
  ok: "Healthy",
  expiring: "Expiring",
  expired: "Expired",
  error: "Error",
  drift: "Drift",
  "broken-chain": "Broken chain",
  "weak-crypto": "Weak crypto",
};

export default function StatusBadge({ status, className, children }) {
  const color = STATUS_COLOR[status] ?? STATUS_COLOR.error;
  return (
    <span
      className={cn(
        "inline-flex h-5 w-fit shrink-0 items-center gap-1.5 rounded-md border border-transparent px-2 text-xs font-medium whitespace-nowrap",
        className,
      )}
      style={{ backgroundColor: `color-mix(in oklch, ${color} 16%, transparent)`, color }}
    >
      <span className="size-1.5 shrink-0 rounded-full" style={{ backgroundColor: color }} />
      {children != null ? (
        <>
          {children} {STATUS_LABEL[status]?.toLowerCase() ?? status}
        </>
      ) : (
        STATUS_LABEL[status] ?? status
      )}
    </span>
  );
}
