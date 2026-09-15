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

MVP.

### Done

- [x] cert-manager `Certificate` scan across multiple kubeconfigs
- [x] raw `kubernetes.io/tls` Secret scan (leaf cert parsed directly, independent of Certificate status)
- [x] live TLS endpoint probe (`probe` command, concurrent; also probeable from the dashboard
      via `--probe`/`--probe-timeout` at startup or the "Add endpoint" UI, merged into the same
      `/api/certs` report as the cluster scan)
- [x] threshold-tiered status classification (`ok` / `expiring` / `expired` / `error` / `drift` /
      `weak-crypto` / `broken-chain`)
- [x] drift detection: a Ready cert-manager Certificate is cross-checked against its backing
      Secret's actual leaf cert; a missing Secret, a mismatched expiry, or a `spec.dnsNames` that
      no longer matches the Secret's actual SANs flags the row `drift` instead of trusting the
      Certificate's own status (surfaced in `scan`, `--webhook`, and the dashboard)
- [x] weak-crypto and broken/incomplete-chain detection: every scanned cert's key size,
      signature algorithm, and bundled certificate chain are checked — an RSA key under 2048
      bits or a SHA-1/MD5 signature flags `weak-crypto`; a chain missing its intermediate or
      bundled with a mismatched one flags `broken-chain`. Deliberately checks the bundle's own
      internal consistency rather than public CA trust, so certs from a private/internal CA
      (common for cert-manager's own self-signed/CA issuers) aren't false-flagged.
- [x] Issuer/ClusterIssuer health monitoring: cert-manager `Issuer`/`ClusterIssuer` objects are
      scanned alongside Certificates — a `Ready=False` issuer (broken ACME account, exhausted CA
      quota, webhook failure) surfaces immediately instead of waiting for every Certificate it
      backs to start failing.
- [x] full certificate detail view: click any row for the parsed subject/issuer DN, serial,
      SANs, signature/public-key algorithm, SHA-1/SHA-256 fingerprints, key/extended-key usage,
      OCSP/CRL URLs, and the full certificate chain (`GET /api/certs/detail`).
- [x] one-click reissue: a **Force reissue** button on an unhealthy cert-manager certificate
      triggers a real reissuance (the same `Issuing: True` condition mechanism `cmctl renew`
      uses) directly from the dashboard, instead of just reporting the stuck Certificate.
- [x] webhook alerting (Slack/ServiceNow/custom JSON POST) on flagged rows
- [x] Prometheus exposition export (`cert_expiry_days` gauge)
- [x] historical run diffing (SQLite) — track expiry trend, not just point-in-time snapshot:
      `certhealthz scan --record` persists a run, `certhealthz history diff` reports what
      changed since the previous one.
- [x] bundled lightweight dashboard: `certhealthz ui` serves the React UI in
      `ui/` (embedded via `go:embed`, built with `make dashboard-build`) plus a
      `/api/certs` JSON endpoint backed by a live scan.
- [x] `goreleaser` for single-binary cross-platform distribution: see `.goreleaser.yaml`
      and `.github/workflows/release.yml` (tag push builds linux/darwin/windows,
      amd64/arm64 archives + checksums and attaches them to the GitHub release).
- [x] Ingress cross-reference: every Ingress TLS block is checked against its backing Secret —
      missing Secret, or a host the Secret's certificate doesn't actually cover (SAN mismatch) —
      flagged as an `ingress`-sourced row alongside the normal expiry classification. Gateway API
      (`HTTPRoute`/`Gateway`) isn't covered yet — it's an optional CRD with a different shape, left
      for a follow-up rather than bundled in half-done.
- [x] Certificate Transparency log monitoring (`certhealthz ct <domain...>`): queries crt.sh for
      certs recently logged for a domain; with `--kubeconfig` set, cross-checks each result
      against your clusters' Secret DNS names and flags a domain none of them cover as possible
      shadow/rogue issuance.
- [x] full UI parity with the CLI — nothing is CLI-only anymore:
      a **Settings** panel makes `--warn-days`, `--include-secrets`, and the webhook URL
      live-editable (no restart), with a "Send test alert now" button (`POST /api/alert`) that
      posts the current flagged rows through `pkg/alert.Send`; a **History** panel is a
      continuously recorded audit-trail feed — no manual snapshot button, the dashboard records
      automatically on a `--record-interval` timer and shows every detected change, paginated
      (`GET /api/history/events`, backed by the same `--db` SQLite store `history diff` uses,
      pruned on a `--history-retention` window); and a **Check CT logs** panel runs the same
      Certificate Transparency check as `certhealthz ct` (`POST /api/ct`), cross-referenced
      against every configured dashboard cluster (startup `--kubeconfig` and uploads alike), not
      just the CLI's flat `--kubeconfig` list.
- [x] redesigned dashboard: a new **Overview** homepage (cluster/endpoint/certificate counts,
      status breakdown, a "needs attention" list linking straight into a cert's detail view) is
      now the default landing page, and every page shares one sans-serif, sharp-cornered
      architectural design system.
- [x] intermediate/root chain-cert expiry tracking: the bundled chain's own certs are checked for
      expiry, not just the leaf — an intermediate a CA rotates yearly gets flagged `broken-chain`
      even while the leaf itself still looks healthy.
- [x] renewal failure root cause: a not-ready cert-manager `Certificate`'s row is enriched with the
      actual ACME/webhook error from its most recent `CertificateRequest`, not just "not ready".
- [x] private key reuse and DNS name conflict detection: Secrets across every configured cluster
      are cross-checked for a shared public key (usually a copy-pasted key) or the same hostname
      claimed by more than one Secret, flagged on the affected `secret` rows.
- [x] TLS version/cipher visibility on live endpoint probes: `probe`/the dashboard's endpoint
      probes now capture the negotiated protocol version and cipher suite, flagging anything still
      accepting TLS 1.0/1.1 or an insecure cipher as `weak-crypto`.
- [x] scheduled Certificate Transparency monitoring: `certhealthz ui --ct-domains` periodically
      re-runs the same CT check as `certhealthz ct` on a `--ct-interval` timer, alerting through
      the existing webhook when a domain isn't covered by any known cluster.
- [x] "what breaks" blast-radius view: a certificate's detail page lists every Ingress route its
      Secret backs, turning "this cert is expiring" into "these specific things break".
- [x] orphaned Secret detection: a `kubernetes.io/tls` Secret referenced by no cert-manager
      Certificate and no Ingress route is flagged as dead weight — the inverse of the blast-radius
      check above.
- [x] naming/label convention drift: `--require-label` (repeatable, `scan`/`ui`) flags any
      cert-manager Certificate missing an operator-required label key, for compliance-review
      teams enforcing an ownership/environment tagging policy.
- [x] Gateway API cross-reference: `HTTPRoute`/`GRPCRoute`/`TLSRoute` are resolved through their
      parent `Gateway`'s listener `tls.certificateRefs` (honoring cross-namespace `ReferenceGrant`)
      the same way an Ingress TLS block is checked against its Secret — a route attached to an
      unaccepted Gateway, a missing Secret, or a host not covered by the Secret's SANs is flagged
      `error`. `GatewayClass`/`Gateway` `Accepted`/`Programmed` conditions are surfaced as
      `gateway-status` rows, with the failing `GatewayClass`'s controller identified by name (Envoy
      Gateway, NGINX Gateway Fabric, Contour, Istio, Kong, Traefik, GKE Gateway, AWS Gateway API
      Controller, Linkerd). Fails soft (empty result, no error) on a cluster without the Gateway API
      CRDs installed. Orphaned-Secret detection now also counts Gateway-referenced Secrets.
- [x] Ingress → Gateway API migration tooling: a new **Routes** tab (and `/api/routes`,
      `/api/routes/coverage`) cross-references every Ingress and Gateway API route by hostname,
      classifying each host `ingress-only` / `dual-running` / `gateway-only`; a `dual-running` host
      whose Ingress and Gateway sides reference different Secrets is flagged for drift, and a
      "cutover gate" check confirms the Gateway side's `Accepted`+`Programmed` conditions (and,
      where a live address is published, an actual TLS probe against it) before calling a
      `gateway-only`/`dual-running` host ready to cut over. A `gateway-only` host that still has a
      matching Ingress rule host is flagged as a stale Ingress left behind after cutover — detection
      only, nothing is ever deleted.
