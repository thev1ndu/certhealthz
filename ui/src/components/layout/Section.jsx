import { cn } from "@/lib/utils";

// Titled, corner-tick-framed content box — the "grid box" look from the
// certificate detail page, reused wherever a page groups related fields or
// controls under a label (History, CT Check, Settings, ...).
export default function Section({ title, className, children }) {
  return (
    <div className={cn("corner-ticks relative border border-border bg-card/40 p-4", className)}>
      <h2 className="mb-3 text-[11px] font-medium tracking-widest text-muted-foreground uppercase">
        {title}
      </h2>
      {children}
    </div>
  );
}
