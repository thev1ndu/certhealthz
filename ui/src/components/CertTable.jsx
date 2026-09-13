import { useCallback, useMemo, useRef, useState } from "react";
import {
  CaretDownIcon,
  CaretUpDownIcon,
  CaretUpIcon,
  InfoIcon,
  MagnifyingGlassIcon,
} from "@phosphor-icons/react";
import StatusBadge, { STATUS_LABEL } from "@/components/StatusBadge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { Empty, EmptyDescription, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { cn } from "@/lib/utils";

const COLUMNS = [
  {
    id: "name",
    label: "Name",
    width: 260,
    min: 160,
    description:
      "The certificate's name, from cert-manager, its Secret, or the scanned endpoint.",
  },
  {
    id: "source",
    label: "Source",
    width: 130,
    min: 100,
    description:
      "Where certhealthz found this certificate: a cert-manager Certificate, a raw Secret, or a live TLS endpoint probe.",
  },
  {
    id: "cluster",
    label: "Cluster",
    width: 140,
    min: 100,
    description:
      "The Kubernetes cluster this row was scanned from. Endpoint probes aren't tied to a cluster.",
  },
  {
    id: "status",
    label: "Status",
    width: 130,
    min: 110,
    description:
      "Health tier based on days remaining: Healthy, Expiring soon, Expired, or a scan/renewal Error.",
  },
  {
    id: "expiry",
    label: "Expiry",
    width: 100,
    min: 80,
    description: "Days remaining until the certificate's notAfter date.",
  },
];

const SORT_ACCESSORS = {
  name: (r) => r.name.toLowerCase(),
  source: (r) => r.source,
  cluster: (r) => r.cluster,
  status: (r) => r.status,
  expiry: (r) => (r.days === null ? Number.POSITIVE_INFINITY : r.days),
};

function useColumnWidths() {
  const [widths, setWidths] = useState(() =>
    Object.fromEntries(COLUMNS.map((c) => [c.id, c.width])),
  );
  const dragRef = useRef(null);

  const onResizeStart = useCallback(
    (columnId, min) => (e) => {
      e.preventDefault();
      e.stopPropagation();
      const point = e.touches?.[0] ?? e;
      dragRef.current = {
        columnId,
        startX: point.clientX,
        startWidth: widths[columnId],
        min,
      };

      function onMove(ev) {
        const { columnId, startX, startWidth, min } = dragRef.current;
        const p = ev.touches?.[0] ?? ev;
        const next = Math.max(min, startWidth + (p.clientX - startX));
        setWidths((prev) => ({ ...prev, [columnId]: next }));
      }
      function onUp() {
        dragRef.current = null;
        window.removeEventListener("mousemove", onMove);
        window.removeEventListener("mouseup", onUp);
        window.removeEventListener("touchmove", onMove);
        window.removeEventListener("touchend", onUp);
      }
      window.addEventListener("mousemove", onMove);
      window.addEventListener("mouseup", onUp);
      window.addEventListener("touchmove", onMove);
      window.addEventListener("touchend", onUp);
    },
    [widths],
  );

  return { widths, onResizeStart };
}

function ResizeHandle({ onMouseDown, onTouchStart }) {
  return (
    <span
      onMouseDown={onMouseDown}
      onTouchStart={onTouchStart}
      className="absolute inset-y-0 right-0 z-10 w-2 cursor-col-resize touch-none select-none after:absolute after:inset-y-1 after:right-[3px] after:w-px after:bg-border hover:after:bg-primary"
    />
  );
}

function SortHead({ column, sort, onSort }) {
  const active = sort.id === column.id;
  const Caret = !active
    ? CaretUpDownIcon
    : sort.dir === "asc"
      ? CaretUpIcon
      : CaretDownIcon;

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <button
            type="button"
            onClick={() => onSort(column.id)}
            aria-label={`Sort by ${column.label}`}
            className="inline-flex items-center gap-1 font-mono text-[11px] font-medium tracking-wider text-muted-foreground uppercase hover:text-foreground"
          />
        }
      >
        {column.label}
        <Caret
          size={12}
          weight="bold"
          className={active ? "text-primary" : "text-muted-foreground/50"}
        />
      </TooltipTrigger>
      <TooltipContent>{column.description}</TooltipContent>
    </Tooltip>
  );
}

