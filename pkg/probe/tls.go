package probe

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"time"
)

// Option customizes the TLS dial config used by Probe/ProbeAll. The zero
// value (no options) preserves the default behavior: verify against the
// system trust store.
type Option func(*tls.Config)

// WithRootCAs overrides the trust store used to verify the peer
// certificate, e.g. for tests dialing a server with a self-signed cert.
func WithRootCAs(pool *x509.CertPool) Option {
	return func(cfg *tls.Config) { cfg.RootCAs = pool }
}

// EndpointCert is the leaf certificate observed live on a TLS endpoint,
// independent of any Kubernetes state — catches vendor/legacy certs
// cert-manager never sees.
type EndpointCert struct {
	Endpoint string
	NotAfter time.Time
	Issuer   string
	Err      error
}

// dialPeerCertificates dials endpoint with TLS and returns the full peer
// certificate chain as presented by the server (leaf first). Shared by Probe
// (which only needs the leaf's expiry/issuer) and ProbeChain (which needs
// the whole chain for the dashboard's certificate detail view).
func dialPeerCertificates(endpoint string, timeout time.Duration, opts ...Option) ([]*x509.Certificate, error) {
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		host, port = endpoint, "443"
	}
	addr := net.JoinHostPort(host, port)

	cfg := &tls.Config{
		ServerName: host,
		MinVersion: tls.VersionTLS12,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	dialer := &net.Dialer{Timeout: timeout}
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, cfg)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	defer conn.Close()

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return nil, fmt.Errorf("no peer certificates from %s", addr)
	}
	return certs, nil
}

// Probe dials host:port with TLS and reads the leaf certificate's expiry.
// Endpoint may be "host" (defaults to :443) or "host:port".
func Probe(endpoint string, timeout time.Duration, opts ...Option) EndpointCert {
	certs, err := dialPeerCertificates(endpoint, timeout, opts...)
	if err != nil {
		return EndpointCert{Endpoint: endpoint, Err: err}
	}

	leaf := certs[0]
	return EndpointCert{
		Endpoint: endpoint,
		NotAfter: leaf.NotAfter,
		Issuer:   leaf.Issuer.CommonName,
	}
}

// ProbeChain dials endpoint with TLS and returns the full peer certificate
// chain (leaf first, then any intermediates/root the server presented),
// backing the dashboard's certificate detail view.
func ProbeChain(endpoint string, timeout time.Duration, opts ...Option) (leaf *x509.Certificate, chain []*x509.Certificate, err error) {
	certs, err := dialPeerCertificates(endpoint, timeout, opts...)
	if err != nil {
		return nil, nil, err
	}
	return certs[0], certs[1:], nil
}

// ProbeAll probes every endpoint concurrently and returns results in
// the same order they were given.
func ProbeAll(endpoints []string, timeout time.Duration, opts ...Option) []EndpointCert {
	results := make([]EndpointCert, len(endpoints))
	done := make(chan struct{}, len(endpoints))

	for i, ep := range endpoints {
		go func(i int, ep string) {
			results[i] = Probe(ep, timeout, opts...)
			done <- struct{}{}
		}(i, ep)
	}

	for range endpoints {
		<-done
	}
	return results
}
