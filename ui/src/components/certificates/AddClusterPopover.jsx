import { CloudArrowUpIcon, FileXIcon, PlusIcon, XIcon } from "@phosphor-icons/react";
import { Button } from "@/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverDescription,
  PopoverTitle,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { formatFileSize } from "@/lib/format";

// Disabled buttons don't fire pointer events, so the tooltip trigger wraps
// them in a span — see the same pattern in AddEndpointPopover.
function Disabled() {
  return (
    <Tooltip>
      <TooltipTrigger render={<span />}>
        <Button disabled>
          <PlusIcon data-icon="inline-start" />
          Add cluster
        </Button>
      </TooltipTrigger>
      <TooltipContent>No backend wired up in this preview</TooltipContent>
    </Tooltip>
  );
}

export default function AddClusterPopover({ isLive, addCluster }) {
  if (!isLive) return <Disabled />;

  const { open, file, dragOver, error, busy, setDragOver, openChange, pickFile, submit } =
    addCluster;

  return (
    <Popover open={open} onOpenChange={openChange}>
      <PopoverTrigger
        render={
          <Button>
            <PlusIcon data-icon="inline-start" />
            Add cluster
          </Button>
        }
      />
      <PopoverContent className="corner-ticks relative w-96 rounded-none">
        <div className="mb-1 flex items-start justify-between gap-4">
          <PopoverTitle>Add cluster</PopoverTitle>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label="Close"
            onClick={() => openChange(false)}
          >
            <XIcon />
          </Button>
        </div>
        <PopoverDescription className="mb-4">
          Upload a kubeconfig file for the cluster you want to monitor. It's validated before
          being added.
        </PopoverDescription>

        {file ? (
          <div className="flex items-center justify-between gap-3 rounded-none border border-border bg-muted/40 px-3 py-2">
            <div className="flex min-w-0 items-center gap-2">
              <span className="truncate text-sm">{file.name}</span>
              <span className="text-xs text-muted-foreground">
                {formatFileSize(file.size)}
              </span>
            </div>
            <Button
              variant="outline"
              size="icon-sm"
              className="rounded-none"
              aria-label="Remove file"
              onClick={() => pickFile(null)}
            >
              <FileXIcon />
            </Button>
          </div>
        ) : (
          <label
            className={
              "flex cursor-pointer flex-col items-center gap-2 rounded-none border border-dashed px-4 py-8 text-center transition-colors " +
              (dragOver ? "border-primary bg-primary/5" : "border-border")
            }
            onDragOver={(e) => {
              e.preventDefault();
              setDragOver(true);
            }}
            onDragLeave={() => setDragOver(false)}
            onDrop={(e) => {
              e.preventDefault();
              setDragOver(false);
              const dropped = e.dataTransfer.files?.[0];
              if (dropped) pickFile(dropped);
            }}
          >
            <CloudArrowUpIcon size={26} className="text-muted-foreground" />
            <span className="text-sm">
              Drag and drop your kubeconfig file here, or{" "}
              <span className="text-xs">click to browse</span>
            </span>
            <input
              type="file"
              accept=".yaml,.yml,text/yaml,text/plain,application/x-yaml"
              className="hidden"
              onChange={(e) => {
                const picked = e.target.files?.[0];
                if (picked) pickFile(picked);
                e.target.value = "";
              }}
            />
          </label>
        )}
        {error && <p className="mt-2 text-sm text-destructive">{error}</p>}

        <div className="mt-4 flex justify-end gap-2">
          <Button variant="secondary" onClick={() => openChange(false)}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={busy || !file}>
            {busy ? "Checking…" : "Add"}
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  );
}
