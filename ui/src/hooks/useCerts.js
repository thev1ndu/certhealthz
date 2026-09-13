import { useCallback, useEffect, useState } from "react";
import { getCerts } from "@/lib/api";

// Loads /api/certs on mount. `isLive` flips true on the first successful
// response — false stays the signal for "no backend wired up" (e.g.
// `npm run dev` standalone), which every tab gates its content on.
export function useCerts() {
  const [certs, setCerts] = useState([]);
  const [isLive, setIsLive] = useState(false);

  const reload = useCallback(() => {
    return getCerts().then((data) => {
      setCerts(data);
      setIsLive(true);
      return data;
    });
  }, []);

  useEffect(() => {
    reload().catch(() => {
      // no live backend — stays empty, isLive stays false
    });
  }, [reload]);

  return { certs, isLive, reload };
}
