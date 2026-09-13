import { useEffect, useState } from "react";
import { getSettings, saveSettings, sendTestAlert } from "@/lib/api";

const DEFAULT_FORM = { warnDays: 14, includeSecrets: true, webhookURL: "" };

// Loads/saves the live-editable scan settings, plus the "send test alert"
// action. `onSaved` runs after a successful save so the caller can refresh
// anything derived from settings (e.g. re-scan with the new warnDays).
export function useSettings(isLive, onSaved) {
  const [form, setForm] = useState(DEFAULT_FORM);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [alertResult, setAlertResult] = useState(null);
  const [alertBusy, setAlertBusy] = useState(false);

  useEffect(() => {
    if (!isLive) return;
    getSettings()
      .then(setForm)
      .catch(() => {
        // settings just keeps its defaults if this fails
      });
  }, [isLive]);

  function updateField(field, value) {
    setForm((prev) => ({ ...prev, [field]: value }));
  }

  function submit() {
    setBusy(true);
    setError("");
    saveSettings(form)
      .then((data) => {
        setForm(data);
        return onSaved?.();
      })
      .catch((err) => setError(String(err.message || err)))
      .finally(() => setBusy(false));
  }

  function testAlert() {
    setAlertBusy(true);
    setAlertResult(null);
    sendTestAlert()
      .then(setAlertResult)
      .catch((err) => setAlertResult({ error: String(err.message || err) }))
      .finally(() => setAlertBusy(false));
  }

  return { form, error, busy, alertResult, alertBusy, updateField, submit, testAlert };
}
