// Package migration cross-references Ingress and Gateway API routes by
// hostname to help an operator migrate from Ingress to Gateway API with
// confidence: which hosts are still Ingress-only, which are already
// Gateway-only, which are running on both at once (and might be drifting),
// and whether a host's Gateway side is actually ready to take traffic
// before its Ingress is retired.
package migration

import (
	"crypto/tls"
	"sort"
	"time"

	"github.com/thev1ndu/certhealthz/pkg/gateway"
	"github.com/thev1ndu/certhealthz/pkg/ingress"
	"github.com/thev1ndu/certhealthz/pkg/probe"
)

// State classifies a hostname's migration status.
type State string

const (
	IngressOnly State = "ingress-only"
	DualRunning State = "dual-running"
	GatewayOnly State = "gateway-only"
)

// HostCoverage is one hostname's migration status: which side(s) serve it,
// whether the Ingress and Gateway sides agree on which Secret backs it, and
// (for dual-running/gateway-only hosts) whether the Gateway side has passed
// its cutover gate.
type HostCoverage struct {
	Host  string
	State State

	IngressSecret string // "namespace/name", empty if not served by Ingress or that Ingress has no TLS block for this host
	GatewaySecret string // "namespace/name", empty if not served by Gateway API
	SecretDrift   bool   // true if both sides serve this host with different Secrets
	CutoverReady  bool   // true if the Gateway side has passed its cutover gate (see cutoverGate)
	CutoverDetail string // why CutoverReady is false, or what passed if true

	// StaleIngresses lists any Ingress objects still routing this host
	// (spec.rules[].host, regardless of TLS block) even though the host is
	// already gateway-only — detection only, never acted on automatically.
	// Empty unless State == GatewayOnly and at least one such Ingress
	// exists.
	StaleIngresses []string // "namespace/ingress-name"
}

// hostEntry accumulates what's known about one hostname while scanning
// both route sources, before being folded into a HostCoverage.
type hostEntry struct {
	ingressServed bool   // true if any Ingress routes this host, TLS or not
	ingressSecret string // set only if that Ingress terminates TLS for it
	gatewayRoute  *gateway.Route
}

// CoverageWithStaleIngresses is Coverage plus stale-Ingress-after-cutover
// detection: ruleHosts is every Ingress's spec.rules[].host (see
// ingress.ScanRuleHosts), independent of TLS — a host classified
// gateway-only that still has a matching Ingress rule host is flagged for
// review (detection only; nothing is ever deleted automatically).
func CoverageWithStaleIngresses(ingressRoutes []ingress.Route, gatewayRoutes []gateway.Route, ruleHosts []ingress.RuleHost, probeTimeout time.Duration) []HostCoverage {
	cov := Coverage(ingressRoutes, gatewayRoutes, probeTimeout)

	byHost := make(map[string][]string)
	for _, rh := range ruleHosts {
		byHost[rh.Host] = append(byHost[rh.Host], rh.Namespace+"/"+rh.Ingress)
	}

	for i := range cov {
		if cov[i].State != GatewayOnly {
			continue
		}
		if names, ok := byHost[cov[i].Host]; ok {
			cov[i].StaleIngresses = names
		}
	}
	return cov
}

