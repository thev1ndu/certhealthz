package mesh

import (
	"context"
	"testing"

	kubernetesfake "k8s.io/client-go/kubernetes/fake"
)

func TestCRDInstalledNilClient(t *testing.T) {
	if CRDInstalled(nil) {
		t.Error("expected CRDInstalled(nil) to be false")
	}
	if TraefikCRDInstalled(nil) {
		t.Error("expected TraefikCRDInstalled(nil) to be false")
	}
}

// TestScanFailsSoftWithoutCRDs verifies the core Phase 3 requirement: a
// cluster with no Istio/Traefik CRDs installed (the overwhelming majority
// of clusters) must produce an empty result and no error, never a panic or
// a propagated "resource not found" error.
func TestScanFailsSoftWithoutCRDs(t *testing.T) {
	kubeClient := kubernetesfake.NewSimpleClientset()

	routes, err := ScanIstio(context.Background(), "test", nil, kubeClient)
	if err != nil {
		t.Fatalf("ScanIstio: unexpected error: %v", err)
	}
	if routes != nil {
		t.Errorf("expected nil routes when Istio CRDs aren't installed, got %+v", routes)
	}

	troutes, err := ScanTraefik(context.Background(), "test", nil, kubeClient)
	if err != nil {
		t.Fatalf("ScanTraefik: unexpected error: %v", err)
	}
	if troutes != nil {
		t.Errorf("expected nil routes when Traefik CRDs aren't installed, got %+v", troutes)
	}
}

func TestCRDInstalledFalseForUnregisteredGroupVersion(t *testing.T) {
	kubeClient := kubernetesfake.NewSimpleClientset()

	// The fake clientset's discovery doesn't know about any GroupVersion
	// unless resources are registered on it explicitly; without that, an
	// unregistered GV like Istio's must be treated as "not installed"
	// (this is exactly the "most clusters don't have Istio" case).
	if CRDInstalled(kubeClient) {
		t.Error("expected an unregistered GroupVersion to report CRDInstalled=false")
	}
}

func TestResolveIstioCredential(t *testing.T) {
	tests := []struct {
		gatewayNamespace, credentialName string
		wantNamespace, wantName          string
	}{
		{"istio-system", "my-cert", "istio-system", "my-cert"},
		{"istio-system", "kubernetes-gateway://web/my-cert", "web", "my-cert"},
		{"istio-system", "kubernetes-gateway://my-cert", "istio-system", "my-cert"},
	}
	for _, tt := range tests {
		ns, name := resolveIstioCredential(tt.gatewayNamespace, tt.credentialName)
		if ns != tt.wantNamespace || name != tt.wantName {
			t.Errorf("resolveIstioCredential(%q, %q) = (%q, %q), want (%q, %q)",
				tt.gatewayNamespace, tt.credentialName, ns, name, tt.wantNamespace, tt.wantName)
		}
	}
}

func TestExtractHosts(t *testing.T) {
	tests := []struct {
		rule string
		want []string
	}{
		{"Host(`example.com`)", []string{"example.com"}},
		{"Host(`a.example.com`, `b.example.com`)", []string{"a.example.com", "b.example.com"}},
		{"Host(`a.example.com`) && PathPrefix(`/api`)", []string{"a.example.com"}},
		{"HostSNI(`*.example.com`)", []string{"*.example.com"}},
		{"PathPrefix(`/api`)", nil},
	}
	for _, tt := range tests {
		got := extractHosts(tt.rule)
		if len(got) != len(tt.want) {
			t.Errorf("extractHosts(%q) = %v, want %v", tt.rule, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("extractHosts(%q) = %v, want %v", tt.rule, got, tt.want)
				break
			}
		}
	}
}
