import NotConnected from "@/components/layout/NotConnected";
import PageHeading from "@/components/layout/PageHeading";
import Section from "@/components/layout/Section";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useSettings } from "@/hooks/useSettings";

export default function SettingsPage({ isLive, onSaved }) {
  const { form, error, busy, alertResult, alertBusy, updateField, submit, testAlert } =
    useSettings(isLive, onSaved);

  return (
    <div>
      <PageHeading
        title="Settings"
        description="Change scan thresholds and alerting without restarting the server"
      />

      {!isLive ? (
        <NotConnected />
      ) : (
        <div className="flex max-w-lg flex-col gap-4">
          <Section title="Scan settings">
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor="warn-days">Warn days</FieldLabel>
                <Input
                  id="warn-days"
                  type="number"
                  min={1}
                  value={form.warnDays}
                  onChange={(e) => updateField("warnDays", Number(e.target.value))}
                />
              </Field>

              <Field orientation="horizontal">
                <Checkbox
                  id="include-secrets"
                  checked={form.includeSecrets}
                  onCheckedChange={(checked) => updateField("includeSecrets", checked)}
                />
                <Label htmlFor="include-secrets" className="text-sm font-normal">
                  Scan raw Secrets (drift detection, Ingress cross-referencing)
                </Label>
              </Field>
            </FieldGroup>

            {error && <p className="mt-2 text-sm text-destructive">{error}</p>}

            <div className="mt-4">
              <Button onClick={submit} disabled={busy}>
                {busy ? "Saving…" : "Save"}
              </Button>
            </div>
          </Section>

          <Section title="Alerting">
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor="webhook-url">Webhook URL</FieldLabel>
                <Input
                  id="webhook-url"
                  placeholder="https://hooks.example.com/certhealthz"
                  value={form.webhookURL}
                  onChange={(e) => updateField("webhookURL", e.target.value)}
                />
              </Field>
            </FieldGroup>

            <div className="mt-4">
              <Button
                variant="secondary"
                onClick={testAlert}
                disabled={alertBusy || !form.webhookURL?.trim()}
              >
                {alertBusy ? "Sending…" : "Send test alert"}
              </Button>
            </div>

            {alertResult && (
              <p
                className={
                  "mt-2 text-sm " +
                  (alertResult.error ? "text-destructive" : "text-muted-foreground")
                }
              >
                {alertResult.error
                  ? alertResult.error
                  : alertResult.sent
                    ? `Sent — ${alertResult.flagged} flagged certificate(s).`
                    : "Nothing to send — 0 flagged certificates."}
              </p>
            )}
          </Section>
        </div>
      )}
    </div>
  );
}
