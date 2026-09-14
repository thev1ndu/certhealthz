import HistoryEventList from "@/components/history/HistoryEventList";
import NotConnected from "@/components/layout/NotConnected";
import PageHeading from "@/components/layout/PageHeading";
import Section from "@/components/layout/Section";
import { Button } from "@/components/ui/button";
import { useHistoryEvents } from "@/hooks/useHistoryEvents";

export default function HistoryPage({ isLive, active }) {
  const { events, hasMore, error, loading, loadingMore, loadMore } = useHistoryEvents(
    isLive,
    active,
  );

  return (
    <div>
      <PageHeading
        title="History"
        description="Audit log of every certificate change detected — recorded automatically, no manual snapshots"
      />

      {!isLive ? (
        <NotConnected />
      ) : (
        <div className="max-w-2xl">
          <Section title="Audit log">
            {error && <p className="mb-3 text-sm text-destructive">{error}</p>}

            {loading && <p className="text-sm text-muted-foreground">Loading…</p>}

            {!loading && events.length === 0 && !error && (
              <p className="text-sm text-muted-foreground">
                No changes recorded yet — the first automatic scan runs shortly after startup.
              </p>
            )}

            {!loading && events.length > 0 && <HistoryEventList events={events} />}

            {hasMore && (
              <div className="mt-4">
                <Button variant="outline" onClick={loadMore} disabled={loadingMore}>
                  {loadingMore ? "Loading…" : "Load more"}
                </Button>
              </div>
            )}
          </Section>
        </div>
      )}
    </div>
  );
}
