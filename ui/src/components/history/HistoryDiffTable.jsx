import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

export default function HistoryDiffTable({ changes }) {
  return (
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
        {changes.map((c, i) => (
          <TableRow key={i}>
            <TableCell className="text-xs uppercase">{c.kind}</TableCell>
            <TableCell className="text-xs text-muted-foreground">{c.source}</TableCell>
            <TableCell>{c.name}</TableCell>
            <TableCell className="text-xs">{c.fromState || "—"}</TableCell>
            <TableCell className="text-xs">{c.toState || "—"}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
