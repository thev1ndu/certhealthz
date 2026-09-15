package probe

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// RouteTestStep is the outcome of one discrete step of a synthetic route
// test (see TestRoute) — reported individually, rather than folded into a
// single aggregate pass/fail, so the dashboard can show exactly which layer
// of the connection failed.
type RouteTestStep struct {
	Name       string `json:"name"`
	OK         bool   `json:"ok"`
	Detail     string `json:"detail"`
	DurationMs int64  `json:"durationMs"`
}

// RouteTestResult is the full outcome of a synthetic route test: every step
// that ran, plus an overall OK that's true only if every step that ran
// passed. A step that could not run because an earlier step failed (e.g.
// tls_handshake never runs if tcp_connect failed) is simply absent from
// Steps rather than reported as a synthetic failure.
type RouteTestResult struct {
	Steps []RouteTestStep `json:"steps"`
	OK    bool            `json:"ok"`
}

// addStep appends a step outcome and folds it into the running OK: this is
// a small helper local to TestRoute's construction.
func (r *RouteTestResult) addStep(name string, ok bool, detail string, dur time.Duration) {
	r.Steps = append(r.Steps, RouteTestStep{Name: name, OK: ok, Detail: detail, DurationMs: dur.Milliseconds()})
	if !ok {
		r.OK = false
	}
}

// TestRoute runs a synthetic, on-demand connectivity check against one
// already-scanned route: TCP dial, TLS handshake (SNI forced to sni, the
// same override mechanism pkg/migration's cutoverGate uses via Option),
// live-certificate comparison against expectedCert (the Secret the scan
// says backs this route — catching a Gateway/Ingress that's up and healthy
// but serving the wrong certificate), and an HTTP GET with Host: hostHeader
// issued over that same TLS connection (no second handshake).
//
// dialAddr is an "ip:port" or "host:port" to dial directly — TestRoute never
// resolves or accepts a caller-supplied URL; the caller (cmd.handleTestRoute)
// is responsible for deriving dialAddr from data the scanner already
// discovered (a published LB/Gateway address, or a port-forwarded local
// address), never from arbitrary user input.
//
// expectedCert may be nil (e.g. the route's Secret couldn't be resolved),
// in which case cert_match is reported as skipped rather than failed.
func TestRoute(ctx context.Context, dialAddr, sni, hostHeader string, expectedCert *x509.Certificate, timeout time.Duration, opts ...Option) RouteTestResult {
	result := RouteTestResult{OK: true}

	dialer := &net.Dialer{Timeout: timeout}
	start := time.Now()
	conn, err := dialer.DialContext(ctx, "tcp", dialAddr)
	dur := time.Since(start)
	if err != nil {
		result.addStep("tcp_connect", false, err.Error(), dur)
		return result
	}
	result.addStep("tcp_connect", true, fmt.Sprintf("connected to %s", dialAddr), dur)

	cfg := &tls.Config{ //nolint:gosec // MinVersion below is the real floor; no low-version allowance here like dialPeerCertificates
		ServerName: sni,
		MinVersion: tls.VersionTLS12,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	tlsConn := tls.Client(conn, cfg)
	if deadline, ok := ctx.Deadline(); ok {
		_ = tlsConn.SetDeadline(deadline)
	} else if timeout > 0 {
		_ = tlsConn.SetDeadline(time.Now().Add(timeout))
	}
	defer tlsConn.Close()

	start = time.Now()
	err = tlsConn.HandshakeContext(ctx)
	dur = time.Since(start)
	if err != nil {
		result.addStep("tls_handshake", false, err.Error(), dur)
		return result
	}
	state := tlsConn.ConnectionState()
	result.addStep("tls_handshake", true, fmt.Sprintf("negotiated %s with SNI %q", tlsVersionName(state.Version), sni), dur)

	start = time.Now()
	switch {
	case expectedCert == nil:
		result.addStep("cert_match", true, "no expected certificate to compare against; skipped", time.Since(start))
	case len(state.PeerCertificates) == 0:
		result.addStep("cert_match", false, "server presented no certificate", time.Since(start))
	case bytes.Equal(state.PeerCertificates[0].Raw, expectedCert.Raw):
		result.addStep("cert_match", true, fmt.Sprintf("presented certificate matches expected Secret certificate (serial %s)", state.PeerCertificates[0].SerialNumber), time.Since(start))
	default:
		presented := state.PeerCertificates[0]
		result.addStep("cert_match", false, fmt.Sprintf(
			"presented certificate (serial %s, CN %q) does not match the Secret's certificate (serial %s, CN %q) — this route is serving the wrong certificate",
			presented.SerialNumber, presented.Subject.CommonName, expectedCert.SerialNumber, expectedCert.Subject.CommonName,
		), time.Since(start))
	}

	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			// Reuse the connection we already handshook above instead of
			// dialing (and handshaking) a second time.
			DialTLSContext: func(context.Context, string, string) (net.Conn, error) {
				return tlsConn, nil
			},
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+hostHeader+"/", nil)
	if err != nil {
		result.addStep("http_request", false, err.Error(), 0)
		return result
	}
	req.Host = hostHeader

	start = time.Now()
	resp, err := client.Do(req)
	dur = time.Since(start)
	if err != nil {
		result.addStep("http_request", false, err.Error(), dur)
		return result
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))

	detail := fmt.Sprintf("HTTP %d", resp.StatusCode)
	if loc := resp.Header.Get("Location"); loc != "" {
		detail += fmt.Sprintf(", Location: %s", loc)
	}
	// The request round-tripping successfully is what this step reports —
	// a 4xx/5xx from the backend is informational (captured in Detail),
	// not itself a connectivity failure worth failing the overall test on.
	result.addStep("http_request", true, detail, dur)

	return result
}
