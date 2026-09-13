import { useEffect, useMemo, useState } from "react";
import {
  Badge,
  Banner,
  Button,
  Dialog,
  DropdownMenu,
  Input,
  InputGroup,
  LayerCard,
  Text,
  Toolbar,
  Tooltip,
  TooltipProvider,
} from "@cloudflare/kumo";
import {
  DownloadSimpleIcon,
  FunnelIcon,
  GearSixIcon,
  InfoIcon,
  MagnifyingGlassIcon,
  PlusIcon,
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

export default function App() {
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState(new Set());
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
  const [addClusterPath, setAddClusterPath] = useState("");
  const [addClusterError, setAddClusterError] = useState("");
  const [addClusterBusy, setAddClusterBusy] = useState(false);

  function submitAddCluster() {
    const path = addClusterPath.trim();
    if (!path) {
      setAddClusterError("Enter a kubeconfig path");
      return;
    }
    setAddClusterBusy(true);
    setAddClusterError("");
    fetch("/api/clusters", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ path }),
    })
      .then(async (res) => {
        if (!res.ok) throw new Error(await res.text());
        return reloadCerts();
      })
      .then(() => {
        setAddClusterOpen(false);
        setAddClusterPath("");
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

  function toggleRow(id) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  function toggleAll() {
    setSelected((prev) =>
      prev.size === rows.length ? new Set() : new Set(rows.map((r) => r.id)),
    );
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
              certhealthz
            </Text>
            <Text variant="secondary" size="xs">
              Live TLS certificate health across clusters
            </Text>
          </div>
          <Text as="code" variant="mono-secondary" size="xs">
            $ certhealthz scan --dashboard :8090
          </Text>
        </header>

        {!isLive && (
          <Banner
            className="mb-3"
            size="sm"
            variant="secondary"
            icon={<InfoIcon weight="fill" />}
            title="Sample data — nothing here hit a real cluster"
            description={
              <>
                No <Text as="code" variant="mono" size="sm">/api/certs</Text>{" "}
                backend reachable, showing mock data. Run{" "}
                <Text as="code" variant="mono" size="sm">
                  certhealthz dashboard
                </Text>{" "}
                to serve this against a real scan.
              </>
            }
          />
        )}

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
            <Dialog.Root
              open={addClusterOpen}
              onOpenChange={(open) => {
                setAddClusterOpen(open);
                if (!open) {
                  setAddClusterPath("");
                  setAddClusterError("");
                }
              }}
            >
              <Dialog.Trigger
                render={(p) => (
                  <Button {...p} variant="primary" icon={PlusIcon}>
                    Add cluster
                  </Button>
                )}
              />
              <Dialog className="p-6">
                <div className="mb-3 flex items-start justify-between gap-4">
                  <Dialog.Title className="text-lg font-semibold">
                    Add cluster
                  </Dialog.Title>
                  <Dialog.Close
                    aria-label="Close"
                    render={(p) => (
                      <Button
                        {...p}
                        variant="secondary"
                        shape="square"
                        aria-label="Close"
                      >
                        ×
                      </Button>
                    )}
                  />
                </div>
                <Dialog.Description className="mb-4 text-kumo-subtle">
                  Path to a kubeconfig file readable by the{" "}
                  <Text as="code" variant="mono" size="sm">
                    certhealthz dashboard
                  </Text>{" "}
                  process. It's validated before being added.
                </Dialog.Description>
                <Input
                  placeholder="/home/you/.kube/staging-config"
                  value={addClusterPath}
                  onChange={(e) => setAddClusterPath(e.target.value)}
                  error={addClusterError || undefined}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") submitAddCluster();
                  }}
                />
                <div className="mt-4 flex justify-end gap-2">
                  <Dialog.Close
                    render={(p) => (
                      <Button {...p} variant="secondary">
                        Cancel
                      </Button>
                    )}
                  />
                  <Button
                    variant="primary"
                    onClick={submitAddCluster}
                    disabled={addClusterBusy}
                  >
                    {addClusterBusy ? "Checking…" : "Add"}
                  </Button>
                </div>
              </Dialog>
            </Dialog.Root>
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

          <CertTable
            rows={rows}
            selected={selected}
            onToggle={toggleRow}
            onToggleAll={toggleAll}
          />

          <LayerCard.Secondary className="flex flex-wrap items-center justify-between gap-2 px-4 py-3">
            <Text as="span" variant="secondary" size="sm">
              Showing {rows.length === 0 ? 0 : 1}–{rows.length} of{" "}
              {certs.length}
            </Text>
            <Text as="span" variant="secondary" size="sm">
              {selected.size} selected
            </Text>
          </LayerCard.Secondary>
        </LayerCard>

      </div>
    </TooltipProvider>
  );
}
