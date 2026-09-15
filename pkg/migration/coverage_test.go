package migration

import (
	"testing"

	"github.com/thev1ndu/certhealthz/pkg/gateway"
	"github.com/thev1ndu/certhealthz/pkg/ingress"
)

func TestCoverageClassifiesHosts(t *testing.T) {
	ingressRoutes := []ingress.Route{
		{Namespace: "web", Ingress: "legacy", SecretName: "legacy-tls", Hosts: []string{"a.example.com"}},
		{Namespace: "web", Ingress: "dual", SecretName: "dual-tls", Hosts: []string{"b.example.com"}},
	}
	gatewayRoutes := []gateway.Route{
		{
			Namespace: "web", RouteKind: "HTTPRoute", RouteName: "dual-route",
			GatewayName: "gw", GatewayNamespace: "web",
			SecretName: "dual-tls-gw", SecretNamespace: "web",
			Hosts: []string{"b.example.com"}, Accepted: true, GatewayProgrammed: true,
		},
		{
			Namespace: "web", RouteKind: "HTTPRoute", RouteName: "new-route",
			GatewayName: "gw", GatewayNamespace: "web",
			SecretName: "new-tls", SecretNamespace: "web",
			Hosts: []string{"c.example.com"}, Accepted: true, GatewayProgrammed: true,
		},
	}

	cov := Coverage(ingressRoutes, gatewayRoutes, 0)
	byHost := map[string]HostCoverage{}
	for _, hc := range cov {
		byHost[hc.Host] = hc
	}

	if len(cov) != 3 {
		t.Fatalf("expected 3 hosts, got %d: %+v", len(cov), cov)
	}
	if byHost["a.example.com"].State != IngressOnly {
		t.Errorf("expected a.example.com ingress-only, got %s", byHost["a.example.com"].State)
	}
	if byHost["b.example.com"].State != DualRunning {
		t.Errorf("expected b.example.com dual-running, got %s", byHost["b.example.com"].State)
	}
	if !byHost["b.example.com"].SecretDrift {
		t.Errorf("expected b.example.com to be flagged for secret drift (web/dual-tls vs web/dual-tls-gw)")
	}
	if byHost["c.example.com"].State != GatewayOnly {
		t.Errorf("expected c.example.com gateway-only, got %s", byHost["c.example.com"].State)
	}
	if !byHost["c.example.com"].CutoverReady {
		t.Errorf("expected c.example.com cutover ready (Accepted+Programmed, no probe requested): %s", byHost["c.example.com"].CutoverDetail)
	}
}

func TestCoverageNoSecretDriftWhenSame(t *testing.T) {
	ingressRoutes := []ingress.Route{
		{Namespace: "web", Ingress: "dual", SecretName: "shared-tls", Hosts: []string{"same.example.com"}},
	}
	gatewayRoutes := []gateway.Route{
		{
			Namespace: "web", RouteKind: "HTTPRoute", RouteName: "dual-route",
			GatewayName: "gw", GatewayNamespace: "web",
			SecretName: "shared-tls", SecretNamespace: "web",
			Hosts: []string{"same.example.com"}, Accepted: true, GatewayProgrammed: true,
		},
	}

	cov := Coverage(ingressRoutes, gatewayRoutes, 0)
	if len(cov) != 1 {
		t.Fatalf("expected 1 host, got %d", len(cov))
	}
	if cov[0].SecretDrift {
		t.Errorf("expected no secret drift when both sides use the same Secret")
	}
}

func TestCutoverGateNotAcceptedOrProgrammed(t *testing.T) {
	route := &gateway.Route{Accepted: false, GatewayProgrammed: true}
	ready, detail := cutoverGate(route, 0)
	if ready {
		t.Errorf("expected not ready when route not Accepted, got detail %q", detail)
	}

	route2 := &gateway.Route{Accepted: true, GatewayProgrammed: false}
	ready2, detail2 := cutoverGate(route2, 0)
	if ready2 {
		t.Errorf("expected not ready when Gateway not Programmed, got detail %q", detail2)
	}
}

func TestCutoverGateNilRoute(t *testing.T) {
	ready, detail := cutoverGate(nil, 0)
	if ready {
		t.Errorf("expected not ready for nil route, got detail %q", detail)
	}
}
