import { useState } from "react";
import { addEndpoint } from "@/lib/api";

// Drives the "Add endpoint" popover, mirroring useAddCluster's shape.
export function useAddEndpoint(onAdded) {
  const [open, setOpen] = useState(false);
  const [value, setValue] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  function reset() {
    setValue("");
    setError("");
  }

  function openChange(next) {
    setOpen(next);
    if (!next) reset();
  }

  function changeValue(next) {
    setValue(next);
    setError("");
  }

  function submit() {
    const endpoint = value.trim();
    if (!endpoint) {
      setError("Enter a host or host:port");
      return;
    }
    setBusy(true);
    setError("");
    addEndpoint(endpoint)
      .then(() => onAdded())
      .then(() => {
        setOpen(false);
        reset();
      })
      .catch((err) => setError(String(err.message || err)))
      .finally(() => setBusy(false));
  }

  return { open, value, error, busy, openChange, changeValue, submit };
}