// Coverage groups ingressRoutes and gatewayRoutes by hostname and classifies
// each host's migration state. probeTimeout controls the live TLS probe run
// as part of the cutover-gate check for dual-running/gateway-only hosts; a
// zero value disables the live probe (Gateway readiness is judged purely
// from its Accepted/Programmed conditions in that case).
func Coverage(ingressRoutes []ingress.Route, gatewayRoutes []gateway.Route, probeTimeout time.Duration) []HostCoverage {
	byHost := make(map[string]*hostEntry)

	entry := func(host string) *hostEntry {
		e, ok := byHost[host]
		if !ok {
			e = &hostEntry{}
			byHost[host] = e
		}
		return e
	}

	for _, route := range ingressRoutes {
		hosts := route.Hosts
		if len(hosts) == 0 {
			continue
		}
		for _, host := range hosts {
			if host == "" {
				continue
			}
			e := entry(host)
			e.ingressServed = true
			if route.SecretName != "" {
				e.ingressSecret = route.Namespace + "/" + route.SecretName
			}
		}
	}

	for i := range gatewayRoutes {
		route := &gatewayRoutes[i]
		if route.SecretName == "" {
			continue
		}
		for _, host := range route.Hosts {
			if host == "" {
				continue
			}
			e := entry(host)
			// A host may have several Gateway routes; keep the first
			// Accepted+Programmed one if there's a choice, since that's
			// the one most representative of the host's real Gateway
			// readiness.
			if e.gatewayRoute == nil || (!e.gatewayRoute.Accepted || !e.gatewayRoute.GatewayProgrammed) && route.Accepted && route.GatewayProgrammed {
				e.gatewayRoute = route
			}
		}
	}

	out := make([]HostCoverage, 0, len(byHost))
	for host, e := range byHost {
		hc := HostCoverage{Host: host}
		switch {
		case e.ingressServed && e.gatewayRoute != nil:
			hc.State = DualRunning
		case e.ingressServed:
			hc.State = IngressOnly
		default:
			hc.State = GatewayOnly
		}

		hc.IngressSecret = e.ingressSecret
		if e.gatewayRoute != nil {
			hc.GatewaySecret = e.gatewayRoute.SecretNamespace + "/" + e.gatewayRoute.SecretName
		}
		if hc.State == DualRunning && hc.IngressSecret != "" && hc.IngressSecret != hc.GatewaySecret {
			hc.SecretDrift = true
		}

		if hc.State == DualRunning || hc.State == GatewayOnly {
			hc.CutoverReady, hc.CutoverDetail = cutoverGate(e.gatewayRoute, probeTimeout)
		}

		out = append(out, hc)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Host < out[j].Host })
	return out
}

// cutoverGate decides whether a dual-running or gateway-only host's Gateway
// side is ready to take (or keep) traffic: its Gateway must report
// Accepted+Programmed, and — if the Gateway published a reachable address —
// a live TLS probe against it must succeed. A Gateway with no published
// address yet (common right after creation, or for controllers that don't
// populate status.addresses) skips the live probe and is judged on its
// conditions alone.
func cutoverGate(route *gateway.Route, probeTimeout time.Duration) (ready bool, detail string) {
	if route == nil {
		return false, "no Gateway route resolved for this host"
	}
	if !route.Accepted {
		return false, "Gateway has not accepted this route (Accepted condition not True)"
	}
	if !route.GatewayProgrammed {
		return false, "Gateway is not Programmed"
	}
	if probeTimeout <= 0 || len(route.GatewayAddresses) == 0 {
		return true, "Gateway Accepted+Programmed (no live probe: no address published)"
	}

	host := route.Hosts
	sni := ""
	if len(host) > 0 {
		sni = host[0]
	}
	for _, addr := range route.GatewayAddresses {
		var opts []probe.Option
		if sni != "" {
			opts = append(opts, withSNIOverride(sni))
		}
		result := probe.Probe(addr, probeTimeout, opts...)
		if result.Err == nil {
			return true, "Gateway Accepted+Programmed, live TLS probe against " + addr + " succeeded"
		}
	}
	return false, "Gateway Accepted+Programmed, but a live TLS probe against every published address failed"
}

// withSNIOverride sets the SNI ServerName probe.Probe presents during its
// TLS handshake — probe.Probe otherwise derives ServerName from the dialed
// address itself (an IP, for a Gateway's published address), which won't
// match a Gateway listener's Hostname-based SNI routing.
func withSNIOverride(sni string) probe.Option {
	return func(cfg *tls.Config) {
		cfg.ServerName = sni
	}
}
