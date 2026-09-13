//go:build e2e

package e2e

import (
	"testing"
	"time"

	"github.com/thev1ndu/certhealthz/pkg/probe"
)

// TestProbeEndToEnd dials a real local TLS listener and asserts the parsed
// leaf certificate matches what was generated for it.
func TestProbeEndToEnd(t *testing.T) {
	notAfter := time.Now().Add(30 * 24 * time.Hour).Truncate(time.Second)
	cert := generateCert(t, "127.0.0.1", notAfter)
	addr := startTLSServer(t, cert)

	result := probe.Probe(addr, 2*time.Second, probe.WithRootCAs(certPool(cert)))
	if result.Err != nil {
		t.Fatalf("Probe: unexpected error: %v", result.Err)
	}
	if !result.NotAfter.Equal(notAfter) {
		t.Errorf("expected NotAfter %v, got %v", notAfter, result.NotAfter)
	}
	if result.Issuer != "127.0.0.1" {
		t.Errorf("expected issuer CN 127.0.0.1, got %q", result.Issuer)
	}
}

// TestProbeUnreachableEndToEnd asserts a dial failure surfaces as Err rather
// than a zero-value success.
func TestProbeUnreachableEndToEnd(t *testing.T) {
	// Port 0 on an address with nothing listening; dialing should fail fast
	// with the given short timeout rather than hang.
	result := probe.Probe("127.0.0.1:1", 500*time.Millisecond)
	if result.Err == nil {
		t.Fatal("expected an error probing an unreachable endpoint, got nil")
	}
	if !result.NotAfter.IsZero() {
		t.Errorf("expected zero NotAfter on failure, got %v", result.NotAfter)
	}
}
