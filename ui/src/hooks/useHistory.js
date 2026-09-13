import { useEffect, useState } from "react";
import { getHistoryDiff, recordHistorySnapshot } from "@/lib/api";

// Loads the latest history diff whenever the History tab becomes active,
// and drives "Record snapshot". `diff.message` is always set by the server
// when there's nothing to show in `diff.changes` (no runs yet, only one
// run, or two runs with no changes) — render it, don't assume a message
// means an error.
export function useHistory(isLive, active) {
  const [diff, setDiff] = useState(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  function fetchDiff() {
    setBusy(true);
    setError("");
    getHistoryDiff()
      .then(setDiff)
      .catch((err) => setError(String(err.message || err)))
      .finally(() => setBusy(false));
  }

  function recordSnapshot() {
    setBusy(true);
    setError("");
    recordHistorySnapshot()
      .then(() => fetchDiff())
      .catch((err) => {
        setError(String(err.message || err));
        setBusy(false);
      });
  }

  useEffect(() => {
    if (isLive && active) fetchDiff();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isLive, active]);

  return { diff, error, busy, recordSnapshot };
}
