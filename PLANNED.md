# Planned

Feature roadmap for CertHealthz, split out of README.md so the README stays a
usage doc. See README's [Comparison](README.md#comparison) for how the
already-shipped feature set stacks up against cert-manager, Datadog,
Prometheus `blackbox_exporter`, `testssl.sh`, and enterprise machine-identity
platforms (Venafi, DigiCert CertCentral) — the items below are what's next.

## Shipped

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
- [x] dashboard: Certificates tab split into a **Clusters** tab (cert-manager/Secret/Ingress/Gateway
      rows only, no live endpoints, no "Add endpoint") and a dedicated **Endpoints** tab (live TLS
      probes only), each with its own source-scoped "Manage sources" dialog (Clusters manages
      clusters only, Endpoints manages endpoints only); **Settings** gained a combined "Manage
      sources" section that can add/remove both, so there's still one place to do everything.
- [x] on-demand synthetic route test: a **Test** button on any Routes-tab row (and on a Migration
      coverage row, to re-verify a `cutoverReady` claim) runs a live TCP connect → TLS handshake
      (SNI forced to the route's own host) → certificate comparison against the scan's own backing
      Secret → HTTP request, reporting each step's own pass/fail and latency (`POST
      /api/routes/test`) — generalizes the migration cutover-gate's live-TLS-probe into a full
      HTTP-layer check, and catches "Gateway/Ingress is Accepted and healthy but is actually
      serving the wrong certificate," a gap drift detection alone can't see since it never opens a
      real connection. Only ever dials an address the scanner already discovered — a published
      LB/Gateway address, or, for a ClusterIP-only route with no external address (e.g. a
      bare-metal cluster with no LoadBalancer support), a port-forward straight to the fronting
      controller's Service, supported for Envoy Gateway and ingress-nginx specifically; every other
      controller reports an explicit "unsupported controller for port-forward testing" result
      rather than guessing at one — never an arbitrary user-supplied URL.

## Trust & chain validation

- [ ] OCSP/CRL revocation status check
- [ ] issuer-change anomaly detection (cert for a domain suddenly issued by an unexpected CA)

## Root cause & remediation

- [ ] admission webhook: warn or block on an Ingress/Gateway referencing an already-expiring cert
- [ ] one-click reissue extended to Gateway/mesh-backed certs — today's "Force reissue" button
      only covers cert-manager `Certificate` objects; a Gateway/Istio/Traefik row pointing at a
      stale Secret has no equivalent action

## Alerting & workflow

- [ ] native Slack Block Kit / Teams adaptive-card formatting, not raw JSON in `text`
- [ ] PagerDuty, Opsgenie, and email alert channels alongside webhook
- [ ] alert de-dup and escalation (re-notify as expiry gets closer, don't just fire once)
- [ ] per-namespace/per-team warn thresholds and alert routing
- [ ] Gateway/migration-specific alert content — a "host ready to cut over" or "dual-run drift
      detected" event reads better as its own alert type than a generic `error` row

## Detection coverage (extends Gateway API / mesh work)

- [ ] `BackendTLSPolicy` scanning (`gateway.networking.k8s.io/v1`) — validates the *upstream* TLS
      cert a Gateway connects to, not just the client-facing listener cert
- [ ] `ListenerSet` support — lets a namespace attach extra listeners to a shared Gateway,
      currently invisible to the Gateway API scan
- [ ] Istio `PeerAuthentication` mTLS-mode check (STRICT/PERMISSIVE/DISABLE) — mesh-wide posture,
      complementing the per-Secret mTLS client-cert tracking already shipped
- [ ] Kong Ingress Controller / HAProxy Ingress CRD recognition, same discovery-gated pattern as
      Istio/Traefik

## Policy & enforcement

- [ ] Kyverno/OPA Gatekeeper policy bundle (e.g. "no Ingress without cert-manager annotation",
      "no wildcard cert older than 90 days") — reuse an existing policy engine instead of building
      a bespoke one
- [ ] cert issuance policy drift: flag a cert-manager `Issuer` whose ACME/CA config silently
      changed from what GitOps declares

## Non-k8s-TLS identity types

- [ ] private CA visibility: HashiCorp Vault PKI, `step-ca` — same drift-detection model as
      cert-manager, different backend
- [ ] code-signing / container-image-signing key expiry (cosign/sigstore), adjacent to existing
      supply-chain-security territory
- [ ] SSH CA / short-lived cert tracking for cluster access

## Ecosystem integration

- [ ] ArgoCD/Flux health check plugin — cert status as a sync-wave gate, not just a dashboard
- [ ] Terraform provider or data source — expose scan results to IaC pipelines pre-deploy
- [ ] Backstage plugin — cert health as a service-catalog tile

## Operating at scale

- [ ] in-cluster mode: run as a Deployment/CronJob under a ServiceAccount, no kubeconfig needed
- [ ] Helm chart for in-cluster install
- [ ] CRD/operator (`CertHealthzPolicy`) for GitOps-managed thresholds and alert routing
- [ ] dashboard auth (OIDC/SSO) — currently unauthenticated
- [ ] trend charts in the dashboard, backed by the existing SQLite history
- [ ] compliance export (CSV/PDF) — auditable evidence of cert hygiene for SOC2/PCI reviews
- [ ] `--namespace`/`--label-selector` scan scoping — every scanner (cert-manager, Ingress,
      Gateway, mesh, cloud) still lists cluster-wide; large multi-tenant clusters will want to
      narrow this
- [ ] cloud SDK rate-limit/backoff handling for `--aws-region`/`--gcp-project`/`--azure-vault-url`
      at fleet scale (ACM/Key Vault list calls can hit throttling on large accounts)

## Enterprise readiness (competing for Venafi/DigiCert-class accounts)

- [ ] RBAC-scoped dashboard views (namespace/team-scoped visibility, not all-or-nothing), pairs
      with the OIDC/SSO item above
- [ ] audit log of who triggered a reissue / viewed what — extend the existing SQLite history
      schema rather than bolt on a new store
- [ ] multi-tenant mode for MSPs/platform teams managing many downstream clusters — one
      dashboard, tenant-scoped rows

**Non-goal, deliberately:** a required operator/CRD/database just to chase enterprise
checkboxes. The core pitch — single binary, no CRD, works across cert-manager, raw Secrets,
Ingress, Gateway API, mesh, cloud, and live endpoints in one report — is the differentiator
Venafi/Datadog-class tools can't match zero-install. Enterprise features above should stay
opt-in additions, not requirements to use the tool at all.

## Growth & distribution

- [ ] demo GIF/video of the dashboard at the top of the README
- [ ] zero-setup demo (`docker run` or hosted playground with fake data, no kubeconfig needed)
- [x] comparison table vs cert-manager, Datadog cert monitoring, `testssl.sh`, and others — see
      [Comparison](README.md#comparison)
- [ ] Homebrew tap
- [ ] `krew` plugin (kubectl plugin index)
- [ ] submit to `awesome-kubernetes`, `awesome-go`, CNCF landscape
- [ ] Codecov/test-coverage badge
- [ ] CONTRIBUTING.md + good-first-issue labels
- [ ] Show HN / r/kubernetes / r/devops launch post timed with a release
