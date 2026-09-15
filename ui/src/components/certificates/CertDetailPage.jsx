import { useEffect, useState } from "react";
import { ArrowLeftIcon, ArrowsClockwiseIcon, WarningIcon } from "@phosphor-icons/react";
import StatusBadge from "@/components/StatusBadge";
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
import Section from "@/components/layout/Section";
import { getCertDetail, reissueCert } from "@/lib/api";

function Field({ label, mono, full, children }) {
  return (
    <div className={full ? "sm:col-span-2" : undefined}>
      <div className="mb-1 text-[11px] text-muted-foreground">{label}</div>
      <div className={mono ? "text-sm break-all" : "text-sm break-words"}>
        {children ?? <span className="text-muted-foreground">—</span>}
      </div>
    </div>
  );
}

function TagList({ items }) {
  if (!items || items.length === 0) return <span className="text-muted-foreground">—</span>;
  return (
    <div className="flex flex-wrap gap-1.5">
      {items.map((item) => (
        <span
          key={item}
          className="border border-border bg-muted px-1.5 py-0.5 text-xs"
        >
          {item}
        </span>
      ))}
    </div>
  );
}

function Warning({ children }) {
  return <span className="text-destructive">{children}</span>;
}

function formatDate(iso) {
  if (!iso) return null;
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return null;
  return d.toLocaleString(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  });
}

