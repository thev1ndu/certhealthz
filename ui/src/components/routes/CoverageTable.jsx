import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import RouteTestDialog from "@/components/routes/RouteTestDialog";

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

// gatewayRouteForHost finds the Gateway API route (not Ingress) backing a
// coverage row's host, so its cutoverReady claim can be re-verified with a
// live test — apiHostCoverage has no cluster field (it groups purely by
// hostname across every configured cluster), so this is a best-effort match
// on host alone; if more than one cluster happens to serve the same host,
// the first match wins.
function gatewayRouteForHost(routes, host) {
  return routes.find((r) => r.kind !== "ingress" && r.hosts?.includes(host));
}

export default function CoverageTable({ coverage, routes = [] }) {
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
          <TableHead className="text-right">Test</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {coverage.map((c) => {
          const route = c.state === "ingress-only" ? null : gatewayRouteForHost(routes, c.host);
          return (
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
              <TableCell className="text-right">
                {route ? (
                  <RouteTestDialog route={route} label={`${c.host} (re-verify cutover gate)`} />
                ) : (
                  <span className="text-xs text-muted-foreground">—</span>
                )}
              </TableCell>
            </TableRow>
          );
        })}
      </TableBody>
    </Table>
  );
}
