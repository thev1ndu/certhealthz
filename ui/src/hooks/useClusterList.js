import { useCallback, useEffect, useState } from "react";
import { getClusters } from "@/lib/api";

// The explicit --kubeconfig/upload cluster labels the server knows about.
// Only meaningful once connected to a live backend.
export function useClusterList(isLive) {
  const [serverClusters, setServerClusters] = useState(null);

  const reload = useCallback(() => {
    return getClusters()
      .then(setServerClusters)
      .catch(() => {
        // filter dropdown just falls back to clusters seen in cert rows
      });
  }, []);

  useEffect(() => {
    if (isLive) reload();
  }, [isLive, reload]);

  return { serverClusters, reload };
}
