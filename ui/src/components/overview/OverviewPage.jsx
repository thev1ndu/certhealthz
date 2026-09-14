import { useMemo } from "react";
import {
  ArrowRightIcon,
  CertificateIcon,
  ClockCounterClockwiseIcon,
  ShieldCheckIcon,
} from "@phosphor-icons/react";
import StatusBadge from "@/components/StatusBadge";
import NotConnected from "@/components/layout/NotConnected";
import PageHeading from "@/components/layout/PageHeading";
import Section from "@/components/layout/Section";
import { useKnownClusters } from "@/hooks/useKnownClusters";
import { ATTENTION_STATUSES, summarizeStatuses } from "@/lib/statuses";

function Tile({ label, value, sub }) {
  return (
    <div className="corner-ticks relative border border-border bg-card/40 p-4">
      <div className="text-[11px] tracking-widest text-muted-foreground uppercase">{label}</div>
      <div className="mt-2 font-heading text-3xl font-medium tracking-tight">{value}</div>
      {sub && <div className="mt-1 text-xs text-muted-foreground">{sub}</div>}
    </div>
  );
}

function NavCard({ icon: Icon, title, description, onClick }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="corner-ticks relative flex flex-col items-start gap-2 border border-border bg-card/40 p-4 text-left transition-colors hover:bg-card"
    >
      <div className="flex w-full items-center justify-between">
        <Icon size={18} className="text-muted-foreground" />
        <ArrowRightIcon size={14} className="text-muted-foreground" />
      </div>
      <div className="text-sm font-medium">{title}</div>
      <div className="text-xs text-muted-foreground">{description}</div>
    </button>
  );
}

function daysSortKey(row) {
  return row.days === null || row.days === undefined ? Number.POSITIVE_INFINITY : row.days;
}

export default function OverviewPage({ certs, isLive, onNavigate }) {
  const { clusters } = useKnownClusters(certs, isLive);

  const endpointCount = useMemo(
    () => new Set(certs.filter((r) => r.source === "endpoint").map((r) => r.name)).size,
    [certs],
  );

  const summary = useMemo(() => summarizeStatuses(certs), [certs]);

  const attention = useMemo(
    () =>
      certs
        .filter((r) => ATTENTION_STATUSES.has(r.status))
        .sort((a, b) => daysSortKey(a) - daysSortKey(b))
        .slice(0, 6),
    [certs],
  );

  return (
    <div>
      <PageHeading
        title="Overview"
        description="Snapshot of certificate health across every connected cluster and endpoint"
      />

      {!isLive ? (
        <NotConnected />
      ) : (
        <div className="flex flex-col gap-4">
          <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
            <Tile label="Certificates" value={certs.length} sub="tracked in this scan" />
            <Tile label="Clusters" value={clusters.length} sub="connected" />
            <Tile label="Endpoints" value={endpointCount} sub="probed live" />
            <Tile
              label="Needs attention"
              value={attention.length}
              sub={attention.length === 0 ? "all clear" : "expiring, expired, or error"}
            />
          </div>

          <Section title="Status breakdown">
            {summary.length === 0 ? (
              <p className="text-sm text-muted-foreground">No certificates scanned yet.</p>
            ) : (
              <div className="flex flex-wrap items-center gap-2">
                {summary.map(([status, count]) => (
                  <StatusBadge key={status} status={status} className="gap-1.5 px-2.5 py-1 text-sm">
                    {count}
                  </StatusBadge>
                ))}
              </div>
            )}
          </Section>

          <Section title="Needs attention">
            {attention.length === 0 ? (
              <p className="text-sm text-muted-foreground">
                Every tracked certificate is healthy.
              </p>
            ) : (
              <div className="-mx-4 -mb-4">
                {attention.map((row) => (
                  <button
                    key={row.id}
                    type="button"
                    onClick={() => onNavigate("certificates", row.id)}
                    className="flex w-full items-center justify-between gap-3 border-t border-border/60 px-4 py-2.5 text-left first:border-t-0 hover:bg-accent/40"
                  >
                    <div className="min-w-0">
                      <div className="truncate text-sm font-medium">{row.name}</div>
                      <div className="truncate text-xs text-muted-foreground">
                        {row.source}
                        {row.cluster !== "-" ? ` · ${row.cluster}` : ""}
                        {row.namespace !== "-" ? ` · ${row.namespace}` : ""}
                      </div>
                    </div>
                    <div className="flex shrink-0 items-center gap-3">
                      <span className="text-sm tabular-nums text-muted-foreground">
                        {row.days === null ? "—" : `${row.days}d`}
                      </span>
                      <StatusBadge status={row.status} />
                    </div>
                  </button>
                ))}
              </div>
            )}
            {attention.length > 0 && (
              <button
                type="button"
                onClick={() => onNavigate("certificates")}
                className="mt-3 inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
              >
                View all certificates
                <ArrowRightIcon size={12} />
              </button>
            )}
          </Section>

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
            <NavCard
              icon={CertificateIcon}
              title="Certificates"
              description="Browse every scanned certificate"
              onClick={() => onNavigate("certificates")}
            />
            <NavCard
              icon={ClockCounterClockwiseIcon}
              title="History"
              description="Browse the automatically recorded audit log"
              onClick={() => onNavigate("history")}
            />
            <NavCard
              icon={ShieldCheckIcon}
              title="CT Check"
              description="Search Certificate Transparency logs"
              onClick={() => onNavigate("ct")}
            />
          </div>
        </div>
      )}
    </div>
  );
}
