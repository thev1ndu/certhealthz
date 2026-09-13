import { useState } from "react";
import { addCluster } from "@/lib/api";

// Drives the "Add cluster" popover: file selection/drag state, submit, and
// resetting back to empty whenever the popover closes. `onAdded` re-fetches
// whatever depends on the cluster list (certs, cluster labels).
export function useAddCluster(onAdded) {
  const [open, setOpen] = useState(false);
  const [file, setFile] = useState(null);
  const [dragOver, setDragOver] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  function reset() {
    setFile(null);
    setDragOver(false);
    setError("");
  }

  function openChange(next) {
    setOpen(next);
    if (!next) reset();
  }

  function pickFile(next) {
    setFile(next);
    setError("");
  }

  function submit() {
    if (!file) {
      setError("Choose a kubeconfig file");
      return;
    }
    setBusy(true);
    setError("");
    addCluster(file)
      .then(() => onAdded())
      .then(() => {
        setOpen(false);
        reset();
      })
      .catch((err) => setError(String(err.message || err)))
      .finally(() => setBusy(false));
  }

  return { open, file, dragOver, error, busy, setDragOver, openChange, pickFile, submit };
}
