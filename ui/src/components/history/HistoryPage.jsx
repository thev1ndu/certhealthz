import HistoryDiffTable from "@/components/history/HistoryDiffTable";
import NotConnected from "@/components/layout/NotConnected";
import PageHeading from "@/components/layout/PageHeading";
import Section from "@/components/layout/Section";
import { Button } from "@/components/ui/button";
import { useHistory } from "@/hooks/useHistory";

export default function HistoryPage({ isLive, active }) {
  const { diff, error, busy, recordSnapshot } = useHistory(isLive, active);

  return (
    <div>
      <PageHeading
        title="History"
        description="Record a snapshot of the current scan, and see what changed since the last one"
      />

      {!isLive ? (
        <NotConnected />
      ) : (
        <div className="max-w-2xl">
          <Section title="Snapshot">
            <Button onClick={recordSnapshot} disabled={busy}>
              {busy ? "Working…" : "Record snapshot"}
            </Button>

            {error && <p className="mt-2 text-sm text-destructive">{error}</p>}

            {diff && (
              <div className="mt-4">
                {diff.message && (
                  <p className="mb-2 text-sm text-muted-foreground">{diff.message}</p>
                )}
                {diff.changes?.length > 0 && <HistoryDiffTable changes={diff.changes} />}
              </div>
            )}
          </Section>
        </div>
      )}
    </div>
  );
}