- [x] service mesh / other ingress-controller TLS scanning: Istio `Gateway` (`tls.credentialName`,
      including the `kubernetes-gateway://` cross-namespace form) and Traefik `IngressRoute`
      (`spec.tls.secretName` plus hostnames parsed out of `Host()`/`HostSNI()` match rules) are
      cross-checked against their backing Secrets the same way Ingress/Gateway API are. Both are
      discovery-gated: a cluster without the Istio or Traefik CRDs installed scans as empty, not an
      error — most clusters run neither.
- [x] cloud-managed cert scanning: `--aws-region` (AWS Certificate Manager, default AWS credential
      chain), `--gcp-project` (GCP Certificate Manager, Application Default Credentials), and
      `--azure-vault-url` (Azure Key Vault, `azidentity`'s default credential chain) each add rows
      (`aws-acm` / `gcp-certmanager` / `azure-keyvault`) to the same unified report — no cloud
      credentials are ever loaded unless the corresponding flag is set.
- [x] mTLS client-certificate expiry tracking: `--mtls-secret-selector` (repeatable label selector)
      scans matching `kubernetes.io/tls` Secrets through the same parsing path as a normal Secret
      scan, tagged `mtls-client` with a "client certificate" note, so client certs are tracked
      without being mistaken for server certs backing a route.

### Planned

**Trust & chain validation**

- [ ] OCSP/CRL revocation status check
- [ ] issuer-change anomaly detection (cert for a domain suddenly issued by an unexpected CA)

**Root cause & remediation**

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
