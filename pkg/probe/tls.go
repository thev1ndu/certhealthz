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
	Endpoint    string
	NotAfter    time.Time
	Issuer      string
	TLSVersion  string
	CipherSuite string
	// WeakTLS and TLSIssue mirror certmanager.WeakCryptoIssue's shape for
	// the negotiated protocol itself (as opposed to the certificate's own
	// key/signature) — flags anything still accepting a deprecated
	// TLS version or cipher suite.
	WeakTLS  bool
	TLSIssue string
	Err      error
}

// dialPeerCertificates dials endpoint with TLS and returns the full peer
// certificate chain as presented by the server (leaf first), plus the
// negotiated connection state (protocol version/cipher). Shared by Probe
// (which needs the leaf's expiry/issuer and the negotiated crypto) and
// ProbeChain (which needs the whole chain for the dashboard's certificate
// detail view).
func dialPeerCertificates(endpoint string, timeout time.Duration, opts ...Option) ([]*x509.Certificate, tls.ConnectionState, error) {
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		host, port = endpoint, "443"
	}
	addr := net.JoinHostPort(host, port)

	// MinVersion is deliberately TLS 1.0, not the usual 1.2 floor: a
	// deprecated version the server still accepts is exactly what
	// weakTLSIssue needs to detect and flag — refusing to negotiate it
	// here would just turn that into a generic dial error instead.
	cfg := &tls.Config{ //nolint:gosec // low MinVersion is intentional — see comment above
		ServerName: host,
		MinVersion: tls.VersionTLS10,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	dialer := &net.Dialer{Timeout: timeout}
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, cfg)
	if err != nil {
		return nil, tls.ConnectionState{}, fmt.Errorf("dial %s: %w", addr, err)
	}
	defer conn.Close()

	state := conn.ConnectionState()
	certs := state.PeerCertificates
	if len(certs) == 0 {
		return nil, tls.ConnectionState{}, fmt.Errorf("no peer certificates from %s", addr)
	}
	return certs, state, nil
}

// tlsVersionName renders a negotiated tls.ConnectionState.Version as a
// human-readable protocol name.
func tlsVersionName(v uint16) string {
	switch v {
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	default:
		return fmt.Sprintf("unknown (0x%04x)", v)
	}
}

// Probe dials host:port with TLS and reads the leaf certificate's expiry
// and the negotiated protocol version/cipher suite. Endpoint may be "host"
// (defaults to :443) or "host:port".
func Probe(endpoint string, timeout time.Duration, opts ...Option) EndpointCert {
	certs, state, err := dialPeerCertificates(endpoint, timeout, opts...)
	if err != nil {
		return EndpointCert{Endpoint: endpoint, Err: err}
	}

	leaf := certs[0]
	weak, issue := weakTLSIssue(state.Version, state.CipherSuite)
	return EndpointCert{
		Endpoint:    endpoint,
		NotAfter:    leaf.NotAfter,
		Issuer:      leaf.Issuer.CommonName,
		TLSVersion:  tlsVersionName(state.Version),
		CipherSuite: tls.CipherSuiteName(state.CipherSuite),
		WeakTLS:     weak,
		TLSIssue:    issue,
	}
}

// weakTLSIssue reports whether the negotiated protocol version or cipher
// suite is one considered deprecated today: below TLS 1.2, or a suite Go's
// standard library itself classifies as insecure (tls.InsecureCipherSuites
// — RC4, 3DES, CBC-mode suites with known padding-oracle history, etc).
func weakTLSIssue(version, cipherSuite uint16) (weak bool, issue string) {
	if version < tls.VersionTLS12 {
		return true, fmt.Sprintf("negotiated %s, below the TLS 1.2 minimum", tlsVersionName(version))
	}
	for _, c := range tls.InsecureCipherSuites() {
		if c.ID == cipherSuite {
			return true, fmt.Sprintf("negotiated insecure cipher suite %s", tls.CipherSuiteName(cipherSuite))
		}
	}
	return false, ""
}

// ProbeChain dials endpoint with TLS and returns the full peer certificate
// chain (leaf first, then any intermediates/root the server presented),
// backing the dashboard's certificate detail view.
func ProbeChain(endpoint string, timeout time.Duration, opts ...Option) (leaf *x509.Certificate, chain []*x509.Certificate, err error) {
	certs, _, err := dialPeerCertificates(endpoint, timeout, opts...)
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
