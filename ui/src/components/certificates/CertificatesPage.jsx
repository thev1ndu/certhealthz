import { useMemo, useState } from "react";
import CertTable, { useVisibleColumns } from "@/components/CertTable";
import CertDetailPage from "@/components/certificates/CertDetailPage";
import CertificatesToolbar from "@/components/certificates/CertificatesToolbar";
import StatusBadge from "@/components/StatusBadge";
import NotConnected from "@/components/layout/NotConnected";
import PageHeading from "@/components/layout/PageHeading";
import Section from "@/components/layout/Section";
import { useAddCluster } from "@/hooks/useAddCluster";
import { useKnownClusters } from "@/hooks/useKnownClusters";
import { summarizeStatuses } from "@/lib/statuses";

function matchesSearch(row, query) {
  if (!query) return true;
  const q = query.toLowerCase();
  return (
    row.name.toLowerCase().includes(q) ||
    row.cluster.toLowerCase().includes(q) ||
    row.namespace.toLowerCase().includes(q) ||
    row.detail.toLowerCase().includes(q)
  );
}

// Empty set = no filter applied (show every status/cluster).
function matchesFilters(row, statusFilter, clusterFilter) {
  if (statusFilter.size > 0 && !statusFilter.has(row.status)) return false;
  if (clusterFilter.size > 0 && !clusterFilter.has(row.cluster)) return false;
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
  a.download = "certhealthz-certs.json";
  a.click();
  URL.revokeObjectURL(url);
}

export default function CertificatesPage({ certs, isLive, reloadCerts, initialCertId = null }) {
  const [query, setQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState(() => new Set());
  const [clusterFilter, setClusterFilter] = useState(() => new Set());
  const [selectedCertId, setSelectedCertId] = useState(initialCertId);
  const { visible: visibleColumns, toggle: toggleColumn } = useVisibleColumns();

  const clusterCerts = useMemo(() => certs.filter((r) => r.source !== "endpoint"), [certs]);

  const { clusters, reload: reloadClusters } = useKnownClusters(clusterCerts, isLive);

  function refreshAfterAdd() {
    return Promise.all([reloadCerts(), reloadClusters()]);
  }

  const addCluster = useAddCluster(refreshAfterAdd);

  const rows = useMemo(
    () =>
      clusterCerts.filter((r) => matchesSearch(r, query) && matchesFilters(r, statusFilter, clusterFilter)),
    [clusterCerts, query, statusFilter, clusterFilter],
  );

  const summary = useMemo(() => summarizeStatuses(clusterCerts), [clusterCerts]);

  if (selectedCertId) {
    const selectedRow = certs.find((r) => r.id === selectedCertId);
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
        title="Clusters"
        description="Certificate health for every cert-manager Certificate, raw Secret, Ingress, and Gateway API route across your clusters"
      />

      {!isLive && (
        <div className="mb-4">
          <NotConnected />
        </div>
      )}

      <CertificatesToolbar
        query={query}
        onQueryChange={setQuery}
        statusFilter={statusFilter}
        clusterFilter={clusterFilter}
        onToggleStatus={(status) => setStatusFilter((prev) => toggleInSet(prev, status))}
        onToggleCluster={(cluster) => setClusterFilter((prev) => toggleInSet(prev, cluster))}
        onClearFilters={() => {
          setStatusFilter(new Set());
          setClusterFilter(new Set());
        }}
        clusters={clusters}
        onExport={() => exportJson(rows)}
        isLive={isLive}
        addCluster={addCluster}
        onSourcesChanged={refreshAfterAdd}
        visibleColumns={visibleColumns}
        onToggleColumn={toggleColumn}
      />

      <Section title="Clusters">
        <div className="-mx-4 -mt-1">
          <div className="flex flex-row flex-wrap items-center justify-between gap-2 border-y border-border px-4 py-3">
            <span className="text-xs text-muted-foreground">
              {clusterCerts.length} certificates tracked across {clusters.length} clusters
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
              Showing {rows.length === 0 ? 0 : 1}–{rows.length} of {clusterCerts.length}
            </span>
          </div>
        </div>
      </Section>
    </div>
  );
}
