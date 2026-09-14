import CTResultsTable from "@/components/ct/CTResultsTable";
import NotConnected from "@/components/layout/NotConnected";
import PageHeading from "@/components/layout/PageHeading";
import Section from "@/components/layout/Section";
import { Button } from "@/components/ui/button";
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { useCTCheck } from "@/hooks/useCTCheck";

const SINCE_OPTIONS = [
  { value: "1h", label: "Last hour" },
  { value: "24h", label: "Last 24 hours" },
  { value: "168h", label: "Last 7 days" },
  { value: "720h", label: "Last 30 days" },
];

export default function CTCheckPage({ isLive }) {
  const { domains, setDomains, since, setSince, results, error, busy, submit } = useCTCheck();

  return (
    <div>
      <PageHeading
        title="CT Check"
        description="Find recently logged certs for your domains not covered by any known Secret"
      />

      {!isLive ? (
        <NotConnected />
      ) : (
        <div className="flex max-w-2xl flex-col gap-4">
          <Section title="Check">
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor="ct-domains">
                  Domains (comma or newline separated)
                </FieldLabel>
                <Textarea
                  id="ct-domains"
                  placeholder="example.com, api.example.com"
                  value={domains}
                  onChange={(e) => setDomains(e.target.value)}
                />
              </Field>
              <Field className="w-fit">
                <FieldLabel htmlFor="ct-since">Since</FieldLabel>
                <Select value={since} onValueChange={(v) => setSince(v ?? "24h")}>
                  <SelectTrigger id="ct-since" className="w-48">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      {SINCE_OPTIONS.map((opt) => (
                        <SelectItem key={opt.value} value={opt.value}>
                          {opt.label}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </Field>
            </FieldGroup>

            {error && <p className="mt-2 text-sm text-destructive">{error}</p>}

            <div className="mt-4">
              <Button onClick={submit} disabled={busy || !domains.trim()}>
                {busy ? "Checking…" : "Check"}
              </Button>
            </div>
          </Section>

          {results && (
            <Section title="Results">
              <CTResultsTable results={results} />
            </Section>
          )}
        </div>
      )}
    </div>
  );
}
