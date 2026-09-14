import { useEffect, useState } from "react";
import { ArrowLeftIcon, WarningIcon } from "@phosphor-icons/react";
import StatusBadge from "@/components/StatusBadge";
import { Button } from "@/components/ui/button";
import Section from "@/components/layout/Section";
import { getCertDetail } from "@/lib/api";

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
      </div>

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

          {detail.chain?.length > 0 && (
            <div className="md:col-span-2">
              <Section title="Certificate chain">
                {detail.chain.map((c, i) => (
                  <div key={i} className="border-t border-border/60 pt-3 first:border-0 first:pt-0">
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
          )}
        </div>
      )}
    </div>
  );
}
