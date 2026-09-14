package certmanager

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// SecretCert is the parsed x509 reality behind a kubernetes.io/tls Secret,
// used to catch Certificate objects reporting Ready while the backing
// secret has actually drifted (stale cert, failed silent renewal, etc), and
// to check Ingress routes against what the cert actually covers.
type SecretCert struct {
	Cluster   string
	Namespace string
	Name      string
	NotAfter  time.Time
	DNSNames  []string

	// ChainOK is false when the bundled chain is missing a link or has the
	// wrong one — see VerifyChain for exactly what's (and isn't) checked.
	ChainOK     bool
	ChainIssue  string
	WeakCrypto  bool
	CryptoIssue string
}

// ScanSecrets lists every kubernetes.io/tls Secret and parses its leaf
// certificate's expiry, key/signature strength, and bundled-chain
// consistency, independent of any Certificate object owning it.
func ScanSecrets(ctx context.Context, cluster string, client kubernetes.Interface) ([]SecretCert, error) {
	list, err := client.CoreV1().Secrets("").List(ctx, metav1.ListOptions{
		FieldSelector: "type=kubernetes.io/tls",
	})
	if err != nil {
		return nil, fmt.Errorf("listing tls secrets on %s: %w", cluster, err)
	}

	out := make([]SecretCert, 0, len(list.Items))
	for _, s := range list.Items {
		sc, ok := parseSecret(cluster, s)
		if ok {
			out = append(out, sc)
		}
	}
	return out, nil
}

func parseSecret(cluster string, s corev1.Secret) (SecretCert, bool) {
	leaf, chain, err := ParseSecretChain(s)
	if err != nil {
		return SecretCert{}, false
	}

	sc := SecretCert{
		Cluster:   cluster,
		Namespace: s.Namespace,
		Name:      s.Name,
		NotAfter:  leaf.NotAfter,
		DNSNames:  leaf.DNSNames,
	}

	pubAlg, pubBits := PublicKeyDetail(leaf.PublicKey)
	sc.WeakCrypto, sc.CryptoIssue = WeakCryptoIssue(pubAlg, pubBits, leaf.SignatureAlgorithm.String())
	sc.ChainOK, sc.ChainIssue = VerifyChain(leaf, chain)

	return sc, true
}

// ParseSecretChain decodes every PEM block in a kubernetes.io/tls Secret's
// tls.crt, returning the leaf certificate and any intermediates/roots that
// followed it in the same file. Unlike parseSecret (which only needs the
// leaf's expiry), this backs the dashboard's full certificate detail view.
func ParseSecretChain(s corev1.Secret) (leaf *x509.Certificate, chain []*x509.Certificate, err error) {
	raw := s.Data[corev1.TLSCertKey]
	if len(raw) == 0 {
		return nil, nil, fmt.Errorf("secret %s/%s has no %s data", s.Namespace, s.Name, corev1.TLSCertKey)
	}

	var certs []*x509.Certificate
	for len(raw) > 0 {
		var block *pem.Block
		block, raw = pem.Decode(raw)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, nil, fmt.Errorf("parsing certificate in %s/%s: %w", s.Namespace, s.Name, err)
		}
		certs = append(certs, cert)
	}
	if len(certs) == 0 {
		return nil, nil, fmt.Errorf("no certificates found in %s/%s", s.Namespace, s.Name)
	}
	return certs[0], certs[1:], nil
}

// PublicKeyDetail identifies the algorithm and key size of a parsed
// certificate's public key, for display and for weak-key detection. Shared
// by the bulk scan path (WeakCryptoIssue) and the on-demand certificate
// detail view (cmd/certdetail.go) so both agree on what a key "is".
func PublicKeyDetail(pub any) (algorithm string, bits int) {
	switch k := pub.(type) {
	case *rsa.PublicKey:
		return "RSA", k.N.BitLen()
	case *ecdsa.PublicKey:
		return "ECDSA", k.Curve.Params().BitSize
	case ed25519.PublicKey:
		return "Ed25519", 256
	default:
		return "Unknown", 0
	}
}

// weakRSABits is the minimum RSA key size still considered acceptable;
// below this, factoring is practical with modern hardware (NIST deprecated
// 1024-bit RSA in 2013, and 2048 is the current floor most CAs enforce).
const weakRSABits = 2048

// WeakCryptoIssue reports whether a certificate's key/signature algorithm
// falls below what's considered secure today, and why. Only RSA key size is
// bit-length-gated — ECDSA/Ed25519 keys aren't weak at the sizes typically
// issued (a 256-bit ECDSA key is not comparable to a 256-bit RSA key), so
// pubBits is ignored for non-RSA algorithms.
func WeakCryptoIssue(pubAlg string, pubBits int, sigAlg string) (weak bool, issue string) {
	if pubAlg == "RSA" && pubBits > 0 && pubBits < weakRSABits {
		return true, fmt.Sprintf("RSA key is only %d bits (minimum recommended: %d)", pubBits, weakRSABits)
	}
	upper := strings.ToUpper(sigAlg)
	if strings.Contains(upper, "SHA1") || strings.Contains(upper, "MD5") {
		return true, fmt.Sprintf("signed with a deprecated algorithm (%s)", sigAlg)
	}
	return false, ""
}

// VerifyChain checks that the certificate bundle alongside a leaf is
// internally consistent — every link the operator provided actually
// validates the one below it — rather than checking the leaf against the
// public system trust store. That's a deliberate choice: certs issued by a
// private/internal CA (cert-manager's own self-signed or CA issuers, or any
// internal cluster PKI) are extremely common and not "broken" just because
// they're not publicly trusted — the only real "chain" problem a scanner
// can meaningfully flag is a bundle that's missing a link or has the wrong
// one. A self-signed leaf with no bundled chain is treated as intentional
// and not flagged.
func VerifyChain(leaf *x509.Certificate, chain []*x509.Certificate) (ok bool, issue string) {
	if len(chain) == 0 {
		if bytes.Equal(leaf.RawIssuer, leaf.RawSubject) {
			return true, "" // self-signed leaf — nothing to validate against
		}
		return false, fmt.Sprintf("issued by %q but no intermediate/root certificate was bundled with it", leaf.Issuer.CommonName)
	}

	// Trust the top of the provided bundle as this chain's root — we're
	// checking the bundle's own internal consistency, not tracing it to a
	// public root. KeyUsages is Any because this checks signature/chain
	// validity, not whether the leaf declares ExtKeyUsageServerAuth.
	roots := x509.NewCertPool()
	roots.AddCert(chain[len(chain)-1])
	intermediates := x509.NewCertPool()
	for _, c := range chain[:len(chain)-1] {
		intermediates.AddCert(c)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}); err != nil {
		return false, err.Error()
	}
	return true, ""
}
