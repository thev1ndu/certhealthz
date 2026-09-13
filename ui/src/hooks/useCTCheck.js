import { useState } from "react";
import { checkCT } from "@/lib/api";

export function useCTCheck() {
  const [domains, setDomains] = useState("");
  const [since, setSince] = useState("24h");
  const [results, setResults] = useState(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  function submit() {
    const domainList = domains
      .split(/[,\n]/)
      .map((d) => d.trim())
      .filter(Boolean);
    if (domainList.length === 0) {
      setError("Enter at least one domain");
      return;
    }
    setBusy(true);
    setError("");
    checkCT(domainList, since)
      .then(setResults)
      .catch((err) => setError(String(err.message || err)))
      .finally(() => setBusy(false));
  }

  return { domains, setDomains, since, setSince, results, error, busy, submit };
}
