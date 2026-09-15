import { useState } from "react";
import { PlugsConnectedIcon } from "@phosphor-icons/react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { useRouteTest } from "@/hooks/useRouteTest";

const STEP_LABEL = {
  resolve_address: "Resolve address",
  tcp_connect: "TCP connect",
  tls_handshake: "TLS handshake",
  cert_match: "Certificate match",
  http_request: "HTTP request",
};

const VIA_LABEL = {
  direct: "Tested via a direct connection to a published LB/Gateway address.",
  portforward: "Tested via a port-forward tunnel to the controller's Service.",
  unreachable: "Not tested — no reachable address was found for this route.",
};

function StepRow({ step }) {
  return (
    <div className="flex items-start justify-between gap-4 border-b border-border py-2 last:border-b-0">
      <div className="flex items-center gap-2 pt-0.5">
        <span
          className="size-1.5 shrink-0 rounded-full"
          style={{ backgroundColor: step.ok ? "var(--status-ok)" : "var(--status-error)" }}
        />
        <span className="text-sm">{STEP_LABEL[step.name] ?? step.name}</span>
      </div>
      <div className="max-w-[60%] text-right">
        <div
          className="text-xs font-medium"
          style={{ color: step.ok ? "var(--status-ok)" : "var(--status-error)" }}
        >
          {step.ok ? "Pass" : "Fail"}
          {typeof step.durationMs === "number" && step.durationMs > 0 ? ` · ${step.durationMs}ms` : ""}
        </div>
        {step.detail && (
          <div className="mt-0.5 text-xs break-words text-muted-foreground">{step.detail}</div>
        )}
      </div>
    </div>
  );
}

// RouteTestDialog is the shared "test this route" trigger + confirm/result
// dialog, used from both RoutesTable (every route row) and CoverageTable
// (a coverage row's re-verifiable cutoverReady claim) — same hook, same
// dialog, just a different caller-supplied route identity and label.
//
// `route` must carry {cluster, namespace, kind, name} — the same identity
// fields apiRoute already exposes, echoed straight back to
// POST /api/routes/test (see ui/src/lib/api.js's testRoute).
export default function RouteTestDialog({ route, label, disabled, buttonSize = "sm" }) {
  const [open, setOpen] = useState(false);
  const { busy, error, result, submit, reset } = useRouteTest();

  function handleOpenChange(next) {
    setOpen(next);
    if (next) {
      submit(route);
    } else {
      reset();
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger
        render={
          <Button variant="secondary" size={buttonSize} disabled={disabled}>
            <PlugsConnectedIcon data-icon="inline-start" />
            Test
          </Button>
        }
      />
      <DialogContent className="corner-ticks rounded-none sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Test route</DialogTitle>
          <DialogDescription>
            {label ?? `${route.namespace}/${route.name}`} — a live TCP connect, TLS handshake,
            certificate comparison, and HTTP request run on demand against this route.
          </DialogDescription>
        </DialogHeader>

        {busy && <p className="text-sm text-muted-foreground">Testing…</p>}
        {error && <p className="text-sm text-destructive">{error}</p>}

        {result && !busy && (
          <div className="flex flex-col gap-2">
            <p className="text-xs text-muted-foreground">{VIA_LABEL[result.via] ?? result.via}</p>
            <div>
              {result.steps.map((step) => (
                <StepRow key={step.name} step={step} />
              ))}
            </div>
            <p
              className="text-sm font-medium"
              style={{ color: result.ok ? "var(--status-ok)" : "var(--status-error)" }}
            >
              {result.ok ? "All checks passed" : "One or more checks failed"}
            </p>
          </div>
        )}

        <DialogFooter>
          <Button variant="secondary" onClick={() => handleOpenChange(false)}>
            Close
          </Button>
          <Button onClick={() => submit(route)} disabled={busy}>
            {busy ? "Testing…" : "Retest"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
