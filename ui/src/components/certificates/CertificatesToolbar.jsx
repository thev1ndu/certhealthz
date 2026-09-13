import { DownloadSimpleIcon, FunnelIcon, MagnifyingGlassIcon } from "@phosphor-icons/react";
import AddClusterPopover from "@/components/certificates/AddClusterPopover";
import AddEndpointPopover from "@/components/certificates/AddEndpointPopover";
import { STATUS_LABEL } from "@/components/StatusBadge";
import { Button } from "@/components/ui/button";
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
import { Input } from "@/components/ui/input";
import { STATUS_ORDER } from "@/lib/statuses";

export default function CertificatesToolbar({
  query,
  onQueryChange,
  statusFilter,
  clusterFilter,
  onToggleStatus,
  onToggleCluster,
  onClearFilters,
  clusters,
  onExport,
  isLive,
  addCluster,
  addEndpoint,
}) {
  const activeFilterCount = statusFilter.size + clusterFilter.size;

  return (
    <div className="mb-4 flex flex-wrap items-center gap-2">
      <div className="relative min-w-[220px] flex-1">
        <MagnifyingGlassIcon
          size={15}
          className="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-muted-foreground"
        />
        <Input
          placeholder="Search certificates"
          value={query}
          onChange={(e) => onQueryChange(e.target.value)}
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
            {STATUS_ORDER.map((status) => (
              <DropdownMenuCheckboxItem
                key={status}
                checked={statusFilter.has(status)}
                onCheckedChange={() => onToggleStatus(status)}
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
                onCheckedChange={() => onToggleCluster(cluster)}
                closeOnClick={false}
              >
                {cluster}
              </DropdownMenuCheckboxItem>
            ))}
          </DropdownMenuGroup>
          {activeFilterCount > 0 && (
            <>
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={onClearFilters}>Clear filters</DropdownMenuItem>
            </>
          )}
        </DropdownMenuContent>
      </DropdownMenu>

      <Button variant="outline" onClick={onExport}>
        <DownloadSimpleIcon data-icon="inline-start" />
        Export
      </Button>

      <div className="ml-auto flex items-center gap-2">
        <AddClusterPopover isLive={isLive} addCluster={addCluster} />
        <AddEndpointPopover isLive={isLive} addEndpoint={addEndpoint} />
      </div>
    </div>
  );
}
