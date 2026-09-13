# CertHealthz

[![CI](https://github.com/thev1ndu/certhealthz/actions/workflows/ci.yml/badge.svg)](https://github.com/thev1ndu/certhealthz/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/thev1ndu/certhealthz.svg)](https://pkg.go.dev/github.com/thev1ndu/certhealthz)
[![Go Report Card](https://goreportcard.com/badge/github.com/thev1ndu/certhealthz)](https://goreportcard.com/report/github.com/thev1ndu/certhealthz)
[![Release](https://img.shields.io/github/v/release/thev1ndu/certhealthz)](https://github.com/thev1ndu/certhealthz/releases)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

Live TLS certificate health across clusters. Single Go binary, no CRD, no
in-cluster install — point it at kubeconfigs and/or live endpoints and get
a unified expiry report.

![CertHealthz dashboard](https://i.postimg.cc/RZCs0rM6/image.png)

## Why

- cert-manager tracks its own `Certificate` objects, but says nothing about
  raw TLS endpoints (load balancers, vendor APIs, legacy certs) it doesn't manage.
- A `Certificate` can report `Ready` while the backing `Secret`'s actual leaf
  cert is stale — renewal silently broke and nobody noticed.
- Multi-cluster orgs have no single place to see "what expires next, everywhere."

CertHealthz scans all three sources — cert-manager `Certificate` objects, raw
`kubernetes.io/tls` Secrets, and live TLS endpoints — and reports them in one
sorted-by-urgency table.

## Install

```sh
go install github.com/thev1ndu/certhealthz@latest
```

Or download a prebuilt binary from the [releases page](https://github.com/thev1ndu/certhealthz/releases) (linux/darwin/windows, amd64/arm64):

```sh
tar -xzf certhealthz_darwin_arm64.tar.gz   # pick the asset for your OS/arch
xattr -d com.apple.quarantine certhealthz  # macOS only — clears Gatekeeper quarantine
chmod +x certhealthz
sudo mv certhealthz /usr/local/bin/
certhealthz --help
```

See [docs/RELEASES.md](docs/RELEASES.md) for checksum verification, Windows
steps, and how to update to a newer release.

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
- [x] threshold-tiered status classification (`ok` / `expiring` / `expired` / `error` / `drift`)
- [x] drift detection: a Ready cert-manager Certificate is cross-checked against its backing
      Secret's actual leaf cert; a missing Secret or a mismatched expiry flags the row `drift`
      instead of trusting the Certificate's own status (surfaced in `scan`, `--webhook`, and the
      dashboard)
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

**Detection coverage**

- [ ] Ingress/Gateway API cross-reference: flag routes pointing at a Secret that's missing, expired, or SAN-mismatched against the route's host
- [ ] cloud-managed cert scanning: AWS ACM, GCP Certificate Manager, Azure Key Vault — one report across k8s and cloud
- [ ] Certificate Transparency log monitoring: catch certs issued for your domains outside any known cluster or cloud account (shadow/rogue issuance)
- [ ] mTLS client-certificate expiry tracking, not just server certs

**Trust & chain validation**

- [ ] full chain validation: missing/expired intermediates, weak signature algorithm (SHA-1), undersized keys
- [ ] OCSP/CRL revocation status check
- [ ] issuer-change anomaly detection (cert for a domain suddenly issued by an unexpected CA)

**Root cause & remediation**

- [ ] renewal failure root-cause hints (rate-limit hit, DNS-01 challenge broken, webhook misconfig)
- [ ] one-shot remediation: trigger cert-manager re-issuance directly (`certhealthz fix <name>`) instead of just reporting the stuck Certificate
- [ ] admission webhook: warn or block on an Ingress/Gateway referencing an already-expiring cert

**Alerting & workflow**

- [ ] native Slack Block Kit / Teams adaptive-card formatting, not raw JSON in `text`
- [ ] PagerDuty, Opsgenie, and email alert channels alongside webhook
- [ ] alert de-dup and escalation (re-notify as expiry gets closer, don't just fire once)
- [ ] per-namespace/per-team warn thresholds and alert routing

**Operating at scale**

- [ ] in-cluster mode: run as a Deployment/CronJob under a ServiceAccount, no kubeconfig needed
- [ ] Helm chart for in-cluster install
- [ ] CRD/operator (`CertHealthzPolicy`) for GitOps-managed thresholds and alert routing
- [ ] dashboard auth (OIDC/SSO) — currently unauthenticated
- [ ] trend charts in the dashboard, backed by the existing SQLite history
- [ ] compliance export (CSV/PDF) — auditable evidence of cert hygiene for SOC2/PCI reviews

### Growth

- [ ] demo GIF/video of the dashboard at the top of the README
- [ ] zero-setup demo (`docker run` or hosted playground with fake data, no kubeconfig needed)
- [ ] comparison table vs cert-manager, Datadog cert monitoring, `testssl.sh`
- [ ] Homebrew tap
- [ ] `krew` plugin (kubectl plugin index)
- [ ] submit to `awesome-kubernetes`, `awesome-go`, CNCF landscape
- [ ] Codecov/test-coverage badge
- [ ] CONTRIBUTING.md + good-first-issue labels
- [ ] Show HN / r/kubernetes / r/devops launch post timed with a release

## License

Apache 2.0
