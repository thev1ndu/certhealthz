import { cn } from "@/lib/utils";

// Titled, corner-tick-framed content box — the "grid box" look from the
// certificate detail page, reused wherever a page groups related fields or
// controls under a label (History, CT Check, Settings, ...).
export default function Section({ title, headerRight, className, children }) {
  return (
    <div className={cn("corner-ticks relative border border-border bg-card/40 p-4", className)}>
      <div className="mb-3 flex items-center justify-between gap-3">
        <h2 className="text-[11px] font-medium tracking-widest text-muted-foreground uppercase">
          {title}
        </h2>
        {headerRight}
      </div>
      {children}
    </div>
  );
}
