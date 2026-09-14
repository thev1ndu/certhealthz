import { PlugsConnectedIcon } from "@phosphor-icons/react";
import { Empty, EmptyDescription, EmptyMedia, EmptyTitle } from "@/components/ui/empty";

export default function NotConnected() {
  return (
    <Empty className="border border-dashed border-border">
      <EmptyMedia variant="icon">
        <PlugsConnectedIcon size={20} />
      </EmptyMedia>
      <EmptyTitle>Not connected to a live scan</EmptyTitle>
      <EmptyDescription>
        Run this and open this page from there:
        <br />
        <span className="mt-1 inline-block rounded-md bg-muted px-2 py-1 text-xs text-foreground">
          certhealthz ui
        </span>
      </EmptyDescription>
    </Empty>
  );
}
