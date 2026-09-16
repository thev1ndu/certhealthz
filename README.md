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

## Features

**Detection & scanning**
- cert-manager `Certificate` scan across multiple kubeconfigs, plus raw
  `kubernetes.io/tls` Secret scan independent of Certificate status
- Live TLS endpoint probing (concurrent), capturing negotiated TLS
  version/cipher and flagging TLS 1.0/1.1 or an insecure cipher
- Threshold-tiered status classification: `ok` / `expiring` / `expired` /
  `error` / `drift` / `weak-crypto` / `broken-chain`
- Weak-crypto and broken/incomplete chain detection (RSA keys under 2048
  bits, SHA-1/MD5 signatures, missing or mismatched intermediates),
  including intermediate/root chain-cert expiry, not just the leaf
- cert-manager `Issuer`/`ClusterIssuer` health monitoring alongside
  Certificates
- Certificate Transparency log monitoring — one-off `ct` command or
  scheduled `--ct-domains` — cross-checked against known cluster DNS names
  to flag shadow/rogue issuance
- Private key reuse and DNS name conflict detection across Secrets
- mTLS client-certificate tracking (`--mtls-secret-selector`), tracked
  separately from server certs

**Drift & root cause**
- Certificate-vs-Secret drift detection: catches a `Ready` Certificate
  whose backing Secret's actual leaf cert doesn't match
- Renewal failure root cause: surfaces the actual ACME/webhook error from
  the latest `CertificateRequest`, not just "not ready"
- Naming/label convention drift (`--require-label`) for compliance-review
  teams

**Cross-referencing routes**
- Ingress TLS block cross-reference (SAN/Secret match), including a plain
  HTTP Ingress with no TLS block at all
- Gateway API cross-reference (`HTTPRoute`/`GRPCRoute`/`TLSRoute` →
  `Gateway` listener `tls.certificateRefs`, cross-namespace
  `ReferenceGrant` aware), plus `GatewayClass`/`Gateway` status surfaced by
  controller name
