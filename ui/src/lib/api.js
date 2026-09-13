// Thin wrappers around the dashboard's /api/* endpoints (see
// cmd/ui.go's NewUIMux). Every function returns parsed JSON and throws an
// Error (with the server's response body as the message) on a non-2xx
// response, so callers can rely on try/catch or .catch() uniformly.

async function request(path, options) {
  const res = await fetch(path, options);
  if (!res.ok) {
    throw new Error((await res.text()) || `${path} failed: ${res.status}`);
  }
  if (res.status === 204) return null;
  return res.json();
}

function postJSON(path, body) {
  return request(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}

export function getCerts() {
  return request("/api/certs");
}

export function getClusters() {
  return request("/api/clusters");
}

export function addCluster(kubeconfigFile) {
  const body = new FormData();
  body.append("kubeconfig", kubeconfigFile);
  return request("/api/clusters", { method: "POST", body });
}

export function addEndpoint(endpoint) {
  return postJSON("/api/endpoints", { endpoint });
}

export function getSettings() {
  return request("/api/settings");
}

export function saveSettings(settings) {
  return postJSON("/api/settings", settings);
}

export function sendTestAlert() {
  return request("/api/alert", { method: "POST" });
}

export function getHistoryDiff() {
  return request("/api/history/diff");
}

export function recordHistorySnapshot() {
  return request("/api/history/record", { method: "POST" });
}

export function checkCT(domains, since) {
  return postJSON("/api/ct", { domains, since });
}