export default function CertDetailPage({ id, row, onBack }) {
  const [detail, setDetail] = useState(null);
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(true);

  const [reissueOpen, setReissueOpen] = useState(false);
  const [reissuing, setReissuing] = useState(false);
  const [reissueError, setReissueError] = useState(null);
  const [reissued, setReissued] = useState(false);

  function handleReissue() {
    setReissuing(true);
    setReissueError(null);
    reissueCert(id)
      .then(() => {
        setReissued(true);
        setReissueOpen(false);
      })
      .catch((err) => setReissueError(err.message || "Failed to trigger reissue"))
      .finally(() => setReissuing(false));
  }

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    setDetail(null);
    getCertDetail(id)
      .then((d) => {
        if (!cancelled) setDetail(d);
      })
      .catch((err) => {
        if (!cancelled) setError(err.message || "Failed to load certificate detail");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [id]);

  return (
    <div style={{ "--radius": "0" }}>
      <div className="mb-6 flex items-center gap-3 border-b border-border pb-5">
        <Button variant="outline" size="icon-sm" aria-label="Back to certificates" onClick={onBack}>
          <ArrowLeftIcon />
        </Button>
        <div className="min-w-0 flex-1">
          <div className="mb-1 text-xs tracking-widest text-primary uppercase">
            Certificate detail
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="truncate font-heading text-xl font-medium tracking-tight">
              {row?.name ?? detail?.name ?? id}
            </h1>
            {row && <StatusBadge status={row.status} />}
          </div>
          {row && row.namespace !== "-" && (
            <p className="mt-1 text-xs text-muted-foreground">
              {row.source} · {row.cluster !== "-" ? `${row.cluster} · ` : ""}
              {row.namespace}
            </p>
          )}
        </div>

        {row?.source === "cert-manager" && row.status !== "ok" && (
          <Dialog open={reissueOpen} onOpenChange={setReissueOpen}>
            <DialogTrigger
              render={
                <Button variant="secondary">
                  <ArrowsClockwiseIcon data-icon="inline-start" />
                  Force reissue
                </Button>
              }
            />
            <DialogContent className="corner-ticks rounded-none sm:max-w-md">
              <DialogHeader>
                <DialogTitle>Force reissue this certificate?</DialogTitle>
                <DialogDescription>
                  This triggers a real reissuance against the live cluster — cert-manager will
                  request a new certificate from its issuer right away, rather than waiting for
                  the normal renewal window. If the issuer is ACME-based (e.g. Let's Encrypt),
                  this counts against its rate limits. The new certificate will appear here on
                  the next automatic scan.
                </DialogDescription>
              </DialogHeader>
              {reissueError && <p className="text-sm text-destructive">{reissueError}</p>}
              <DialogFooter>
                <Button variant="secondary" onClick={() => setReissueOpen(false)} disabled={reissuing}>
                  Cancel
                </Button>
                <Button onClick={handleReissue} disabled={reissuing}>
                  {reissuing ? "Triggering…" : "Force reissue"}
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        )}
      </div>

      {reissued && (
        <div className="corner-ticks relative mb-4 flex items-center gap-2 border border-status-ok/40 bg-status-ok/10 p-4 text-sm text-status-ok">
          Reissue triggered — the new certificate will appear on the next automatic scan.
        </div>
      )}

      {!loading && error && (
        <div className="corner-ticks relative flex items-center gap-2 border border-destructive/40 bg-destructive/10 p-4 text-sm text-destructive">
          <WarningIcon size={16} weight="fill" />
          {error}
        </div>
      )}

      {!loading && !error && detail && (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
          <Section title="Identity">
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <Field label="Subject" full mono>
                {detail.subject}
              </Field>
              <Field label="Issuer" full mono>
                {detail.issuer}
              </Field>
              <Field label="Serial number" full mono>
                {detail.serialNumber}
              </Field>
              <Field label="x509 version">{detail.version}</Field>
            </div>
          </Section>

          <Section title="Validity">
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <Field label="Not before">{formatDate(detail.notBefore)}</Field>
              <Field label="Not after">{formatDate(detail.notAfter)}</Field>
              <Field label="Days remaining">
                {row?.days != null ? `${row.days}d` : null}
              </Field>
              <Field label="Is CA">{detail.isCA ? "Yes" : "No"}</Field>
            </div>
          </Section>

          <Section title="Subject alternative names">
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <Field label="DNS names" full>
                <TagList items={detail.dnsNames} />
              </Field>
              <Field label="IP addresses" full>
                <TagList items={detail.ipAddresses} />
              </Field>
              <Field label="Email addresses" full>
                <TagList items={detail.emailAddresses} />
              </Field>
              <Field label="URIs" full>
                <TagList items={detail.uris} />
              </Field>
            </div>
          </Section>

          <Section title="Cryptography">
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <Field label="Signature algorithm">{detail.signatureAlgorithm}</Field>
              <Field label="Public key">
                {detail.publicKeyAlgorithm}
                {detail.publicKeyBits ? ` (${detail.publicKeyBits}-bit)` : ""}
              </Field>
              <Field label="SHA-256 fingerprint" full mono>
                {detail.fingerprintSha256}
              </Field>
              <Field label="SHA-1 fingerprint" full mono>
                {detail.fingerprintSha1}
              </Field>
              <Field label="Weak crypto" full>
                {detail.isWeakCrypto ? <Warning>Yes — {detail.cryptoIssue}</Warning> : "No"}
              </Field>
            </div>
          </Section>

          <Section title="Extensions">
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <Field label="Key usage" full>
                <TagList items={detail.keyUsage} />
              </Field>
              <Field label="Extended key usage" full>
                <TagList items={detail.extKeyUsage} />
              </Field>
            </div>
          </Section>

          <Section title="Revocation">
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <Field label="OCSP servers" full>
                <TagList items={detail.ocspServers} />
              </Field>
              <Field label="CRL distribution points" full>
                <TagList items={detail.crlDistributionPoints} />
              </Field>
            </div>
          </Section>

          {detail.backedRoutes?.length > 0 && (
            <div className="md:col-span-2">
              <Section title="Backs">
                <div className="flex flex-col gap-3">
                  {detail.backedRoutes.map((route, i) => (
                    <div key={i} className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
                      <span className="text-sm font-medium">{route.ingress}</span>
                      <TagList items={route.hosts} />
                    </div>
                  ))}
                </div>
              </Section>
            </div>
          )}

          <div className="md:col-span-2">
            <Section title="Certificate chain">
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <Field label="Chain valid" full>
                  {detail.chainValid ? "Yes" : <Warning>No — {detail.chainIssue}</Warning>}
                </Field>
              </div>
              {detail.chain?.length > 0 &&
                detail.chain.map((c, i) => (
                  <div key={i} className="mt-3 border-t border-border/60 pt-3">
                    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                      <Field label={`#${i + 1} subject`} mono>
                        {c.subject}
                      </Field>
                      <Field label={`#${i + 1} issuer`} mono>
                        {c.issuer}
                      </Field>
                      <Field label="Not after">{formatDate(c.notAfter)}</Field>
                      <Field label="Is CA">{c.isCA ? "Yes" : "No"}</Field>
                    </div>
                  </div>
                ))}
            </Section>
          </div>
        </div>
      )}
    </div>
  );
}
