import { useEffect, useMemo, useState } from "react";
import {
  Badge,
  Button,
  DropdownMenu,
  InputGroup,
  LayerCard,
  Popover,
  Text,
  Toolbar,
  Tooltip,
  TooltipProvider,
} from "@cloudflare/kumo";
import {
  CloudArrowUpIcon,
  DownloadSimpleIcon,
  FileXIcon,
  FunnelIcon,
  GearSixIcon,
  MagnifyingGlassIcon,
  PlusIcon,
  XIcon,
} from "@phosphor-icons/react";
import CertTable, { STATUS_BADGE, STATUS_LABEL } from "./components/CertTable";
import { certs as mockCerts } from "./data/certs";

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

const SUMMARY_ORDER = ["expired", "expiring", "ok", "error"];

function formatFileSize(bytes) {
  if (bytes < 1024) return `${bytes} B`;
  return `${(bytes / 1024).toFixed(1)} KB`;
}

export default function App() {
  const [query, setQuery] = useState("");
  const [certs, setCerts] = useState(mockCerts);
  const [isLive, setIsLive] = useState(false);
  const [statusFilter, setStatusFilter] = useState(() => new Set());
  const [clusterFilter, setClusterFilter] = useState(() => new Set());

  function reloadCerts() {
    return fetch("/api/certs")
      .then((res) => (res.ok ? res.json() : Promise.reject(res.status)))
      .then((data) => {
        setCerts(data);
        setIsLive(true);
      });
  }

  useEffect(() => {
    reloadCerts().catch(() => {
      // no live backend (e.g. `npm run dev` standalone) — keep mock data
    });
  }, []);

  const [addClusterOpen, setAddClusterOpen] = useState(false);
  const [addClusterFile, setAddClusterFile] = useState(null);
  const [addClusterDragOver, setAddClusterDragOver] = useState(false);
  const [addClusterError, setAddClusterError] = useState("");
  const [addClusterBusy, setAddClusterBusy] = useState(false);

  function resetAddCluster() {
    setAddClusterFile(null);
    setAddClusterDragOver(false);
    setAddClusterError("");
  }

  function submitAddCluster() {
    if (!addClusterFile) {
      setAddClusterError("Choose a kubeconfig file");
      return;
    }
    setAddClusterBusy(true);
    setAddClusterError("");
    const body = new FormData();
    body.append("kubeconfig", addClusterFile);
    fetch("/api/clusters", { method: "POST", body })
      .then(async (res) => {
        if (!res.ok) throw new Error(await res.text());
        return reloadCerts();
      })
      .then(() => {
        setAddClusterOpen(false);
        resetAddCluster();
      })
      .catch((err) => setAddClusterError(String(err.message || err)))
      .finally(() => setAddClusterBusy(false));
  }

  const rows = useMemo(
    () =>
      certs.filter(
        (r) =>
          matchesSearch(r, query) &&
          matchesFilters(r, statusFilter, clusterFilter),
      ),
    [certs, query, statusFilter, clusterFilter],
  );

  const summary = useMemo(() => {
    const counts = {};
    for (const row of certs) counts[row.status] = (counts[row.status] ?? 0) + 1;
    return SUMMARY_ORDER.filter((s) => counts[s]).map((s) => [s, counts[s]]);
  }, [certs]);

  const [serverClusters, setServerClusters] = useState(null);

  const clusters = useMemo(() => {
    if (serverClusters) return serverClusters;
    return [...new Set(certs.map((r) => r.cluster).filter((c) => c !== "-"))].sort();
  }, [certs, serverClusters]);

  const clusterCount = clusters.length;

  const activeFilterCount = statusFilter.size + clusterFilter.size;

  function toggleStatusFilter(status) {
    setStatusFilter((prev) => {
      const next = new Set(prev);
      if (next.has(status)) next.delete(status);
      else next.add(status);
      return next;
    });
  }

  function toggleClusterFilter(cluster) {
    setClusterFilter((prev) => {
      const next = new Set(prev);
      if (next.has(cluster)) next.delete(cluster);
      else next.add(cluster);
      return next;
    });
  }

  function clearFilters() {
    setStatusFilter(new Set());
    setClusterFilter(new Set());
  }

  function exportJson() {
    const blob = new Blob([JSON.stringify(rows, null, 2)], {
      type: "application/json",
    });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = "certhealthz-certs.json";
    a.click();
    URL.revokeObjectURL(url);
  }

  return (
    <TooltipProvider>
      <div className="shell mx-auto w-full px-6 py-8">
        <header className="mb-3 flex flex-wrap items-baseline justify-between gap-3">
          <div className="flex flex-wrap items-baseline gap-3">
            <Text variant="heading" size="lg" as="h1">
              CertHealthz
            </Text>
            <Text variant="secondary" size="xs">
              Live TLS certificate health across clusters
            </Text>
          </div>
        </header>

        <div className="mb-3 flex flex-wrap items-center gap-2">
          <Toolbar className="flex-1">
            <Toolbar.InputGroup
              aria-label="Search certificates"
              className="flex-1"
            >
              <InputGroup.Addon>
                <MagnifyingGlassIcon />
              </InputGroup.Addon>
              <InputGroup.Input
                placeholder="Search certificates"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
              />
            </Toolbar.InputGroup>
            <DropdownMenu>
              <DropdownMenu.Trigger
                render={
                  <Toolbar.Button icon={FunnelIcon}>
                    Filters
                    {activeFilterCount > 0 && (
                      <Badge variant="primary">{activeFilterCount}</Badge>
                    )}
                  </Toolbar.Button>
                }
              />
              <DropdownMenu.Content>
                <DropdownMenu.Group>
                  <DropdownMenu.Label>Status</DropdownMenu.Label>
                  {SUMMARY_ORDER.map((status) => (
                    <DropdownMenu.CheckboxItem
                      key={status}
                      checked={statusFilter.has(status)}
                      onCheckedChange={() => toggleStatusFilter(status)}
                      closeOnClick={false}
                    >
                      {STATUS_LABEL[status]}
                    </DropdownMenu.CheckboxItem>
                  ))}
                </DropdownMenu.Group>
                <DropdownMenu.Separator />
                <DropdownMenu.Group>
                  <DropdownMenu.Label>Cluster</DropdownMenu.Label>
                  {clusters.map((cluster) => (
                    <DropdownMenu.CheckboxItem
                      key={cluster}
                      checked={clusterFilter.has(cluster)}
                      onCheckedChange={() => toggleClusterFilter(cluster)}
                      closeOnClick={false}
                    >
                      {cluster}
                    </DropdownMenu.CheckboxItem>
                  ))}
                </DropdownMenu.Group>
                {activeFilterCount > 0 && (
                  <>
                    <DropdownMenu.Separator />
                    <DropdownMenu.Item onClick={clearFilters}>
                      Clear filters
                    </DropdownMenu.Item>
                  </>
                )}
              </DropdownMenu.Content>
            </DropdownMenu>
            <Tooltip content="No backend wired up in this preview">
              <Toolbar.Button
                icon={GearSixIcon}
                aria-label="Display options"
                disabled
              />
            </Tooltip>
            <Toolbar.Button
              icon={DownloadSimpleIcon}
              onClick={exportJson}
            >
              Export
            </Toolbar.Button>
          </Toolbar>
          {isLive ? (
            <Popover
              open={addClusterOpen}
              onOpenChange={(open) => {
                setAddClusterOpen(open);
                if (!open) resetAddCluster();
              }}
            >
              <Popover.Trigger
                render={(p) => (
                  <Button {...p} variant="primary" icon={PlusIcon}>
                    Add cluster
                  </Button>
                )}
              />
              <Popover.Content className="w-96 p-6">
                <div className="mb-4 flex items-start justify-between gap-4">
                  <Popover.Title className="text-lg font-semibold">
                    Add cluster
                  </Popover.Title>
                  <Popover.Close
                    aria-label="Close"
                    render={(p) => (
                      <Button
                        {...p}
                        variant="secondary"
                        shape="square"
                        icon={<XIcon />}
                        aria-label="Close"
                      />
                    )}
                  />
                </div>
                <Popover.Description className="mb-4 text-kumo-subtle">
                  Upload a kubeconfig file for the cluster you want to
                  monitor. It's validated before being added.
                </Popover.Description>

                {addClusterFile ? (
                  <div className="flex items-center justify-between gap-3 rounded-lg border border-kumo-line bg-kumo-base px-3 py-2">
                    <div className="flex min-w-0 items-center gap-2">
                      <Text as="span" size="sm" className="truncate">
                        {addClusterFile.name}
                      </Text>
                      <Text as="span" variant="secondary" size="xs">
                        {formatFileSize(addClusterFile.size)}
                      </Text>
                    </div>
                    <Button
                      variant="ghost"
                      shape="square"
                      size="sm"
                      icon={<FileXIcon />}
                      aria-label="Remove file"
                      onClick={() => {
                        setAddClusterFile(null);
                        setAddClusterError("");
                      }}
                    />
                  </div>
                ) : (
                  <label
                    className={`flex cursor-pointer flex-col items-center gap-2 rounded-lg border border-dashed px-4 py-8 text-center transition-colors ${
                      addClusterDragOver
                        ? "border-kumo-accent bg-kumo-elevated"
                        : "border-kumo-line"
                    }`}
                    onDragOver={(e) => {
                      e.preventDefault();
                      setAddClusterDragOver(true);
                    }}
                    onDragLeave={() => setAddClusterDragOver(false)}
                    onDrop={(e) => {
                      e.preventDefault();
                      setAddClusterDragOver(false);
                      const file = e.dataTransfer.files?.[0];
                      if (file) {
                        setAddClusterFile(file);
                        setAddClusterError("");
                      }
                    }}
                  >
                    <CloudArrowUpIcon
                      size={28}
                      className="text-kumo-subtle"
                    />
                    <Text as="span" size="sm">
                      Drag and drop your kubeconfig file here, or{" "}
                      <Text as="span" variant="mono" size="sm">
                        click to browse
                      </Text>
                    </Text>
                    <input
                      type="file"
                      accept=".yaml,.yml,text/yaml,text/plain,application/x-yaml"
                      className="hidden"
                      onChange={(e) => {
                        const file = e.target.files?.[0];
                        if (file) {
                          setAddClusterFile(file);
                          setAddClusterError("");
                        }
                        e.target.value = "";
                      }}
                    />
                  </label>
                )}
                {addClusterError && (
                  <Text variant="error" size="sm" className="mt-2 block">
                    {addClusterError}
                  </Text>
                )}

                <div className="mt-4 flex justify-end gap-2">
                  <Popover.Close
                    render={(p) => (
                      <Button {...p} variant="secondary">
                        Cancel
                      </Button>
                    )}
                  />
                  <Button
                    variant="primary"
                    onClick={submitAddCluster}
                    disabled={addClusterBusy || !addClusterFile}
                  >
                    {addClusterBusy ? "Checking…" : "Add"}
                  </Button>
                </div>
              </Popover.Content>
            </Popover>
          ) : (
            <Tooltip content="No backend wired up in this preview">
              <Button variant="primary" icon={PlusIcon} disabled>
                Add cluster
              </Button>
            </Tooltip>
          )}
        </div>

        <LayerCard className="p-0">
          <LayerCard.Secondary className="flex flex-wrap items-center justify-between gap-2 px-4 py-3">
            <Text as="span" variant="secondary" size="sm">
              {certs.length} certificates tracked across {clusterCount} clusters
              and live endpoints
            </Text>
            <div className="flex flex-wrap items-center gap-2">
              {summary.map(([status, count]) => (
                <Badge
                  key={status}
                  variant={STATUS_BADGE[status]}
                  appearance="dot"
                >
                  {count} {STATUS_LABEL[status].toLowerCase()}
                </Badge>
              ))}
            </div>
          </LayerCard.Secondary>

          <CertTable rows={rows} />

          <LayerCard.Secondary className="flex flex-wrap items-center justify-between gap-2 px-4 py-3">
            <Text as="span" variant="secondary" size="sm">
              Showing {rows.length === 0 ? 0 : 1}–{rows.length} of{" "}
              {certs.length}
            </Text>
          </LayerCard.Secondary>
        </LayerCard>

      </div>
    </TooltipProvider>
  );
}
