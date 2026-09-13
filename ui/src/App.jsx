import { useEffect, useMemo, useState } from "react";
import {
  Badge,
  Button,
  Checkbox,
  DropdownMenu,
  Input,
  InputGroup,
  LayerCard,
  Popover,
  Text,
  Toolbar,
  Tooltip,
  TooltipProvider,
} from "@cloudflare/kumo";
import {
  ClockCounterClockwiseIcon,
  CloudArrowUpIcon,
  DownloadSimpleIcon,
  FileXIcon,
  FunnelIcon,
  GearSixIcon,
  MagnifyingGlassIcon,
  PlugsConnectedIcon,
  PlusIcon,
  ShieldCheckIcon,
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

const SUMMARY_ORDER = ["expired", "drift", "expiring", "ok", "error"];

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

  const [addEndpointOpen, setAddEndpointOpen] = useState(false);
  const [addEndpointValue, setAddEndpointValue] = useState("");
  const [addEndpointError, setAddEndpointError] = useState("");
  const [addEndpointBusy, setAddEndpointBusy] = useState(false);

  function resetAddEndpoint() {
    setAddEndpointValue("");
    setAddEndpointError("");
  }

  function submitAddEndpoint() {
    const endpoint = addEndpointValue.trim();
    if (!endpoint) {
      setAddEndpointError("Enter a host or host:port");
      return;
    }
    setAddEndpointBusy(true);
    setAddEndpointError("");
    fetch("/api/endpoints", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ endpoint }),
    })
      .then(async (res) => {
        if (!res.ok) throw new Error(await res.text());
        return reloadCerts();
      })
      .then(() => {
        setAddEndpointOpen(false);
        resetAddEndpoint();
      })
      .catch((err) => setAddEndpointError(String(err.message || err)))
      .finally(() => setAddEndpointBusy(false));
  }

  const [settingsOpen, setSettingsOpen] = useState(false);
  const [settingsForm, setSettingsForm] = useState({
    warnDays: 14,
    includeSecrets: true,
    webhookURL: "",
  });
  const [settingsError, setSettingsError] = useState("");
  const [settingsBusy, setSettingsBusy] = useState(false);
  const [alertResult, setAlertResult] = useState(null);
  const [alertBusy, setAlertBusy] = useState(false);

  useEffect(() => {
    if (!isLive) return;
    fetch("/api/settings")
      .then((res) => (res.ok ? res.json() : Promise.reject(res.status)))
      .then((data) => setSettingsForm(data))
      .catch(() => {
        // settings just keeps its defaults if this fails
      });
  }, [isLive]);

  function submitSettings() {
    setSettingsBusy(true);
    setSettingsError("");
    fetch("/api/settings", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(settingsForm),
    })
      .then(async (res) => {
        if (!res.ok) throw new Error(await res.text());
        return res.json();
      })
      .then((data) => {
        setSettingsForm(data);
        return reloadCerts();
      })
      .catch((err) => setSettingsError(String(err.message || err)))
      .finally(() => setSettingsBusy(false));
  }

  function sendTestAlert() {
    setAlertBusy(true);
    setAlertResult(null);
    fetch("/api/alert", { method: "POST" })
      .then(async (res) => {
        if (!res.ok) throw new Error(await res.text());
        return res.json();
      })
      .then((data) => setAlertResult(data))
      .catch((err) => setAlertResult({ error: String(err.message || err) }))
      .finally(() => setAlertBusy(false));
  }

  const [historyOpen, setHistoryOpen] = useState(false);
  const [historyDiff, setHistoryDiff] = useState(null);
  const [historyError, setHistoryError] = useState("");
  const [historyBusy, setHistoryBusy] = useState(false);

  function fetchHistoryDiff() {
    setHistoryBusy(true);
    setHistoryError("");
    fetch("/api/history/diff")
      .then((res) => (res.ok ? res.json() : Promise.reject(res.status)))
      .then((data) => setHistoryDiff(data))
      .catch((err) => setHistoryError(String(err.message || err)))
      .finally(() => setHistoryBusy(false));
  }

  function recordSnapshot() {
    setHistoryBusy(true);
    setHistoryError("");
    fetch("/api/history/record", { method: "POST" })
      .then(async (res) => {
        if (!res.ok) throw new Error(await res.text());
        return fetchHistoryDiff();
      })
      .catch((err) => {
        setHistoryError(String(err.message || err));
        setHistoryBusy(false);
      });
  }

  const [ctOpen, setCtOpen] = useState(false);
  const [ctDomains, setCtDomains] = useState("");
  const [ctSince, setCtSince] = useState("24h");
  const [ctResults, setCtResults] = useState(null);
  const [ctError, setCtError] = useState("");
  const [ctBusy, setCtBusy] = useState(false);

  function submitCTCheck() {
    const domains = ctDomains
      .split(/[,\n]/)
      .map((d) => d.trim())
      .filter(Boolean);
    if (domains.length === 0) {
      setCtError("Enter at least one domain");
      return;
    }
    setCtBusy(true);
    setCtError("");
    fetch("/api/ct", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ domains, since: ctSince }),
    })
      .then(async (res) => {
        if (!res.ok) throw new Error(await res.text());
        return res.json();
      })
      .then((data) => setCtResults(data))
      .catch((err) => setCtError(String(err.message || err)))
      .finally(() => setCtBusy(false));
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
            {isLive ? (
              <Popover
                open={settingsOpen}
                onOpenChange={(open) => {
                  setSettingsOpen(open);
                  if (open) {
                    setAlertResult(null);
                    setSettingsError("");
                  }
                }}
              >
                <Popover.Trigger
                  render={(p) => (
                    <Toolbar.Button
                      {...p}
                      icon={GearSixIcon}
                      aria-label="Settings"
                    />
                  )}
                />
                <Popover.Content className="w-96 p-6">
                  <div className="mb-4 flex items-start justify-between gap-4">
                    <Popover.Title className="text-lg font-semibold">
                      Settings
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
                    Change scan thresholds and alerting without restarting
                    the server.
                  </Popover.Description>

                  <div className="flex flex-col gap-3">
                    <label className="flex flex-col gap-1">
                      <Text as="span" size="sm">
                        Warn days
                      </Text>
                      <Input
                        type="number"
                        min={1}
                        value={settingsForm.warnDays}
                        onChange={(e) =>
                          setSettingsForm((prev) => ({
                            ...prev,
                            warnDays: Number(e.target.value),
                          }))
                        }
                      />
                    </label>

                    <label className="flex items-center gap-2">
                      <Checkbox
                        checked={settingsForm.includeSecrets}
                        onCheckedChange={(checked) =>
                          setSettingsForm((prev) => ({
                            ...prev,
                            includeSecrets: checked,
                          }))
                        }
                      />
                      <Text as="span" size="sm">
                        Scan raw Secrets (drift detection, Ingress
                        cross-referencing)
                      </Text>
                    </label>

                    <label className="flex flex-col gap-1">
                      <Text as="span" size="sm">
                        Webhook URL
                      </Text>
                      <Input
                        placeholder="https://hooks.example.com/certhealthz"
                        value={settingsForm.webhookURL}
                        onChange={(e) =>
                          setSettingsForm((prev) => ({
                            ...prev,
                            webhookURL: e.target.value,
                          }))
                        }
                      />
                    </label>
                  </div>

                  {settingsError && (
                    <Text variant="error" size="sm" className="mt-2 block">
                      {settingsError}
                    </Text>
                  )}

                  <div className="mt-4 flex items-center justify-between gap-2">
                    <Button
                      variant="secondary"
                      onClick={sendTestAlert}
                      disabled={alertBusy || !settingsForm.webhookURL?.trim()}
                    >
                      {alertBusy ? "Sending…" : "Send test alert"}
                    </Button>
                    <div className="flex gap-2">
                      <Popover.Close
                        render={(p) => (
                          <Button {...p} variant="secondary">
                            Close
                          </Button>
                        )}
                      />
                      <Button
                        variant="primary"
                        onClick={submitSettings}
                        disabled={settingsBusy}
                      >
                        {settingsBusy ? "Saving…" : "Save"}
                      </Button>
                    </div>
                  </div>

                  {alertResult && (
                    <Text
                      as="p"
                      size="sm"
                      variant={alertResult.error ? "error" : "secondary"}
                      className="mt-2"
                    >
                      {alertResult.error
                        ? alertResult.error
                        : alertResult.sent
                          ? `Sent — ${alertResult.flagged} flagged certificate(s).`
                          : "Nothing to send — 0 flagged certificates."}
                    </Text>
                  )}
                </Popover.Content>
              </Popover>
            ) : (
              <Tooltip content="No backend wired up in this preview">
                <Toolbar.Button
                  icon={GearSixIcon}
                  aria-label="Display options"
                  disabled
                />
              </Tooltip>
            )}
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
          {isLive ? (
            <Popover
              open={addEndpointOpen}
              onOpenChange={(open) => {
                setAddEndpointOpen(open);
                if (!open) resetAddEndpoint();
              }}
            >
              <Popover.Trigger
                render={(p) => (
                  <Button {...p} variant="secondary" icon={PlugsConnectedIcon}>
                    Add endpoint
                  </Button>
                )}
              />
              <Popover.Content className="w-96 p-6">
                <div className="mb-4 flex items-start justify-between gap-4">
                  <Popover.Title className="text-lg font-semibold">
                    Add endpoint
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
                  Probe a live TLS endpoint on every scan, alongside your
                  clusters — a vendor API, a load balancer, anything
                  cert-manager doesn't manage.
                </Popover.Description>

                <Input
                  placeholder="example.com or example.com:8443"
                  value={addEndpointValue}
                  onChange={(e) => {
                    setAddEndpointValue(e.target.value);
                    setAddEndpointError("");
                  }}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") submitAddEndpoint();
                  }}
                />
                {addEndpointError && (
                  <Text variant="error" size="sm" className="mt-2 block">
                    {addEndpointError}
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
                    onClick={submitAddEndpoint}
                    disabled={addEndpointBusy || !addEndpointValue.trim()}
                  >
                    {addEndpointBusy ? "Adding…" : "Add"}
                  </Button>
                </div>
              </Popover.Content>
            </Popover>
          ) : (
            <Tooltip content="No backend wired up in this preview">
              <Button variant="secondary" icon={PlugsConnectedIcon} disabled>
                Add endpoint
              </Button>
            </Tooltip>
          )}
          {isLive ? (
            <Popover
              open={historyOpen}
              onOpenChange={(open) => {
                setHistoryOpen(open);
                if (open) fetchHistoryDiff();
              }}
            >
              <Popover.Trigger
                render={(p) => (
                  <Button
                    {...p}
                    variant="secondary"
                    icon={ClockCounterClockwiseIcon}
                  >
                    History
                  </Button>
                )}
              />
              <Popover.Content className="w-[28rem] p-6">
                <div className="mb-4 flex items-start justify-between gap-4">
                  <Popover.Title className="text-lg font-semibold">
                    History
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
                  Record a snapshot of the current scan, and see what
                  changed since the last one.
                </Popover.Description>

                <Button
                  variant="primary"
                  onClick={recordSnapshot}
                  disabled={historyBusy}
                >
                  {historyBusy ? "Working…" : "Record snapshot"}
                </Button>

                {historyError && (
                  <Text variant="error" size="sm" className="mt-2 block">
                    {historyError}
                  </Text>
                )}

                {historyDiff && (
                  <div className="mt-4">
                    {historyDiff.message && (
                      <Text
                        as="p"
                        variant="secondary"
                        size="sm"
                        className="mb-2"
                      >
                        {historyDiff.message}
                      </Text>
                    )}
                    {historyDiff.changes?.length > 0 && (
                      <div className="max-h-64 overflow-y-auto rounded-lg border border-kumo-line">
                        <table className="w-full text-left text-xs">
                          <thead>
                            <tr className="border-b border-kumo-line">
                              <th className="px-2 py-1.5 font-medium">
                                Change
                              </th>
                              <th className="px-2 py-1.5 font-medium">
                                Name
                              </th>
                              <th className="px-2 py-1.5 font-medium">
                                From
                              </th>
                              <th className="px-2 py-1.5 font-medium">To</th>
                            </tr>
                          </thead>
                          <tbody>
                            {historyDiff.changes.map((c, i) => (
                              <tr
                                key={i}
                                className="border-b border-kumo-line last:border-0"
                              >
                                <td className="px-2 py-1.5">{c.kind}</td>
                                <td className="px-2 py-1.5 truncate">
                                  {c.name}
                                </td>
                                <td className="px-2 py-1.5">
                                  {c.fromState || "—"}
                                </td>
                                <td className="px-2 py-1.5">
                                  {c.toState || "—"}
                                </td>
                              </tr>
                            ))}
                          </tbody>
                        </table>
                      </div>
                    )}
                  </div>
                )}
              </Popover.Content>
            </Popover>
          ) : (
            <Tooltip content="No backend wired up in this preview">
              <Button variant="secondary" icon={ClockCounterClockwiseIcon} disabled>
                History
              </Button>
            </Tooltip>
          )}
          {isLive ? (
            <Popover
              open={ctOpen}
              onOpenChange={(open) => {
                setCtOpen(open);
                if (open) {
                  setCtError("");
                  setCtResults(null);
                }
              }}
            >
              <Popover.Trigger
                render={(p) => (
                  <Button {...p} variant="secondary" icon={ShieldCheckIcon}>
                    Check CT logs
                  </Button>
                )}
              />
              <Popover.Content className="w-[28rem] p-6">
                <div className="mb-4 flex items-start justify-between gap-4">
                  <Popover.Title className="text-lg font-semibold">
                    Check Certificate Transparency logs
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
                  Find certs recently logged for your domains that none of
                  your configured clusters' Secrets cover — possible
                  shadow/rogue issuance.
                </Popover.Description>

                <div className="flex flex-col gap-3">
                  <label className="flex flex-col gap-1">
                    <Text as="span" size="sm">
                      Domains (comma or newline separated)
                    </Text>
                    <textarea
                      className="min-h-16 rounded-lg border border-kumo-line bg-kumo-base p-2 text-sm"
                      placeholder="example.com, api.example.com"
                      value={ctDomains}
                      onChange={(e) => setCtDomains(e.target.value)}
                    />
                  </label>
                  <label className="flex flex-col gap-1">
                    <Text as="span" size="sm">
                      Since
                    </Text>
                    <select
                      className="rounded-lg border border-kumo-line bg-kumo-base p-2 text-sm"
                      value={ctSince}
                      onChange={(e) => setCtSince(e.target.value)}
                    >
                      <option value="1h">Last hour</option>
                      <option value="24h">Last 24 hours</option>
                      <option value="168h">Last 7 days</option>
                      <option value="720h">Last 30 days</option>
                    </select>
                  </label>
                </div>

                {ctError && (
                  <Text variant="error" size="sm" className="mt-2 block">
                    {ctError}
                  </Text>
                )}

                <div className="mt-4 flex justify-end gap-2">
                  <Popover.Close
                    render={(p) => (
                      <Button {...p} variant="secondary">
                        Close
                      </Button>
                    )}
                  />
                  <Button
                    variant="primary"
                    onClick={submitCTCheck}
                    disabled={ctBusy || !ctDomains.trim()}
                  >
                    {ctBusy ? "Checking…" : "Check"}
                  </Button>
                </div>

                {ctResults && (
                  <div className="mt-4">
                    {ctResults.length === 0 ? (
                      <Text as="p" variant="secondary" size="sm">
                        No certs logged in that window.
                      </Text>
                    ) : (
                      <div className="max-h-64 overflow-y-auto rounded-lg border border-kumo-line">
                        <table className="w-full text-left text-xs">
                          <thead>
                            <tr className="border-b border-kumo-line">
                              <th className="px-2 py-1.5 font-medium">
                                Name
                              </th>
                              <th className="px-2 py-1.5 font-medium">
                                Status
                              </th>
                              <th className="px-2 py-1.5 font-medium">
                                Detail
                              </th>
                            </tr>
                          </thead>
                          <tbody>
                            {ctResults.map((r) => (
                              <tr
                                key={r.id}
                                className="border-b border-kumo-line last:border-0"
                              >
                                <td className="px-2 py-1.5 truncate">
                                  {r.name}
                                </td>
                                <td className="px-2 py-1.5">
                                  <Badge
                                    variant={STATUS_BADGE[r.status]}
                                    appearance="dot"
                                  >
                                    {STATUS_LABEL[r.status]}
                                  </Badge>
                                </td>
                                <td
                                  className="max-w-56 truncate px-2 py-1.5"
                                  title={r.detail}
                                >
                                  {r.detail}
                                </td>
                              </tr>
                            ))}
                          </tbody>
                        </table>
                      </div>
                    )}
                  </div>
                )}
              </Popover.Content>
            </Popover>
          ) : (
            <Tooltip content="No backend wired up in this preview">
              <Button variant="secondary" icon={ShieldCheckIcon} disabled>
                Check CT logs
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
