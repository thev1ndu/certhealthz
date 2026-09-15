import { useCallback, useEffect, useState } from "react";
import { getRoutes, getRouteCoverage } from "@/lib/api";

// Loads /api/routes and /api/routes/coverage on mount. `isLive` flips true
// on the first successful response pair — false stays the signal for "no
// live backend wired up" (e.g. `npm run dev` standalone), the same shape
// useCerts uses for the certificates tab.
export function useRoutes() {
  const [routes, setRoutes] = useState([]);
  const [coverage, setCoverage] = useState([]);
  const [isLive, setIsLive] = useState(false);

  const reload = useCallback(() => {
    return Promise.all([getRoutes(), getRouteCoverage()]).then(([routesData, coverageData]) => {
      setRoutes(routesData);
      setCoverage(coverageData);
      setIsLive(true);
      return { routes: routesData, coverage: coverageData };
    });
  }, []);

  useEffect(() => {
    reload().catch(() => {
      // no live backend — stays empty, isLive stays false
    });
  }, [reload]);

  return { routes, coverage, isLive, reload };
}
