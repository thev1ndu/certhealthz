import { DownloadSimpleIcon, FunnelIcon, MagnifyingGlassIcon } from "@phosphor-icons/react";
import { ColumnVisibilityMenu } from "@/components/CertTable";
import AddEndpointPopover from "@/components/certificates/AddEndpointPopover";
import SourcesDialog from "@/components/certificates/SourcesDialog";
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

export default function EndpointsToolbar({
  query,
  onQueryChange,
  statusFilter,
  onToggleStatus,
  onClearFilters,
  onExport,
  isLive,
  addEndpoint,
  onSourcesChanged,
  visibleColumns,
  onToggleColumn,
}) {
  const activeFilterCount = statusFilter.size;

  return (
    <div className="mb-4 flex flex-wrap items-center gap-2">
      <div className="relative min-w-[220px] flex-1">
        <MagnifyingGlassIcon
          size={15}
          className="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-muted-foreground"
        />
        <Input
          placeholder="Search endpoints"
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
                <span className="ml-1 flex size-4 items-center justify-center rounded-full bg-primary text-[10px] text-primary-foreground">
                  {activeFilterCount}
                </span>
              )}
            </Button>
          }
        />
        <DropdownMenuContent className="corner-ticks relative rounded-none">
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
        <SourcesDialog isLive={isLive} onChanged={onSourcesChanged} scope="endpoints" />
        <AddEndpointPopover isLive={isLive} addEndpoint={addEndpoint} />
        <ColumnVisibilityMenu visible={visibleColumns} onToggle={onToggleColumn} />
      </div>
    </div>
  );
}
