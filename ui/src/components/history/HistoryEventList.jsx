import StatusBadge from "@/components/StatusBadge";

const KIND_LABEL = {
  new: "New",
  removed: "Removed",
  "status-change": "Status change",
  "expiry-shift": "Expiry shift",
};

const KIND_COLOR = {
  new: "text-status-ok",
  removed: "text-muted-foreground",
  "status-change": "text-status-warning",
  "expiry-shift": "text-status-warning",
};

function formatAt(iso) {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}

function StateChip({ status, days }) {
  if (!status) return <span className="text-muted-foreground">—</span>;
  return (
    <StatusBadge status={status}>{days != null ? `${days}d` : undefined}</StatusBadge>
  );
}

export default function HistoryEventList({ events }) {
  return (
    <div className="-mx-4 -mb-4">
      {events.map((e, i) => (
        <div
          key={`${e.at}-${e.source}-${e.cluster}-${e.namespace}-${e.name}-${i}`}
          className="flex flex-col gap-2 border-t border-border/60 px-4 py-3 first:border-t-0 sm:flex-row sm:items-center sm:justify-between"
        >
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <span className={`text-xs font-medium tracking-wide uppercase ${KIND_COLOR[e.kind] ?? "text-muted-foreground"}`}>
                {KIND_LABEL[e.kind] ?? e.kind}
              </span>
              <span className="truncate text-sm font-medium">{e.name}</span>
            </div>
            <div className="mt-0.5 truncate text-xs text-muted-foreground">
              {e.source}
              {e.cluster !== "-" ? ` · ${e.cluster}` : ""}
              {e.namespace !== "-" ? ` · ${e.namespace}` : ""}
              {" · "}
              {formatAt(e.at)}
            </div>
          </div>

          <div className="flex shrink-0 items-center gap-2 text-sm">
            <StateChip status={e.fromState} days={e.fromDays} />
            {e.fromState && e.toState && <span className="text-muted-foreground">→</span>}
            <StateChip status={e.toState} days={e.toDays} />
          </div>
        </div>
      ))}
    </div>
  );
}
