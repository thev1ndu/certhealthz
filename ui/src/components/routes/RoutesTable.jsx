import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

const KIND_LABEL = {
  ingress: "Ingress",
  httproute: "HTTPRoute",
  grpcroute: "GRPCRoute",
  tlsroute: "TLSRoute",
};

export default function RoutesTable({ routes }) {
  if (routes.length === 0) {
    return <p className="text-sm text-muted-foreground">No Ingress or Gateway API routes found.</p>;
  }

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Kind</TableHead>
          <TableHead>Cluster</TableHead>
          <TableHead>Namespace</TableHead>
          <TableHead>Name</TableHead>
          <TableHead>Gateway</TableHead>
          <TableHead>Secret</TableHead>
          <TableHead>Hosts</TableHead>
          <TableHead>Accepted</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {routes.map((r, i) => (
          <TableRow key={`${r.kind}-${r.cluster}-${r.namespace}-${r.name}-${i}`}>
            <TableCell>{KIND_LABEL[r.kind] ?? r.kind}</TableCell>
            <TableCell className="text-muted-foreground">{r.cluster}</TableCell>
            <TableCell className="text-muted-foreground">{r.namespace}</TableCell>
            <TableCell>{r.name}</TableCell>
            <TableCell className="text-muted-foreground">
              {r.gatewayName ? `${r.gatewayNamespace}/${r.gatewayName}` : "—"}
            </TableCell>
            <TableCell className="text-muted-foreground">
              {r.secretName ? `${r.secretNamespace}/${r.secretName}` : "—"}
            </TableCell>
            <TableCell className="text-muted-foreground">
              {r.hosts && r.hosts.length > 0 ? r.hosts.join(", ") : "—"}
            </TableCell>
            <TableCell className={r.accepted ? "text-status-ok" : "text-status-error"}>
              {r.accepted ? "Yes" : "No"}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
