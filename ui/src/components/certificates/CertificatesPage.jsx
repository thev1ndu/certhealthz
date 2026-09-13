import { useMemo, useState } from "react";
import CertTable from "@/components/CertTable";
import CertificatesToolbar from "@/components/certificates/CertificatesToolbar";
import StatusBadge from "@/components/StatusBadge";
import NotConnected from "@/components/layout/NotConnected";
import PageHeading from "@/components/layout/PageHeading";
import { Card, CardFooter, CardHeader } from "@/components/ui/card";
import { useAddCluster } from "@/hooks/useAddCluster";
import { useAddEndpoint } from "@/hooks/useAddEndpoint";
import { useClusterList } from "@/hooks/useClusterList";
import { STATUS_ORDER } from "@/lib/statuses";

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

export default function CertificatesPage({ certs, isLive, reloadCerts }) {
  const [query, setQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState(() => new Set());
  const [clusterFilter, setClusterFilter] = useState(() => new Set());

  const { serverClusters, reload: reloadClusters } = useClusterList(isLive);

  function refreshAfterAdd() {
    return Promise.all([reloadCerts(), reloadClusters()]);
  }

  const addCluster = useAddCluster(refreshAfterAdd);
  const addEndpoint = useAddEndpoint(reloadCerts);

  const rows = useMemo(
    () => certs.filter((r) => matchesSearch(r, query) && matchesFilters(r, statusFilter, clusterFilter)),
    [certs, query, statusFilter, clusterFilter],
  );

  const summary = useMemo(() => {
    const counts = {};
    for (const row of certs) counts[row.status] = (counts[row.status] ?? 0) + 1;
    return STATUS_ORDER.filter((s) => counts[s]).map((s) => [s, counts[s]]);
  }, [certs]);

  const clusters = useMemo(() => {
    // Union, not override: /api/clusters only knows explicit --kubeconfig
    // entries and uploads, not the implicit "default" cluster a bare scan
    // falls back to — that one only shows up in the cert rows themselves.
    const fromRows = certs.map((r) => r.cluster).filter((c) => c !== "-");
    return [...new Set([...(serverClusters ?? []), ...fromRows])].sort();
  }, [certs, serverClusters]);

  return (
    <div>
      <PageHeading
        title="Certificates"
        description="Live TLS certificate health across clusters and endpoints"
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
        addEndpoint={addEndpoint}
        onSourcesChanged={refreshAfterAdd}
      />

      <Card size="sm" className="gap-0 p-0">
        <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-2 border-b border-border px-4 py-3">
          <span className="font-mono text-xs text-muted-foreground">
            {certs.length} certificates tracked across {clusters.length} clusters and live
            endpoints
          </span>
          <div className="flex flex-wrap items-center gap-1.5">
            {summary.map(([status, count]) => (
              <StatusBadge key={status} status={status} className="gap-1">
                {count}
              </StatusBadge>
            ))}
          </div>
        </CardHeader>

        <CertTable rows={rows} />

        <CardFooter className="border-t border-border px-4 py-3">
          <span className="font-mono text-xs text-muted-foreground">
            Showing {rows.length === 0 ? 0 : 1}–{rows.length} of {certs.length}
          </span>
        </CardFooter>
      </Card>
    </div>
  );
}
