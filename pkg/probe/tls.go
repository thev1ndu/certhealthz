package probe

import (
	"crypto/tls"
	"fmt"
	"net"
	"time"
)

// EndpointCert is the leaf certificate observed live on a TLS endpoint,
// independent of any Kubernetes state — catches vendor/legacy certs
// cert-manager never sees.
type EndpointCert struct {
	Endpoint string
	NotAfter time.Time
	Issuer   string
	Err      error
}

// Probe dials host:port with TLS and reads the leaf certificate's expiry.
// Endpoint may be "host" (defaults to :443) or "host:port".
func Probe(endpoint string, timeout time.Duration) EndpointCert {
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		host, port = endpoint, "443"
	}
	addr := net.JoinHostPort(host, port)

	dialer := &net.Dialer{Timeout: timeout}
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{
		ServerName: host,
		MinVersion: tls.VersionTLS12,
	})
	if err != nil {
		return EndpointCert{Endpoint: endpoint, Err: fmt.Errorf("dial %s: %w", addr, err)}
	}
	defer conn.Close()

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return EndpointCert{Endpoint: endpoint, Err: fmt.Errorf("no peer certificates from %s", addr)}
	}

	leaf := certs[0]
	return EndpointCert{
		Endpoint: endpoint,
		NotAfter: leaf.NotAfter,
		Issuer:   leaf.Issuer.CommonName,
	}
}

// ProbeAll probes every endpoint concurrently and returns results in
// the same order they were given.
func ProbeAll(endpoints []string, timeout time.Duration) []EndpointCert {
	results := make([]EndpointCert, len(endpoints))
	done := make(chan struct{}, len(endpoints))

	for i, ep := range endpoints {
		go func(i int, ep string) {
			results[i] = Probe(ep, timeout)
			done <- struct{}{}
		}(i, ep)
	}

	for range endpoints {
		<-done
	}
	return results
}
