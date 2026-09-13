import { useCallback, useEffect, useState } from "react";
import { getClusters, getEndpoints, removeCluster, removeEndpoint } from "@/lib/api";

// Drives the "Manage sources" dialog: the full cluster/endpoint lists
// (including non-removable --kubeconfig/--probe entries) plus remove
// actions. `onChanged` re-fetches whatever depends on the source list
// (certs, the certificates-page cluster filter) after a removal.
export function useSources(isLive, open, onChanged) {
  const [clusters, setClusters] = useState([]);
  const [endpoints, setEndpoints] = useState([]);
  const [error, setError] = useState("");
  const [removingKey, setRemovingKey] = useState(null);

  const reload = useCallback(() => {
    return Promise.all([getClusters(), getEndpoints()]).then(([c, e]) => {
      setClusters(c);
      setEndpoints(e);
    });
  }, []);

  useEffect(() => {
    if (isLive && open) reload().catch((err) => setError(String(err.message || err)));
  }, [isLive, open, reload]);

  function removeClusterByLabel(label) {
    setRemovingKey(`cluster:${label}`);
    setError("");
    removeCluster(label)
      .then(() => Promise.all([reload(), onChanged?.()]))
      .catch((err) => setError(String(err.message || err)))
      .finally(() => setRemovingKey(null));
  }

  function removeEndpointByHost(endpoint) {
    setRemovingKey(`endpoint:${endpoint}`);
    setError("");
    removeEndpoint(endpoint)
      .then(() => Promise.all([reload(), onChanged?.()]))
      .catch((err) => setError(String(err.message || err)))
      .finally(() => setRemovingKey(null));
  }

  return {
    clusters,
    endpoints,
    error,
    removingKey,
    removeCluster: removeClusterByLabel,
    removeEndpoint: removeEndpointByHost,
  };
}
