import { useMemo } from "react";
import { useClusterList } from "@/hooks/useClusterList";

// Union of every cluster label the server knows about (explicit
// --kubeconfig/upload entries) and every cluster label actually seen in
// scanned rows — the latter catches the implicit "default" cluster a bare
// scan falls back to, which /api/clusters never lists. Shared by
// CertificatesPage and OverviewPage so both count clusters the same way.
export function useKnownClusters(certs, isLive) {
  const { serverClusters, reload } = useClusterList(isLive);

  const clusters = useMemo(() => {
    const fromRows = certs.map((r) => r.cluster).filter((c) => c !== "-");
    return [...new Set([...(serverClusters ?? []), ...fromRows])].sort();
  }, [certs, serverClusters]);

  return { clusters, reload };
}
