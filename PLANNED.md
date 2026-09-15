# Planned

Feature roadmap for CertHealthz, split out of README.md so the README stays a
usage doc. See README's [Comparison](README.md#comparison) for how the
already-shipped feature set stacks up against cert-manager, Datadog,
Prometheus `blackbox_exporter`, `testssl.sh`, and enterprise machine-identity
platforms (Venafi, DigiCert CertCentral) — the items below are what's next.

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
