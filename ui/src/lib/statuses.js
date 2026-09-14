// Fixed display order for status filters/summaries: most urgent first.
export const STATUS_ORDER = ["expired", "drift", "expiring", "ok", "error"];

// Status tiers that mean "someone should look at this" — everything except
// a plain healthy cert. Used to build "needs attention" lists.
export const ATTENTION_STATUSES = new Set(["expired", "drift", "expiring", "error"]);

// [status, count] pairs in STATUS_ORDER, statuses with zero rows omitted.
// Shared by every page that shows a status summary (Overview, Certificates)
// so the tiers/ordering never drift apart.
export function summarizeStatuses(rows) {
  const counts = {};
  for (const row of rows) counts[row.status] = (counts[row.status] ?? 0) + 1;
  return STATUS_ORDER.filter((s) => counts[s]).map((s) => [s, counts[s]]);
}
