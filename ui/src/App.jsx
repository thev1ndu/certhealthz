import { useEffect, useMemo, useState } from "react";
import {
  CertificateIcon,
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
import CertTable, { STATUS_LABEL } from "@/components/CertTable";
import StatusBadge from "@/components/StatusBadge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardFooter, CardHeader } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Empty, EmptyDescription, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Popover,
  PopoverContent,
  PopoverDescription,
  PopoverTitle,
  PopoverTrigger,
} from "@/components/ui/popover";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
} from "@/components/ui/sidebar";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";

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

const TABS = [
  { id: "certificates", label: "Certificates", icon: CertificateIcon, index: "01" },
  { id: "history", label: "History", icon: ClockCounterClockwiseIcon, index: "02" },
  { id: "ct", label: "CT Check", icon: ShieldCheckIcon, index: "03" },
  { id: "settings", label: "Settings", icon: GearSixIcon, index: "04" },
];

const CT_SINCE_OPTIONS = [
  { value: "1h", label: "Last hour" },
  { value: "24h", label: "Last 24 hours" },
  { value: "168h", label: "Last 7 days" },
  { value: "720h", label: "Last 30 days" },
];

function formatFileSize(bytes) {
  if (bytes < 1024) return `${bytes} B`;
  return `${(bytes / 1024).toFixed(1)} KB`;
}

function PageHeading({ title, description }) {
  return (
    <div className="mb-6 border-b border-border pb-5">
      <div className="mb-1.5 font-mono text-xs tracking-widest text-primary uppercase">
        CertHealthz
      </div>
      <h1 className="font-heading text-2xl font-medium tracking-tight">{title}</h1>
      {description && (
        <p className="mt-1 max-w-2xl text-sm text-muted-foreground">{description}</p>
      )}
    </div>
  );
}

function NotConnected() {
  return (
    <Empty className="border border-dashed border-border">
      <EmptyMedia variant="icon">
        <PlugsConnectedIcon size={20} />
      </EmptyMedia>
      <EmptyTitle>Not connected to a live scan</EmptyTitle>
      <EmptyDescription>
        Run this and open this page from there:
        <br />
        <span className="mt-1 inline-block rounded-md bg-muted px-2 py-1 font-mono text-xs text-foreground">
          certhealthz ui
        </span>
      </EmptyDescription>
    </Empty>
  );
}

