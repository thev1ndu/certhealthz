import HistoryDiffTable from "@/components/history/HistoryDiffTable";
import NotConnected from "@/components/layout/NotConnected";
import PageHeading from "@/components/layout/PageHeading";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { useHistory } from "@/hooks/useHistory";

export default function HistoryPage({ isLive, active }) {
  const { diff, error, busy, recordSnapshot } = useHistory(isLive, active);

  return (
    <div className="max-w-2xl">
      <PageHeading
        title="History"
        description="Record a snapshot of the current scan, and see what changed since the last one"
      />

      {!isLive ? (
        <NotConnected />
      ) : (
        <Card size="sm">
          <CardContent>
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
          </CardContent>
        </Card>
      )}
    </div>
  );
}
