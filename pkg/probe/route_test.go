package probe

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"testing"
	"time"
)

// testCert is a self-signed leaf certificate generated for these tests,
// mirroring e2e/fixtures_test.go's generateCert helper (kept local here so
// this package's tests don't need the e2e build tag or its package).
type testCert struct {
	leaf *x509.Certificate
	tls  tls.Certificate
}

func generateTestCert(t *testing.T, host string) testCert {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		t.Fatalf("generating serial: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:         true,
		DNSNames:     []string{host},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating certificate: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshalling key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("building tls.Certificate: %v", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parsing generated certificate: %v", err)
	}
	return testCert{leaf: leaf, tls: tlsCert}
}

// startTestServer starts a local HTTPS server presenting cert, serving
// simple canned responses: any request to "/notfound" gets a 404, anything
// else gets a 200 with the request's Host header echoed in a response
// header (so tests can confirm the Host header actually reached the
// server).
func startTestServer(t *testing.T, cert testCert) string {
	t.Helper()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{cert.tls},
	})
	if err != nil {
		t.Fatalf("starting TLS listener: %v", err)
	}
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Received-Host", r.Host)
			if r.URL.Path == "/notfound" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}),
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return ln.Addr().String()
}

func certPoolFor(certs ...testCert) *x509.CertPool {
	pool := x509.NewCertPool()
	for _, c := range certs {
		pool.AddCert(c.leaf)
	}
	return pool
}

func TestTestRoute_Success(t *testing.T) {
	cert := generateTestCert(t, "route.example.com")
	addr := startTestServer(t, cert)

	result := TestRoute(context.Background(), addr, "route.example.com", "route.example.com", cert.leaf, 2*time.Second, WithRootCAs(certPoolFor(cert)))

	if !result.OK {
		t.Fatalf("expected overall OK, got %+v", result)
	}
	wantSteps := []string{"tcp_connect", "tls_handshake", "cert_match", "http_request"}
	if len(result.Steps) != len(wantSteps) {
		t.Fatalf("expected %d steps, got %d: %+v", len(wantSteps), len(result.Steps), result.Steps)
	}
	for i, name := range wantSteps {
		if result.Steps[i].Name != name {
			t.Errorf("step %d: expected %q, got %q", i, name, result.Steps[i].Name)
		}
		if !result.Steps[i].OK {
			t.Errorf("step %q: expected OK, got %+v", name, result.Steps[i])
		}
	}
}

func TestTestRoute_Unreachable(t *testing.T) {
	// Dial a port nothing is listening on.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding a free port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close() // now definitely nothing listening there

	result := TestRoute(context.Background(), addr, "route.example.com", "route.example.com", nil, 500*time.Millisecond)

	if result.OK {
		t.Fatalf("expected overall failure, got %+v", result)
	}
	if len(result.Steps) != 1 || result.Steps[0].Name != "tcp_connect" || result.Steps[0].OK {
		t.Fatalf("expected exactly one failed tcp_connect step, got %+v", result.Steps)
	}
}

func TestTestRoute_WrongSNI(t *testing.T) {
	cert := generateTestCert(t, "route.example.com")
	addr := startTestServer(t, cert)

	// SNI/expected hostname doesn't match anything in the server's cert
	// SANs — the TLS handshake's hostname verification should fail.
	result := TestRoute(context.Background(), addr, "other.example.com", "other.example.com", nil, 2*time.Second, WithRootCAs(certPoolFor(cert)))

	if result.OK {
		t.Fatalf("expected overall failure, got %+v", result)
	}
	if len(result.Steps) != 2 {
		t.Fatalf("expected exactly tcp_connect + failed tls_handshake, got %+v", result.Steps)
	}
	if !result.Steps[0].OK {
		t.Fatalf("expected tcp_connect to succeed, got %+v", result.Steps[0])
	}
	if result.Steps[1].Name != "tls_handshake" || result.Steps[1].OK {
		t.Fatalf("expected failed tls_handshake, got %+v", result.Steps[1])
	}
}

func TestTestRoute_CertMismatch(t *testing.T) {
	cert := generateTestCert(t, "route.example.com")
	otherCert := generateTestCert(t, "different.example.com")
	addr := startTestServer(t, cert)

	// The server really does present `cert`, and the handshake/hostname
	// check against "route.example.com" succeeds — but the caller expected
	// a *different* certificate (e.g. what the scan says the route's Secret
	// holds), simulating "right host, wrong backing cert".
	result := TestRoute(context.Background(), addr, "route.example.com", "route.example.com", otherCert.leaf, 2*time.Second, WithRootCAs(certPoolFor(cert)))

	if result.OK {
		t.Fatalf("expected overall failure due to cert mismatch, got %+v", result)
	}
	var sawCertMatch, sawHTTP bool
	for _, step := range result.Steps {
		switch step.Name {
		case "tcp_connect", "tls_handshake":
			if !step.OK {
				t.Errorf("expected step %q to pass, got %+v", step.Name, step)
			}
		case "cert_match":
			sawCertMatch = true
			if step.OK {
				t.Errorf("expected cert_match to fail, got %+v", step)
			}
		case "http_request":
			sawHTTP = true
			if !step.OK {
				t.Errorf("expected http_request to still run and succeed despite cert_match failure, got %+v", step)
			}
		}
	}
	if !sawCertMatch || !sawHTTP {
		t.Fatalf("expected both cert_match and http_request steps, got %+v", result.Steps)
	}
}

func TestTestRoute_NonSuccessHTTPStatus(t *testing.T) {
	cert := generateTestCert(t, "route.example.com")
	addr := startTestServer(t, cert)

	result := TestRoute(context.Background(), addr, "route.example.com", "route.example.com", nil, 2*time.Second, WithRootCAs(certPoolFor(cert)))

	// http_request against "/" (not "/notfound") returns 200 here since
	// TestRoute always requests "/" — this test instead checks that a
	// non-2xx response is still reported as a successful round-trip with
	// the status captured in Detail, so run the whole thing again against
	// a server whose root itself 404s.
	_ = result

	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert.tls}})
	if err != nil {
		t.Fatalf("starting TLS listener: %v", err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	result2 := TestRoute(context.Background(), ln.Addr().String(), "route.example.com", "route.example.com", nil, 2*time.Second, WithRootCAs(certPoolFor(cert)))
	if !result2.OK {
		t.Fatalf("expected overall OK despite a 404 (connectivity itself succeeded), got %+v", result2)
	}
	last := result2.Steps[len(result2.Steps)-1]
	if last.Name != "http_request" || !last.OK {
		t.Fatalf("expected a passing http_request step, got %+v", last)
	}
	if !contains(last.Detail, "404") {
		t.Fatalf("expected http_request detail to mention status 404, got %q", last.Detail)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (func() bool {
		for i := 0; i+len(substr) <= len(s); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	})()
}
