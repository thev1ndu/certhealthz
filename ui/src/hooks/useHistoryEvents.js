import { useEffect, useState } from "react";
import { getHistoryEvents } from "@/lib/api";

const PAGE_SIZE = 50;

// Loads the audit-trail feed when the History tab becomes active. Recording
// itself is fully automatic (see cmd/ui.go's autoRecordHistory) — this hook
// only ever reads, plus supports "load more" via the server's `before`
// pagination cursor.
export function useHistoryEvents(isLive, active) {
  const [events, setEvents] = useState([]);
  const [nextBefore, setNextBefore] = useState(0);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);

  function load() {
    setLoading(true);
    setError("");
    getHistoryEvents({ limit: PAGE_SIZE })
      .then((resp) => {
        setEvents(resp.events ?? []);
        setNextBefore(resp.nextBefore ?? 0);
      })
      .catch((err) => setError(String(err.message || err)))
      .finally(() => setLoading(false));
  }

  function loadMore() {
    if (!nextBefore || loadingMore) return;
    setLoadingMore(true);
    getHistoryEvents({ limit: PAGE_SIZE, before: nextBefore })
      .then((resp) => {
        setEvents((prev) => [...prev, ...(resp.events ?? [])]);
        setNextBefore(resp.nextBefore ?? 0);
      })
      .catch((err) => setError(String(err.message || err)))
      .finally(() => setLoadingMore(false));
  }

  useEffect(() => {
    if (isLive && active) load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isLive, active]);

  return { events, hasMore: nextBefore !== 0, error, loading, loadingMore, loadMore, reload: load };
}
