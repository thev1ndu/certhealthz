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

export function getCertDetail(id) {
  return request(`/api/certs/detail?id=${encodeURIComponent(id)}`);
}

export function reissueCert(id) {
  return request(`/api/certs/reissue?id=${encodeURIComponent(id)}`, { method: "POST" });
}

export function getClusters() {
  return request("/api/clusters");
}

export function addCluster(kubeconfigFile) {
  const body = new FormData();
  body.append("kubeconfig", kubeconfigFile);
  return request("/api/clusters", { method: "POST", body });
}

export function removeCluster(label) {
  return request(`/api/clusters/${encodeURIComponent(label)}`, { method: "DELETE" });
}

export function getEndpoints() {
  return request("/api/endpoints");
}

export function addEndpoint(endpoint) {
  return postJSON("/api/endpoints", { endpoint });
}

export function removeEndpoint(endpoint) {
  return request(`/api/endpoints/${encodeURIComponent(endpoint)}`, { method: "DELETE" });
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

export function getHistoryEvents({ limit, before } = {}) {
  const params = new URLSearchParams();
  if (limit) params.set("limit", limit);
  if (before) params.set("before", before);
  const qs = params.toString();
  return request(`/api/history/events${qs ? `?${qs}` : ""}`);
}

export function checkCT(domains, since) {
  return postJSON("/api/ct", { domains, since });
}

export function getRoutes() {
  return request("/api/routes");
}

export function getRouteCoverage() {
  return request("/api/routes/coverage");
}
