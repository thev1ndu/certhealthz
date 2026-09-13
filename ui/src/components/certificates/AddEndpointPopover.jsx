import { PlugsConnectedIcon, XIcon } from "@phosphor-icons/react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Popover,
  PopoverContent,
  PopoverDescription,
  PopoverTitle,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";

function Disabled() {
  return (
    <Tooltip>
      <TooltipTrigger render={<span />}>
        <Button variant="secondary" disabled>
          <PlugsConnectedIcon data-icon="inline-start" />
          Add endpoint
        </Button>
      </TooltipTrigger>
      <TooltipContent>No backend wired up in this preview</TooltipContent>
    </Tooltip>
  );
}

export default function AddEndpointPopover({ isLive, addEndpoint }) {
  if (!isLive) return <Disabled />;

  const { open, value, error, busy, openChange, changeValue, submit } = addEndpoint;

  return (
    <Popover open={open} onOpenChange={openChange}>
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
          <Button variant="ghost" size="icon-sm" aria-label="Close" onClick={() => openChange(false)}>
            <XIcon />
          </Button>
        </div>
        <PopoverDescription className="mb-4">
          Probe a live TLS endpoint on every scan, alongside your clusters — a vendor API, a load
          balancer, anything cert-manager doesn't manage.
        </PopoverDescription>

        <Input
          placeholder="example.com or example.com:8443"
          value={value}
          onChange={(e) => changeValue(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") submit();
          }}
        />
        {error && <p className="mt-2 text-sm text-destructive">{error}</p>}

        <div className="mt-4 flex justify-end gap-2">
          <Button variant="secondary" onClick={() => openChange(false)}>
            Cancel
          </Button>
          <Button onClick={submit} disabled={busy || !value.trim()}>
            {busy ? "Adding…" : "Add"}
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  );
}
