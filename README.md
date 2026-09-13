# CertHealthz

[![CI](https://github.com/thev1ndu/certhealthz/actions/workflows/ci.yml/badge.svg)](https://github.com/thev1ndu/certhealthz/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/thev1ndu/certhealthz.svg)](https://pkg.go.dev/github.com/thev1ndu/certhealthz)
[![Go Report Card](https://goreportcard.com/badge/github.com/thev1ndu/certhealthz)](https://goreportcard.com/report/github.com/thev1ndu/certhealthz)
[![Release](https://img.shields.io/github/v/release/thev1ndu/certhealthz)](https://github.com/thev1ndu/certhealthz/releases)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

Multi-cluster TLS/certificate expiry radar. Single Go binary, no CRD, no
in-cluster install — point it at kubeconfigs and/or live endpoints and get
a unified expiry report.

## Why

- cert-manager tracks its own `Certificate` objects, but says nothing about
  raw TLS endpoints (load balancers, vendor APIs, legacy certs) it doesn't manage.
- A `Certificate` can report `Ready` while the backing `Secret`'s actual leaf
  cert is stale — renewal silently broke and nobody noticed.
- Multi-cluster orgs have no single place to see "what expires next, everywhere."

CertHealthz scans all three sources — cert-manager `Certificate` objects, raw
`kubernetes.io/tls` Secrets, and live TLS endpoints — and reports them in one
sorted-by-urgency table.

## Comparison

| Tool | Cert-manager CR aware | Raw Secret drift check | Live endpoint probe | Multi-cluster | History/trend diff | Dashboard UI | Single binary, no install | Alerting |
|---|---|---|---|---|---|---|---|---|
| **CertHealthz** | ✅ | ✅ | ✅ | ✅ | ✅ (SQLite) | ✅ (embedded) | ✅ | ✅ (webhook) |
| [x509-certificate-exporter](https://github.com/enix/x509-certificate-exporter) | ✅ | ✅ (Secrets scan) | ❌ | ✅ (via Prometheus federation) | ❌ (point-in-time metrics) | ❌ (Grafana-dependent) | ❌ (in-cluster Deployment) | via Alertmanager |
| [ssl_exporter](https://github.com/ribbybibby/ssl_exporter) | ❌ | ❌ | ✅ | N/A (probes targets, cluster-agnostic) | ❌ | ❌ (Grafana-dependent) | ✅ (binary, but needs Prometheus+Grafana around it) | via Alertmanager |
| [testssl.sh](https://github.com/drwetter/testssl.sh) | ❌ | ❌ | ✅ (deep TLS/cipher audit) | ❌ | ❌ | ❌ | ✅ (shell script) | ❌ |
| [sslyze](https://github.com/nabla-c0d3/sslyze) | ❌ | ❌ | ✅ (deep TLS audit) | ❌ | ❌ | ❌ | ✅ (CLI/lib) | ❌ |
| [Uptime Kuma](https://github.com/louislam/uptime-kuma) | ❌ | ❌ | ✅ (basic expiry check) | ❌ | ❌ (uptime history, not cert trend) | ✅ | ❌ (needs a running server/DB) | ✅ (many channels) |
| cert-manager itself | ✅ (source of truth) | ❌ | ❌ | per-cluster only | ❌ | ❌ | ❌ (controller, not a query tool) | ❌ |

CertHealthz is the only one that cross-checks cert-manager's `Ready` status
against the backing Secret's actual leaf cert (catching silent renewal drift)
while staying a zero-install single binary — the exporters need a
Prometheus+Grafana stack, Uptime Kuma needs its own server, and
testssl.sh/sslyze are one-shot scanners with no cluster or history awareness.

## Install

```sh
go install github.com/thev1ndu/certhealthz@latest
```

Or download a prebuilt binary from the [releases page](https://github.com/thev1ndu/certhealthz/releases) (linux/darwin/windows, amd64/arm64).

Or build from source:

```sh
go build -o certhealthz .
```

The `dashboard` command serves a bundled web UI (see [dashboard/](dashboard/)).
`go build` alone embeds a placeholder page for it; run `make dashboard-build`
first to build the real UI and embed it into the binary, then `make build`.

## Usage

Scan cert-manager + Secrets across one or more clusters:

```sh
certhealthz scan --kubeconfig ~/.kube/prod --kubeconfig ~/.kube/staging --warn-days 21
```

Probe live TLS endpoints directly (no Kubernetes needed):

```sh
certhealthz probe api.example.com legacy-vendor.example.com:8443
```

Alert to a webhook (Slack incoming webhook, ServiceNow inbound REST, or any
JSON receiver) when something crosses the warning threshold:

```sh
certhealthz scan --webhook https://hooks.example.com/certhealthz
```

Export Prometheus exposition format instead of a table, e.g. for a
file-based service discovery scrape or a pushgateway job:

```sh
certhealthz scan --prometheus > /var/lib/node_exporter/textfile_collector/certs.prom
```

Record a scan so you can diff it against the next one:

```sh
certhealthz scan --record
certhealthz history diff
```

Serve the bundled web dashboard, backed by a live scan:

```sh
certhealthz dashboard --addr :8090
```

## Flags

| Flag                | Applies to          | Description                                                            |
| ------------------- | ------------------- | ---------------------------------------------------------------------- |
| `--kubeconfig`      | `scan`              | repeatable, one per cluster (default: `$KUBECONFIG`/`~/.kube/config`)  |
| `--warn-days`       | both                | days-remaining threshold before status flips to `expiring`             |
| `--webhook`         | both                | POST flagged rows as JSON to this URL                                  |
| `--prometheus`      | both                | print `cert_expiry_days` gauge instead of a table                      |
| `--include-secrets` | `scan`, `dashboard` | also scan raw `kubernetes.io/tls` Secrets (default `true`)             |
| `--timeout`         | `probe`             | per-endpoint dial timeout (default `5s`)                               |
| `--record`          | `scan`              | persist this scan to the history database                              |
| `--db`              | `scan`, `history`   | path to the SQLite history database (default `certhealthz-history.db`) |
| `--addr`            | `dashboard`         | address to serve the dashboard on (default `:8090`)                    |

## Status

MVP.

### Done

- [x] cert-manager `Certificate` scan across multiple kubeconfigs
- [x] raw `kubernetes.io/tls` Secret scan (leaf cert parsed directly, independent of Certificate status)
- [x] live TLS endpoint probe (`probe` command, concurrent)
- [x] threshold-tiered status classification (`ok` / `expiring` / `expired` / `error`)
- [x] webhook alerting (Slack/ServiceNow/custom JSON POST) on flagged rows
- [x] Prometheus exposition export (`cert_expiry_days` gauge)
- [x] historical run diffing (SQLite) — track expiry trend, not just point-in-time snapshot:
      `certhealthz scan --record` persists a run, `certhealthz history diff` reports what
      changed since the previous one.
- [x] bundled lightweight dashboard: `certhealthz dashboard` serves the React UI in
      `dashboard/` (embedded via `go:embed`, built with `make dashboard-build`) plus a
      `/api/certs` JSON endpoint backed by a live scan.
- [x] `goreleaser` for single-binary cross-platform distribution: see `.goreleaser.yaml`
      and `.github/workflows/release.yml` (tag push builds linux/darwin/windows,
      amd64/arm64 archives + checksums and attaches them to the GitHub release).

### Planned

- [ ] drift detection: Certificate reports `Ready` but backing Secret's actual leaf cert is stale/mismatched
- [ ] renewal failure root-cause hints (rate-limit hit, DNS-01 challenge broken, webhook misconfig)

## License

Apache 2.0
