import { useState } from "react";
import { CertificateIcon, PlugsConnectedIcon, StackIcon, TrashIcon } from "@phosphor-icons/react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useSources } from "@/hooks/useSources";

function SourceRow({ name, removable, removeLabel, busy, onRemove }) {
  return (
    <div className="flex items-center justify-between gap-3 border-b border-border px-1 py-2.5 last:border-0">
      <span className="truncate text-sm">{name}</span>
      {removable ? (
        <Button
          variant="outline"
          size="icon-sm"
          className="rounded-none"
          aria-label={`Remove ${name}`}
          disabled={busy}
          onClick={onRemove}
        >
          <TrashIcon />
        </Button>
      ) : (
        <Tooltip>
          <TooltipTrigger render={<span />}>
            <span className="rounded-none bg-muted px-2 py-0.5 text-[10px] text-muted-foreground uppercase">
              {removeLabel}
            </span>
          </TooltipTrigger>
          <TooltipContent>Configured at startup — restart without that flag to remove it</TooltipContent>
        </Tooltip>
      )}
    </div>
  );
}

export default function SourcesDialog({ isLive, onChanged }) {
  const [open, setOpen] = useState(false);
  const [view, setView] = useState("clusters");
  const { clusters, endpoints, error, removingKey, removeCluster, removeEndpoint } = useSources(
    isLive,
    open,
    onChanged,
  );

  if (!isLive) return null;

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger
        render={
          <Button variant="outline">
            <StackIcon data-icon="inline-start" />
            Manage sources
          </Button>
        }
      />
      <DialogContent className="corner-ticks rounded-none sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Manage sources</DialogTitle>
          <DialogDescription>
            Every cluster and endpoint this scan covers. Only ones added through the UI can be
            removed here.
          </DialogDescription>
        </DialogHeader>

        <ToggleGroup
          value={[view]}
          onValueChange={(v) => v[0] && setView(v[0])}
          variant="outline"
          spacing={0}
          className="w-full"
        >
          <ToggleGroupItem value="clusters" className="flex-1">
            <CertificateIcon data-icon="inline-start" />
            Clusters
          </ToggleGroupItem>
          <ToggleGroupItem value="endpoints" className="flex-1">
            <PlugsConnectedIcon data-icon="inline-start" />
            Endpoints
          </ToggleGroupItem>
        </ToggleGroup>

        {error && <p className="text-sm text-destructive">{error}</p>}

        <div className="max-h-72 overflow-y-auto">
          {view === "clusters" &&
            (clusters.length === 0 ? (
              <p className="py-4 text-center text-sm text-muted-foreground">
                No clusters configured.
              </p>
            ) : (
              clusters.map((c) => (
                <SourceRow
                  key={c.label}
                  name={c.label}
                  removable={c.removable}
                  removeLabel="kubeconfig flag"
                  busy={removingKey === `cluster:${c.label}`}
                  onRemove={() => removeCluster(c.label)}
                />
              ))
            ))}

          {view === "endpoints" &&
            (endpoints.length === 0 ? (
              <p className="py-4 text-center text-sm text-muted-foreground">
                No endpoints configured.
              </p>
            ) : (
              endpoints.map((e) => (
                <SourceRow
                  key={e.endpoint}
                  name={e.endpoint}
                  removable={e.removable}
                  removeLabel="probe flag"
                  busy={removingKey === `endpoint:${e.endpoint}`}
                  onRemove={() => removeEndpoint(e.endpoint)}
                />
              ))
            ))}
        </div>
      </DialogContent>
    </Dialog>
  );
}