export default function CertTable({ rows }) {
  const { widths, onResizeStart } = useColumnWidths();
  const [sort, setSort] = useState({ id: null, dir: "asc" });

  function handleSort(id) {
    setSort((prev) => {
      if (prev.id !== id) return { id, dir: "asc" };
      if (prev.dir === "asc") return { id, dir: "desc" };
      return { id: null, dir: "asc" };
    });
  }

  const sortedRows = useMemo(() => {
    if (!sort.id) return rows;
    const acc = SORT_ACCESSORS[sort.id];
    return [...rows].sort((a, b) => {
      const av = acc(a);
      const bv = acc(b);
      if (typeof av === "number") return sort.dir === "asc" ? av - bv : bv - av;
      const cmp = String(av).localeCompare(String(bv));
      return sort.dir === "asc" ? cmp : -cmp;
    });
  }, [rows, sort]);

  return (
    <div className="overflow-x-auto">
      <Table className="table-fixed">
        <colgroup>
          <col style={{ width: 36 }} />
          {COLUMNS.map((c) => (
            <col key={c.id} style={{ width: widths[c.id] }} />
          ))}
        </colgroup>
        <TableHeader>
          <TableRow className="border-border hover:bg-transparent">
            <TableHead />
            {COLUMNS.map((c) => (
              <TableHead key={c.id} className="relative h-9 px-3">
                <SortHead column={c} sort={sort} onSort={handleSort} />
                <ResizeHandle
                  onMouseDown={onResizeStart(c.id, c.min)}
                  onTouchStart={onResizeStart(c.id, c.min)}
                />
              </TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {sortedRows.map((row) => (
            <TableRow key={row.id} className="border-border/60">
              <TableCell className="px-3">
                <Tooltip>
                  <TooltipTrigger
                    render={
                      <span className="inline-flex text-primary/70 hover:text-primary" />
                    }
                  >
                    <InfoIcon size={14} weight="fill" />
                  </TooltipTrigger>
                  <TooltipContent>{row.detail || "No additional detail"}</TooltipContent>
                </Tooltip>
              </TableCell>
              <TableCell className="px-3">
                <div className="flex flex-col gap-0.5">
                  <span className="truncate text-sm font-medium">{row.name}</span>
                  {row.namespace !== "-" && (
                    <span className="truncate font-mono text-xs text-muted-foreground">
                      /{row.namespace}
                    </span>
                  )}
                </div>
              </TableCell>
              <TableCell className="px-3">
                <span className="font-mono text-xs text-muted-foreground">{row.source}</span>
              </TableCell>
              <TableCell className="px-3">
                <span
                  className={cn(
                    "text-sm",
                    row.cluster === "-" ? "text-muted-foreground" : "text-foreground",
                  )}
                >
                  {row.cluster}
                </span>
              </TableCell>
              <TableCell className="px-3">
                <StatusBadge status={row.status} />
              </TableCell>
              <TableCell className="px-3">
                <span className="font-mono text-sm tabular-nums">
                  {row.days === null ? "—" : `${row.days}d`}
                </span>
              </TableCell>
            </TableRow>
          ))}
          {sortedRows.length === 0 && (
            <TableRow className="hover:bg-transparent">
              <TableCell colSpan={COLUMNS.length + 1} className="py-2">
                <Empty className="border-0 p-10">
                  <EmptyMedia variant="icon">
                    <MagnifyingGlassIcon size={20} />
                  </EmptyMedia>
                  <EmptyTitle>No certificates match this filter</EmptyTitle>
                  <EmptyDescription>
                    Try a different cluster, namespace, or certificate name.
                  </EmptyDescription>
                </Empty>
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
    </div>
  );
}
