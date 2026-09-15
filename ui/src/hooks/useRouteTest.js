import { useState } from "react";
import { testRoute } from "@/lib/api";

// On-demand action hook for the "test this route" dialog — mirrors
// useCTCheck's {busy, error, result, submit} shape rather than useRoutes'
// load-on-mount shape, since a route test only ever runs when a user
// explicitly triggers it.
export function useRouteTest() {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [result, setResult] = useState(null);

  function submit(route) {
    setBusy(true);
    setError("");
    return testRoute(route)
      .then((res) => {
        setResult(res);
        return res;
      })
      .catch((err) => {
        setError(String(err.message || err));
      })
      .finally(() => setBusy(false));
  }

  function reset() {
    setError("");
    setResult(null);
  }

  return { busy, error, result, submit, reset };
}