- Service mesh / other controllers: Istio `Gateway` and Traefik
  `IngressRoute`, both discovery-gated (empty, not an error, when the CRDs
  aren't installed)
- Cloud-managed certs: AWS ACM, GCP Certificate Manager, Azure Key Vault —
  each opt-in per flag, no credentials loaded unless requested
- Orphaned Secret detection, and a "what breaks" blast-radius view from a
  certificate's detail page

**Ingress → Gateway migration**
- Routes tab classifies every host `ingress-only` / `dual-running` /
  `gateway-only`, and flags Secret drift between the two sides
- Cutover gate confirms the Gateway side's `Accepted`+`Programmed`
  conditions (and a live TLS probe, where an address is published) before
  calling a host ready to cut over
- Stale-Ingress detection after cutover — detection only, nothing is ever
  deleted
- On-demand synthetic route test: full TCP connect → TLS handshake →
  certificate comparison → HTTP request per row, with a port-forward
  fallback for ClusterIP-only routes (Envoy Gateway, ingress-nginx)

**Remediation & alerting**
- One-click **Force reissue** for a stuck cert-manager Certificate
- Webhook alerting (Slack/ServiceNow/custom JSON POST) on flagged rows,
  with a "send test alert" button in the dashboard
- Prometheus exposition export (`cert_expiry_days` gauge)
- Historical run diffing (SQLite) — `scan --record` + `history diff`, or
  the dashboard's continuously recorded audit trail

**Dashboard**
- Bundled React UI (`certhealthz ui`), a single binary via `go:embed`
- Overview homepage: cluster/endpoint/certificate counts, status
  breakdown, a "needs attention" list linking straight into a cert
- Clusters / Endpoints / Routes / History / Settings / CT-log tabs — full
  parity with the CLI, nothing is CLI-only
- Full certificate detail view: subject/issuer DN, SANs, fingerprints,
  key/extended-key usage, OCSP/CRL URLs, and the full certificate chain

**Distribution**
- Single Go binary, no CRD, no in-cluster install required
- Cross-platform releases via `goreleaser` (linux/darwin/windows,
  amd64/arm64)

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

The `ui` command serves a bundled web UI (see [ui/](ui/)).
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

Serve the bundled web dashboard, backed by a live scan — probing endpoints
alongside your clusters, either at startup or added later from the UI:

```sh
certhealthz ui --addr :8090 --probe api.example.com
```

Check Certificate Transparency logs for certs issued for your domains that
no configured cluster knows about (pass `--kubeconfig` to get the
known/unknown distinction — otherwise every recent cert is just reported):

```sh
certhealthz ct example.com --kubeconfig ~/.kube/prod --since 24h
```

## Flags

| Flag                | Applies to           | Description                                                                                                    |
| -------------------- | --------------------- | ---------------------------------------------------------------------------------------------------------------- |
| `--kubeconfig`      | `scan`, `ui`, `ct`   | repeatable, one per cluster (default: `$KUBECONFIG`/`~/.kube/config`)                                          |
| `--warn-days`       | `scan`, `probe`, `ui`, `ct` | days-remaining threshold before status flips to `expiring` (`ui`: changeable at runtime from Settings) |
| `--webhook`         | `scan`, `probe`, `ui`, `ct` | POST flagged rows as JSON to this URL (`ui`: changeable at runtime from Settings, with a "Send test alert" button) |
| `--prometheus`      | `scan`, `probe`, `ct` | print `cert_expiry_days` gauge instead of a table                                                              |
| `--include-secrets` | `scan`, `ui`         | also scan raw `kubernetes.io/tls` Secrets, for drift detection and Ingress cross-referencing (default `true`, changeable at runtime from Settings) |
| `--timeout`         | `probe`              | per-endpoint dial timeout (default `5s`)                                                                       |
| `--probe`           | `ui`                 | repeatable, live TLS endpoint to probe on every scan (also addable from the UI)                                |
| `--probe-timeout`   | `ui`                 | per-endpoint dial timeout for `--probe` endpoints (default `5s`)                                               |
| `--since`           | `ct`                 | only report CT log entries logged within this window (default `24h`; the UI's CT panel offers the same choices) |
| `--record`          | `scan`               | persist this scan to the history database                                                                      |
| `--db`              | `scan`, `history`, `ui` | path to the SQLite database (default: OS per-user config dir, e.g. `~/Library/Application Support/certhealthz/state.db`; `ui` uses it for the audit log and persisted settings) |
| `--record-interval` | `ui`                  | how often the dashboard automatically records a scan to the audit log (default `15m`)                          |
| `--history-retention` | `ui`                | how long recorded runs are kept before pruning; `0` disables pruning (default `720h`)                          |
| `--addr`            | `ui`                 | address to serve the dashboard on (default `:8090`)                                                            |
| `--ct-domains`      | `ui`                 | repeatable, domain to periodically check CT logs for on `--ct-interval`; unset disables CT monitoring          |
| `--ct-interval`     | `ui`                 | how often to check `--ct-domains` against CT logs (default `6h`)                                               |
| `--require-label`   | `scan`, `ui`         | repeatable, label key every cert-manager Certificate must carry (value not checked); unset disables the check  |
| `--aws-region`      | `scan`, `ui`         | repeatable, AWS region to scan Certificate Manager (ACM) in via the default AWS credential chain; unset disables AWS scanning |
| `--gcp-project`     | `scan`, `ui`         | GCP project to scan Certificate Manager in via Application Default Credentials; unset disables GCP scanning   |
| `--azure-vault-url` | `scan`, `ui`         | repeatable, Azure Key Vault URL to scan for certificates via `azidentity`'s default credential chain; unset disables Azure scanning |
| `--mtls-secret-selector` | `scan`, `ui`     | repeatable label selector matching `kubernetes.io/tls` Secrets that hold mTLS client certificates, tracked separately from server certs |

## Status

MVP — see [Features](#features) above for everything shipped so far. Full
feature roadmap (trust/chain validation, alerting, detection coverage,
policy enforcement, ecosystem integrations, scaling, enterprise readiness,
and growth/distribution) lives in [PLANNED.md](PLANNED.md).

## License

Apache 2.0
