import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

const STATE_LABEL = {
  "ingress-only": "Ingress-only",
  "dual-running": "Dual-running",
  "gateway-only": "Gateway-only",
};

const STATE_COLOR = {
  "ingress-only": "text-muted-foreground",
  "dual-running": "text-status-warning",
  "gateway-only": "text-status-ok",
};

export default function CoverageTable({ coverage }) {
  if (coverage.length === 0) {
    return <p className="text-sm text-muted-foreground">No hosts found across Ingress or Gateway API routes.</p>;
  }

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Host</TableHead>
          <TableHead>State</TableHead>
          <TableHead>Ingress Secret</TableHead>
          <TableHead>Gateway Secret</TableHead>
          <TableHead>Drift</TableHead>
          <TableHead>Cutover gate</TableHead>
          <TableHead>Stale Ingress</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {coverage.map((c) => (
          <TableRow key={c.host}>
            <TableCell>{c.host}</TableCell>
            <TableCell className={STATE_COLOR[c.state] ?? ""}>
              {STATE_LABEL[c.state] ?? c.state}
            </TableCell>
            <TableCell className="text-muted-foreground">{c.ingressSecret || "—"}</TableCell>
            <TableCell className="text-muted-foreground">{c.gatewaySecret || "—"}</TableCell>
            <TableCell className={c.secretDrift ? "text-status-error" : "text-muted-foreground"}>
              {c.secretDrift ? "Yes" : "No"}
            </TableCell>
            <TableCell
              className={
                c.state === "ingress-only"
                  ? "text-muted-foreground"
                  : c.cutoverReady
                    ? "text-status-ok"
                    : "text-status-error"
              }
              title={c.cutoverDetail || undefined}
            >
              {c.state === "ingress-only" ? "n/a" : c.cutoverReady ? "Ready" : "Not ready"}
            </TableCell>
            <TableCell className={c.staleIngresses?.length ? "text-status-warning" : "text-muted-foreground"}>
              {c.staleIngresses?.length ? c.staleIngresses.join(", ") : "—"}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
