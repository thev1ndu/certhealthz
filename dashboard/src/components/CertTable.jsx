import { useCallback, useMemo, useRef, useState } from "react";
import { Badge, Button, Empty, Table, Text, Tooltip } from "@cloudflare/kumo";
import {
  CaretDownIcon,
  CaretUpDownIcon,
  CaretUpIcon,
  InfoIcon,
  MagnifyingGlassIcon,
} from "@phosphor-icons/react";

// Kumo Badge `appearance="dot"` only renders a dot for these four variants.
export const STATUS_BADGE = {
  ok: "success",
  expiring: "warning",
  expired: "error",
  error: "neutral",
};

export const STATUS_LABEL = {
  ok: "Healthy",
  expiring: "Expiring",
  expired: "Expired",
  error: "Error",
};

const COLUMNS = [
  {
    id: "name",
    label: "Name",
    width: 240,
    min: 160,
    description:
      "The certificate's name, from cert-manager, its Secret, or the scanned endpoint.",
  },
  {
    id: "source",
    label: "Source",
    width: 140,
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
    width: 140,
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

function SortHead({ column, sort, onSort }) {
  const active = sort.id === column.id;
  const Caret = !active
    ? CaretUpDownIcon
    : sort.dir === "asc"
      ? CaretUpIcon
      : CaretDownIcon;

  return (
    <Tooltip content={column.description}>
      <Button
        variant="ghost"
        size="xs"
        onClick={() => onSort(column.id)}
        aria-label={`Sort by ${column.label}`}
      >
        {column.label}
        <Caret
          size={12}
          weight="bold"
          className={active ? "text-kumo-link" : "text-kumo-inactive"}
        />
      </Button>
    </Tooltip>
  );
}

export default function CertTable({ rows, selected, onToggle, onToggleAll }) {
  const allSelected = rows.length > 0 && selected.size === rows.length;
  const someSelected = selected.size > 0 && selected.size < rows.length;
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
      <Table layout="fixed">
        <colgroup>
          <col style={{ width: 44 }} />
          <col style={{ width: 40 }} />
          {COLUMNS.map((c) => (
            <col key={c.id} style={{ width: widths[c.id] }} />
          ))}
        </colgroup>
        <Table.Header>
          <Table.Row>
            <Table.CheckHead
              checked={allSelected}
              indeterminate={someSelected}
              onCheckedChange={onToggleAll}
              aria-label="Select all certificates"
            />
            <Table.Head />
            {COLUMNS.map((c) => (
              <Table.Head key={c.id}>
                <SortHead column={c} sort={sort} onSort={handleSort} />
                <Table.ResizeHandle
                  onMouseDown={onResizeStart(c.id, c.min)}
                  onTouchStart={onResizeStart(c.id, c.min)}
                />
              </Table.Head>
            ))}
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {sortedRows.map((row) => (
            <Table.Row
              key={row.id}
              variant={selected.has(row.id) ? "selected" : "default"}
            >
              <Table.CheckCell
                checked={selected.has(row.id)}
                onCheckedChange={() => onToggle(row.id)}
                aria-label={`Select ${row.name}`}
              />
              <Table.Cell>
                <Tooltip content={row.detail || "No additional detail"}>
                  <span className="inline-flex text-kumo-link">
                    <InfoIcon size={15} weight="fill" />
                  </span>
                </Tooltip>
              </Table.Cell>
              <Table.Cell>
                <div className="flex flex-col">
                  <Text as="span" size="sm" bold truncate>
                    {row.name}
                  </Text>
                  {row.namespace !== "-" && (
                    <Text as="span" variant="mono-secondary" size="xs" truncate>
                      /{row.namespace}
                    </Text>
                  )}
                </div>
              </Table.Cell>
              <Table.Cell>
                <Text as="span" variant="mono-secondary" size="sm">
                  {row.source}
                </Text>
              </Table.Cell>
              <Table.Cell>
                <Text
                  as="span"
                  size="sm"
                  variant={row.cluster === "-" ? "secondary" : "body"}
                >
                  {row.cluster}
                </Text>
              </Table.Cell>
              <Table.Cell>
                <Badge variant={STATUS_BADGE[row.status]} appearance="dot">
                  {STATUS_LABEL[row.status]}
                </Badge>
              </Table.Cell>
              <Table.Cell>
                <Text
                  as="span"
                  variant="mono"
                  size="sm"
                  DANGEROUS_className="tabular-nums"
                >
                  {row.days === null ? "—" : `${row.days}d`}
                </Text>
              </Table.Cell>
            </Table.Row>
          ))}
          {sortedRows.length === 0 && (
            <Table.Row>
              <Table.Cell colSpan={COLUMNS.length + 2}>
                <Empty
                  size="sm"
                  icon={<MagnifyingGlassIcon size={32} />}
                  title="No certificates match this filter"
                  description="Try a different cluster, namespace, or certificate name."
                />
              </Table.Cell>
            </Table.Row>
          )}
        </Table.Body>
      </Table>
    </div>
  );
}
