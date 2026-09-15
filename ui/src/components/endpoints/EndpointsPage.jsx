import { useMemo, useState } from "react";
import CertTable, { useVisibleColumns } from "@/components/CertTable";
import CertDetailPage from "@/components/certificates/CertDetailPage";
import EndpointsToolbar from "@/components/endpoints/EndpointsToolbar";
import StatusBadge from "@/components/StatusBadge";
import NotConnected from "@/components/layout/NotConnected";
import PageHeading from "@/components/layout/PageHeading";
import Section from "@/components/layout/Section";
import { useAddEndpoint } from "@/hooks/useAddEndpoint";
import { summarizeStatuses } from "@/lib/statuses";

function matchesSearch(row, query) {
  if (!query) return true;
  const q = query.toLowerCase();
  return row.name.toLowerCase().includes(q) || row.detail.toLowerCase().includes(q);
}

function matchesFilters(row, statusFilter) {
  if (statusFilter.size > 0 && !statusFilter.has(row.status)) return false;
  return true;
}

function toggleInSet(set, value) {
  const next = new Set(set);
  if (next.has(value)) next.delete(value);
  else next.add(value);
  return next;
}

function exportJson(rows) {
  const blob = new Blob([JSON.stringify(rows, null, 2)], { type: "application/json" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = "certhealthz-endpoints.json";
  a.click();
  URL.revokeObjectURL(url);
}

export default function EndpointsPage({ certs, isLive, reloadCerts, initialCertId = null }) {
  const [query, setQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState(() => new Set());
  const [selectedCertId, setSelectedCertId] = useState(initialCertId);
  const { visible: visibleColumns, toggle: toggleColumn } = useVisibleColumns();

  const addEndpoint = useAddEndpoint(reloadCerts);

  const endpoints = useMemo(() => certs.filter((r) => r.source === "endpoint"), [certs]);

  const rows = useMemo(
    () => endpoints.filter((r) => matchesSearch(r, query) && matchesFilters(r, statusFilter)),
    [endpoints, query, statusFilter],
  );

  const summary = useMemo(() => summarizeStatuses(endpoints), [endpoints]);

  if (selectedCertId) {
    const selectedRow = endpoints.find((r) => r.id === selectedCertId);
    return (
      <CertDetailPage
        id={selectedCertId}
        row={selectedRow}
        onBack={() => setSelectedCertId(null)}
      />
    );
  }

  return (
    <div>
      <PageHeading
        title="Endpoints"
        description="Live TLS endpoint probes, independent of any cluster"
      />

      {!isLive && (
        <div className="mb-4">
          <NotConnected />
        </div>
      )}

      <EndpointsToolbar
        query={query}
        onQueryChange={setQuery}
        statusFilter={statusFilter}
        onToggleStatus={(status) => setStatusFilter((prev) => toggleInSet(prev, status))}
        onClearFilters={() => setStatusFilter(new Set())}
        onExport={() => exportJson(rows)}
        isLive={isLive}
        addEndpoint={addEndpoint}
        onSourcesChanged={reloadCerts}
        visibleColumns={visibleColumns}
        onToggleColumn={toggleColumn}
      />

      <Section title="Endpoints">
        <div className="-mx-4 -mt-1">
          <div className="flex flex-row flex-wrap items-center justify-between gap-2 border-y border-border px-4 py-3">
            <span className="text-xs text-muted-foreground">
              {endpoints.length} probed endpoints
            </span>
            <div className="flex flex-wrap items-center gap-1.5">
              {summary.map(([status, count]) => (
                <StatusBadge key={status} status={status} className="gap-1">
                  {count}
                </StatusBadge>
              ))}
            </div>
          </div>

          <CertTable rows={rows} onRowClick={setSelectedCertId} visible={visibleColumns} />

          <div className="-mb-4 border-t border-border px-4 py-3">
            <span className="text-xs text-muted-foreground">
              Showing {rows.length === 0 ? 0 : 1}–{rows.length} of {endpoints.length}
            </span>
          </div>
        </div>
      </Section>
    </div>
  );
}