export default function App() {
  const [activeTab, setActiveTab] = useState("certificates");
  const [query, setQuery] = useState("");
  const [certs, setCerts] = useState([]);
  const [isLive, setIsLive] = useState(false);
  const [statusFilter, setStatusFilter] = useState(() => new Set());
  const [clusterFilter, setClusterFilter] = useState(() => new Set());
  const [serverClusters, setServerClusters] = useState(null);

  function reloadCerts() {
    return fetch("/api/certs")
      .then((res) => (res.ok ? res.json() : Promise.reject(res.status)))
      .then((data) => {
        setCerts(data);
        setIsLive(true);
      });
  }

  function reloadClusters() {
    return fetch("/api/clusters")
      .then((res) => (res.ok ? res.json() : Promise.reject(res.status)))
      .then((data) => setServerClusters(data))
      .catch(() => {
        // filter dropdown just falls back to clusters seen in cert rows
      });
  }

  useEffect(() => {
    reloadCerts().catch(() => {
      // no live backend (e.g. `npm run dev` standalone) — stays empty
    });
  }, []);

  useEffect(() => {
    if (isLive) reloadClusters();
  }, [isLive]);

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
        return Promise.all([reloadCerts(), reloadClusters()]);
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

  useEffect(() => {
    if (isLive && activeTab === "history") fetchHistoryDiff();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isLive, activeTab]);

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

  const clusters = useMemo(() => {
    // Union, not override: /api/clusters only knows explicit --kubeconfig
    // entries and uploads, not the implicit "default" cluster a bare scan
    // falls back to — that one only shows up in the cert rows themselves.
    const fromRows = certs.map((r) => r.cluster).filter((c) => c !== "-");
    return [...new Set([...(serverClusters ?? []), ...fromRows])].sort();
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
      <SidebarProvider defaultOpen={false}>
        <Sidebar collapsible="icon" className="border-r border-sidebar-border">
          <SidebarHeader className="px-3 py-4">
            <span className="font-heading text-sm font-semibold tracking-tight group-data-[collapsible=icon]:hidden">
              CertHealthz
            </span>
          </SidebarHeader>
          <SidebarContent>
            <SidebarGroup>
              <SidebarMenu>
                {TABS.map((tab) => (
                  <SidebarMenuItem key={tab.id}>
                    <SidebarMenuButton
                      isActive={activeTab === tab.id}
                      tooltip={tab.label}
                      onClick={() => setActiveTab(tab.id)}
                    >
                      <tab.icon size={16} />
                      <span>{tab.label}</span>
                      <span className="ml-auto font-mono text-[10px] text-muted-foreground group-data-[collapsible=icon]:hidden">
                        {tab.index}
                      </span>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                ))}
              </SidebarMenu>
            </SidebarGroup>
          </SidebarContent>
          <SidebarFooter className="px-3 pb-4">
            <div className="flex items-center gap-2 px-2.5 py-2 group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-0">
              <span
                className={
                  "size-1.5 shrink-0 rounded-full " +
                  (isLive ? "bg-status-ok" : "bg-status-neutral")
                }
              />
              <span className="font-mono text-[11px] text-muted-foreground group-data-[collapsible=icon]:hidden">
                {isLive ? "Connected to live scan" : "Not connected"}
              </span>
            </div>
          </SidebarFooter>
        </Sidebar>

        <SidebarInset className="relative">
          <main className="arch-frame relative mx-auto min-h-svh w-full max-w-[1344px] px-6 py-12 md:px-12">
            {activeTab === "certificates" && (
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

                <div className="mb-4 flex flex-wrap items-center gap-2">
                  <div className="relative min-w-[220px] flex-1">
                    <MagnifyingGlassIcon
                      size={15}
                      className="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-muted-foreground"
                    />
                    <Input
                      placeholder="Search certificates"
                      value={query}
                      onChange={(e) => setQuery(e.target.value)}
                      className="pl-8"
                    />
                  </div>

                  <DropdownMenu>
                    <DropdownMenuTrigger
                      render={
                        <Button variant="outline">
                          <FunnelIcon data-icon="inline-start" />
                          Filters
                          {activeFilterCount > 0 && (
                            <span className="ml-1 flex size-4 items-center justify-center rounded-full bg-primary font-mono text-[10px] text-primary-foreground">
                              {activeFilterCount}
                            </span>
                          )}
                        </Button>
                      }
                    />
                    <DropdownMenuContent>
                      <DropdownMenuGroup>
                        <DropdownMenuLabel>Status</DropdownMenuLabel>
                        {SUMMARY_ORDER.map((status) => (
                          <DropdownMenuCheckboxItem
                            key={status}
                            checked={statusFilter.has(status)}
                            onCheckedChange={() => toggleStatusFilter(status)}
                            closeOnClick={false}
                          >
                            {STATUS_LABEL[status]}
                          </DropdownMenuCheckboxItem>
                        ))}
                      </DropdownMenuGroup>
                      <DropdownMenuSeparator />
                      <DropdownMenuGroup>
                        <DropdownMenuLabel>Cluster</DropdownMenuLabel>
                        {clusters.map((cluster) => (
                          <DropdownMenuCheckboxItem
                            key={cluster}
                            checked={clusterFilter.has(cluster)}
                            onCheckedChange={() => toggleClusterFilter(cluster)}
                            closeOnClick={false}
                          >
                            {cluster}
                          </DropdownMenuCheckboxItem>
                        ))}
                      </DropdownMenuGroup>
                      {activeFilterCount > 0 && (
                        <>
                          <DropdownMenuSeparator />
                          <DropdownMenuItem onClick={clearFilters}>
                            Clear filters
                          </DropdownMenuItem>
                        </>
                      )}
                    </DropdownMenuContent>
                  </DropdownMenu>

                  <Button variant="outline" onClick={exportJson}>
                    <DownloadSimpleIcon data-icon="inline-start" />
                    Export
                  </Button>

                  <div className="ml-auto flex items-center gap-2">
                    {isLive ? (
                      <Popover
                        open={addClusterOpen}
                        onOpenChange={(open) => {
                          setAddClusterOpen(open);
                          if (!open) resetAddCluster();
                        }}
                      >
                        <PopoverTrigger
                          render={
                            <Button>
                              <PlusIcon data-icon="inline-start" />
                              Add cluster
                            </Button>
                          }
                        />
                        <PopoverContent className="w-96">
                          <div className="mb-1 flex items-start justify-between gap-4">
                            <PopoverTitle>Add cluster</PopoverTitle>
                            <Button
                              variant="ghost"
                              size="icon-sm"
                              aria-label="Close"
                              onClick={() => setAddClusterOpen(false)}
                            >
                              <XIcon />
                            </Button>
                          </div>
                          <PopoverDescription className="mb-4">
                            Upload a kubeconfig file for the cluster you want to
                            monitor. It's validated before being added.
                          </PopoverDescription>

                          {addClusterFile ? (
                            <div className="flex items-center justify-between gap-3 rounded-md border border-border bg-muted/40 px-3 py-2">
                              <div className="flex min-w-0 items-center gap-2">
                                <span className="truncate text-sm">
                                  {addClusterFile.name}
                                </span>
                                <span className="font-mono text-xs text-muted-foreground">
                                  {formatFileSize(addClusterFile.size)}
                                </span>
                              </div>
                              <Button
                                variant="ghost"
                                size="icon-sm"
                                aria-label="Remove file"
                                onClick={() => {
                                  setAddClusterFile(null);
                                  setAddClusterError("");
                                }}
                              >
                                <FileXIcon />
                              </Button>
                            </div>
                          ) : (
                            <label
                              className={
                                "flex cursor-pointer flex-col items-center gap-2 rounded-md border border-dashed px-4 py-8 text-center transition-colors " +
                                (addClusterDragOver
                                  ? "border-primary bg-primary/5"
                                  : "border-border")
                              }
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
                              <CloudArrowUpIcon size={26} className="text-muted-foreground" />
                              <span className="text-sm">
                                Drag and drop your kubeconfig file here, or{" "}
                                <span className="font-mono text-xs">click to browse</span>
                              </span>
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
                            <p className="mt-2 text-sm text-destructive">{addClusterError}</p>
                          )}

                          <div className="mt-4 flex justify-end gap-2">
                            <Button variant="secondary" onClick={() => setAddClusterOpen(false)}>
                              Cancel
                            </Button>
                            <Button
                              onClick={submitAddCluster}
                              disabled={addClusterBusy || !addClusterFile}
                            >
                              {addClusterBusy ? "Checking…" : "Add"}
                            </Button>
                          </div>
                        </PopoverContent>
                      </Popover>
                    ) : (
                      <Tooltip>
                        <TooltipTrigger render={<span />}>
                          <Button disabled>
                            <PlusIcon data-icon="inline-start" />
                            Add cluster
                          </Button>
                        </TooltipTrigger>
                        <TooltipContent>No backend wired up in this preview</TooltipContent>
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
                        <PopoverTrigger
                          render={
                            <Button variant="secondary">
                              <PlugsConnectedIcon data-icon="inline-start" />
                              Add endpoint
                            </Button>
                          }
                        />
                        <PopoverContent className="w-96">
                          <div className="mb-1 flex items-start justify-between gap-4">
                            <PopoverTitle>Add endpoint</PopoverTitle>
                            <Button
                              variant="ghost"
                              size="icon-sm"
                              aria-label="Close"
                              onClick={() => setAddEndpointOpen(false)}
                            >
                              <XIcon />
                            </Button>
                          </div>
                          <PopoverDescription className="mb-4">
                            Probe a live TLS endpoint on every scan, alongside your
                            clusters — a vendor API, a load balancer, anything
                            cert-manager doesn't manage.
                          </PopoverDescription>

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
                            <p className="mt-2 text-sm text-destructive">{addEndpointError}</p>
                          )}

                          <div className="mt-4 flex justify-end gap-2">
                            <Button variant="secondary" onClick={() => setAddEndpointOpen(false)}>
                              Cancel
                            </Button>
                            <Button
                              onClick={submitAddEndpoint}
                              disabled={addEndpointBusy || !addEndpointValue.trim()}
                            >
                              {addEndpointBusy ? "Adding…" : "Add"}
                            </Button>
                          </div>
                        </PopoverContent>
                      </Popover>
                    ) : (
                      <Tooltip>
                        <TooltipTrigger render={<span />}>
                          <Button variant="secondary" disabled>
                            <PlugsConnectedIcon data-icon="inline-start" />
                            Add endpoint
                          </Button>
                        </TooltipTrigger>
                        <TooltipContent>No backend wired up in this preview</TooltipContent>
                      </Tooltip>
                    )}
                  </div>
                </div>

                <Card size="sm" className="gap-0 p-0">
                  <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-2 border-b border-border px-4 py-3">
                    <span className="font-mono text-xs text-muted-foreground">
                      {certs.length} certificates tracked across {clusterCount}{" "}
                      clusters and live endpoints
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
            )}

            {activeTab === "history" && (
              <div className="max-w-2xl">
                <PageHeading
                  title="History"
                  description="Record a snapshot of the current scan, and see what changed since the last one"
                />

                {!isLive ? (
                  <NotConnected />
                ) : (
                  <Card size="sm">
                    <CardContent>
                      <Button onClick={recordSnapshot} disabled={historyBusy}>
                        {historyBusy ? "Working…" : "Record snapshot"}
                      </Button>

                      {historyError && (
                        <p className="mt-2 text-sm text-destructive">{historyError}</p>
                      )}

                      {historyDiff && (
                        <div className="mt-4">
                          {historyDiff.message && (
                            <p className="mb-2 text-sm text-muted-foreground">
                              {historyDiff.message}
                            </p>
                          )}
                          {historyDiff.changes?.length > 0 && (
                            <Table>
                              <TableHeader>
                                <TableRow>
                                  <TableHead>Change</TableHead>
                                  <TableHead>Source</TableHead>
                                  <TableHead>Name</TableHead>
                                  <TableHead>From</TableHead>
                                  <TableHead>To</TableHead>
                                </TableRow>
                              </TableHeader>
                              <TableBody>
                                {historyDiff.changes.map((c, i) => (
                                  <TableRow key={i}>
                                    <TableCell className="font-mono text-xs uppercase">
                                      {c.kind}
                                    </TableCell>
                                    <TableCell className="font-mono text-xs text-muted-foreground">
                                      {c.source}
                                    </TableCell>
                                    <TableCell>{c.name}</TableCell>
                                    <TableCell className="font-mono text-xs">
                                      {c.fromState || "—"}
                                    </TableCell>
                                    <TableCell className="font-mono text-xs">
                                      {c.toState || "—"}
                                    </TableCell>
                                  </TableRow>
                                ))}
                              </TableBody>
                            </Table>
                          )}
                        </div>
                      )}
                    </CardContent>
                  </Card>
                )}
              </div>
            )}

            {activeTab === "ct" && (
              <div className="max-w-2xl">
                <PageHeading
                  title="Check Certificate Transparency logs"
                  description="Find recently logged certs for your domains not covered by any known Secret"
                />

                {!isLive ? (
                  <NotConnected />
                ) : (
                  <Card size="sm">
                    <CardContent>
                      <FieldGroup>
                        <Field>
                          <FieldLabel htmlFor="ct-domains">
                            Domains (comma or newline separated)
                          </FieldLabel>
                          <Textarea
                            id="ct-domains"
                            placeholder="example.com, api.example.com"
                            value={ctDomains}
                            onChange={(e) => setCtDomains(e.target.value)}
                          />
                        </Field>
                        <Field className="w-fit">
                          <FieldLabel htmlFor="ct-since">Since</FieldLabel>
                          <Select value={ctSince} onValueChange={(v) => setCtSince(v ?? "24h")}>
                            <SelectTrigger id="ct-since" className="w-48">
                              <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                              <SelectGroup>
                                {CT_SINCE_OPTIONS.map((opt) => (
                                  <SelectItem key={opt.value} value={opt.value}>
                                    {opt.label}
                                  </SelectItem>
                                ))}
                              </SelectGroup>
                            </SelectContent>
                          </Select>
                        </Field>
                      </FieldGroup>

                      {ctError && <p className="mt-2 text-sm text-destructive">{ctError}</p>}

                      <div className="mt-4">
                        <Button onClick={submitCTCheck} disabled={ctBusy || !ctDomains.trim()}>
                          {ctBusy ? "Checking…" : "Check"}
                        </Button>
                      </div>

                      {ctResults && (
                        <div className="mt-4">
                          {ctResults.length === 0 ? (
                            <p className="text-sm text-muted-foreground">
                              No certs logged in that window.
                            </p>
                          ) : (
                            <Table>
                              <TableHeader>
                                <TableRow>
                                  <TableHead>Name</TableHead>
                                  <TableHead>Status</TableHead>
                                  <TableHead>Detail</TableHead>
                                </TableRow>
                              </TableHeader>
                              <TableBody>
                                {ctResults.map((r) => (
                                  <TableRow key={r.id}>
                                    <TableCell>{r.name}</TableCell>
                                    <TableCell>
                                      <StatusBadge status={r.status} />
                                    </TableCell>
                                    <TableCell className="text-muted-foreground">
                                      {r.detail}
                                    </TableCell>
                                  </TableRow>
                                ))}
                              </TableBody>
                            </Table>
                          )}
                        </div>
                      )}
                    </CardContent>
                  </Card>
                )}
              </div>
            )}

            {activeTab === "settings" && (
              <div className="max-w-lg">
                <PageHeading
                  title="Settings"
                  description="Change scan thresholds and alerting without restarting the server"
                />

                {!isLive ? (
                  <NotConnected />
                ) : (
                  <Card size="sm">
                    <CardContent>
                      <FieldGroup>
                        <Field>
                          <FieldLabel htmlFor="warn-days">Warn days</FieldLabel>
                          <Input
                            id="warn-days"
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
                        </Field>

                        <Field orientation="horizontal">
                          <Checkbox
                            id="include-secrets"
                            checked={settingsForm.includeSecrets}
                            onCheckedChange={(checked) =>
                              setSettingsForm((prev) => ({
                                ...prev,
                                includeSecrets: checked,
                              }))
                            }
                          />
                          <Label htmlFor="include-secrets" className="text-sm font-normal">
                            Scan raw Secrets (drift detection, Ingress cross-referencing)
                          </Label>
                        </Field>

                        <Field>
                          <FieldLabel htmlFor="webhook-url">Webhook URL</FieldLabel>
                          <Input
                            id="webhook-url"
                            placeholder="https://hooks.example.com/certhealthz"
                            value={settingsForm.webhookURL}
                            onChange={(e) =>
                              setSettingsForm((prev) => ({
                                ...prev,
                                webhookURL: e.target.value,
                              }))
                            }
                          />
                        </Field>
                      </FieldGroup>

                      {settingsError && (
                        <p className="mt-2 text-sm text-destructive">{settingsError}</p>
                      )}

                      <div className="mt-4 flex items-center justify-between gap-2">
                        <Button
                          variant="secondary"
                          onClick={sendTestAlert}
                          disabled={alertBusy || !settingsForm.webhookURL?.trim()}
                        >
                          {alertBusy ? "Sending…" : "Send test alert"}
                        </Button>
                        <Button onClick={submitSettings} disabled={settingsBusy}>
                          {settingsBusy ? "Saving…" : "Save"}
                        </Button>
                      </div>

                      {alertResult && (
                        <p
                          className={
                            "mt-2 text-sm " +
                            (alertResult.error ? "text-destructive" : "text-muted-foreground")
                          }
                        >
                          {alertResult.error
                            ? alertResult.error
                            : alertResult.sent
                              ? `Sent — ${alertResult.flagged} flagged certificate(s).`
                              : "Nothing to send — 0 flagged certificates."}
                        </p>
                      )}
                    </CardContent>
                  </Card>
                )}
              </div>
            )}
          </main>
        </SidebarInset>
      </SidebarProvider>
    </TooltipProvider>
  );
}
