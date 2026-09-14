package cmd

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha1" //nolint:gosec // SHA1 fingerprint is a standard x509 display field, not used for security
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/thev1ndu/certhealthz/pkg/certmanager"
	"github.com/thev1ndu/certhealthz/pkg/ingress"
	"github.com/thev1ndu/certhealthz/pkg/probe"
)

// apiChainCert describes one non-leaf certificate (intermediate or root) in
// a chain, in the certificate detail response.
type apiChainCert struct {
	Subject  string    `json:"subject"`
	Issuer   string    `json:"issuer"`
	NotAfter time.Time `json:"notAfter"`
	IsCA     bool      `json:"isCA"`
}

// apiCertDetail is the JSON shape of GET /api/certs/detail — every field the
// dashboard can show about one certificate, derived directly from the
// parsed *x509.Certificate rather than the summary fields apiRow carries.
type apiCertDetail struct {
	ID        string `json:"id"`
	Source    string `json:"source"`
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`

	Subject   string `json:"subject"`
	SubjectCN string `json:"subjectCommonName"`
	Issuer    string `json:"issuer"`
	IssuerCN  string `json:"issuerCommonName"`

	SerialNumber string    `json:"serialNumber"`
	Version      int       `json:"version"`
	NotBefore    time.Time `json:"notBefore"`
	NotAfter     time.Time `json:"notAfter"`

	DNSNames       []string `json:"dnsNames"`
	IPAddresses    []string `json:"ipAddresses"`
	EmailAddresses []string `json:"emailAddresses"`
	URIs           []string `json:"uris"`

	SignatureAlgorithm string `json:"signatureAlgorithm"`
	PublicKeyAlgorithm string `json:"publicKeyAlgorithm"`
	PublicKeyBits      int    `json:"publicKeyBits"`
	FingerprintSHA1    string `json:"fingerprintSha1"`
	FingerprintSHA256  string `json:"fingerprintSha256"`

	IsCA        bool     `json:"isCA"`
	KeyUsage    []string `json:"keyUsage"`
	ExtKeyUsage []string `json:"extKeyUsage"`

	OCSPServers           []string `json:"ocspServers"`
	CRLDistributionPoints []string `json:"crlDistributionPoints"`

	Chain []apiChainCert `json:"chain"`
}

var keyUsageNames = map[x509.KeyUsage]string{
	x509.KeyUsageDigitalSignature:  "Digital Signature",
	x509.KeyUsageContentCommitment: "Content Commitment",
	x509.KeyUsageKeyEncipherment:   "Key Encipherment",
	x509.KeyUsageDataEncipherment:  "Data Encipherment",
	x509.KeyUsageKeyAgreement:      "Key Agreement",
	x509.KeyUsageCertSign:          "Certificate Sign",
	x509.KeyUsageCRLSign:           "CRL Sign",
	x509.KeyUsageEncipherOnly:      "Encipher Only",
	x509.KeyUsageDecipherOnly:      "Decipher Only",
}

var extKeyUsageNames = map[x509.ExtKeyUsage]string{
	x509.ExtKeyUsageServerAuth:      "Server Auth",
	x509.ExtKeyUsageClientAuth:      "Client Auth",
	x509.ExtKeyUsageCodeSigning:     "Code Signing",
	x509.ExtKeyUsageEmailProtection: "Email Protection",
	x509.ExtKeyUsageTimeStamping:    "Time Stamping",
	x509.ExtKeyUsageOCSPSigning:     "OCSP Signing",
	x509.ExtKeyUsageAny:             "Any",
}

func keyUsageStrings(u x509.KeyUsage) []string {
	var out []string
	for bit, name := range keyUsageNames {
		if u&bit != 0 {
			out = append(out, name)
		}
	}
	return out
}

func extKeyUsageStrings(usages []x509.ExtKeyUsage) []string {
	out := make([]string, 0, len(usages))
	for _, u := range usages {
		if name, ok := extKeyUsageNames[u]; ok {
			out = append(out, name)
		} else {
			out = append(out, "Unknown")
		}
	}
	return out
}

func publicKeyDetail(pub any) (algorithm string, bits int) {
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

// certToDetail extracts every field the dashboard's detail view can show
// from a parsed leaf certificate and its chain (intermediates/root, leaf
// excluded), the shared core of handleCertDetail regardless of which source
// (cert-manager Secret, raw Secret, or live TLS probe) produced them.
func certToDetail(id rowIdentity, leaf *x509.Certificate, chain []*x509.Certificate) apiCertDetail {
	sha1Sum := sha1.Sum(leaf.Raw) //nolint:gosec // SHA1 fingerprint is a standard x509 display field, not used for security
	sha256Sum := sha256.Sum256(leaf.Raw)
	pubAlg, pubBits := publicKeyDetail(leaf.PublicKey)

	apiChain := make([]apiChainCert, 0, len(chain))
	for _, c := range chain {
		apiChain = append(apiChain, apiChainCert{
			Subject:  c.Subject.String(),
			Issuer:   c.Issuer.String(),
			NotAfter: c.NotAfter,
			IsCA:     c.IsCA,
		})
	}

	return apiCertDetail{
		ID:        id.encode(),
		Source:    id.Source,
		Cluster:   id.Cluster,
		Namespace: id.Namespace,
		Name:      id.Name,

		Subject:   leaf.Subject.String(),
		SubjectCN: leaf.Subject.CommonName,
		Issuer:    leaf.Issuer.String(),
		IssuerCN:  leaf.Issuer.CommonName,

		SerialNumber: leaf.SerialNumber.String(),
		Version:      leaf.Version,
		NotBefore:    leaf.NotBefore,
		NotAfter:     leaf.NotAfter,

		DNSNames:       leaf.DNSNames,
		IPAddresses:    ipStrings(leaf.IPAddresses),
		EmailAddresses: leaf.EmailAddresses,
		URIs:           uriStrings(leaf.URIs),

		SignatureAlgorithm: leaf.SignatureAlgorithm.String(),
		PublicKeyAlgorithm: pubAlg,
		PublicKeyBits:      pubBits,
		FingerprintSHA1:    fmt.Sprintf("%x", sha1Sum),
		FingerprintSHA256:  fmt.Sprintf("%x", sha256Sum),

		IsCA:        leaf.IsCA,
		KeyUsage:    keyUsageStrings(leaf.KeyUsage),
		ExtKeyUsage: extKeyUsageStrings(leaf.ExtKeyUsage),

		OCSPServers:           leaf.OCSPServer,
		CRLDistributionPoints: leaf.CRLDistributionPoints,

		Chain: apiChain,
	}
}

func ipStrings(ips []net.IP) []string {
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		out = append(out, ip.String())
	}
	return out
}

func uriStrings(uris []*url.URL) []string {
	out := make([]string, 0, len(uris))
	for _, u := range uris {
		out = append(out, u.String())
	}
	return out
}

// parseIngressRowName reverses ingressRoutesRows' composite row name
// ("<ingress>" or "<ingress> (<host>)") back into its parts, so
// handleCertDetail can re-find the originating Route.
func parseIngressRowName(name string) (ingressName, host string) {
	if i := strings.Index(name, " ("); i != -1 && strings.HasSuffix(name, ")") {
		return name[:i], name[i+2 : len(name)-1]
	}
	return name, ""
}

// findClusterClients builds clients for every configured cluster and
// returns the one whose real (possibly kubeconfig-renamed) label matches.
func findClusterClients(entries []ClusterEntry, cluster string, includeSecrets bool) (ClusterClients, error) {
	all, err := buildAllClusterClients(entries, includeSecrets)
	if err != nil {
		return ClusterClients{}, err
	}
	for _, cc := range all {
		if cc.Label == cluster {
			return cc, nil
		}
	}
	return ClusterClients{}, fmt.Errorf("cluster %q not found", cluster)
}

// handleCertDetail resolves ?id= back to a specific certificate and returns
// its full parsed detail. Unlike /api/certs, this re-fetches/re-dials just
// the one cert on demand rather than a cached snapshot, since there is no
// cert cache — this is a low-frequency, operator-triggered lookup.
func handleCertDetail(deps UIDeps, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id, err := decodeRowID(r.URL.Query().Get("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	var detail apiCertDetail
	switch id.Source {
	case "endpoint":
		leaf, chain, err := probe.ProbeChain(id.Name, uiProbeTimeout)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		detail = certToDetail(id, leaf, chain)

	case "secret":
		cc, err := deps.ClusterClientsFor(id.Cluster)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		secret, err := cc.Typed.CoreV1().Secrets(id.Namespace).Get(ctx, id.Name, metav1.GetOptions{})
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		leaf, chain, err := certmanager.ParseSecretChain(*secret)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		detail = certToDetail(id, leaf, chain)

	case "cert-manager":
		cc, err := deps.ClusterClientsFor(id.Cluster)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		certs, err := certmanager.Scan(ctx, cc.Label, cc.Dyn)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		var secretName string
		found := false
		for _, c := range certs {
			if c.Namespace == id.Namespace && c.Name == id.Name {
				secretName = c.SecretName
				found = true
				break
			}
		}
		if !found || secretName == "" {
			http.Error(w, "certificate not found", http.StatusNotFound)
			return
		}
		secret, err := cc.Typed.CoreV1().Secrets(id.Namespace).Get(ctx, secretName, metav1.GetOptions{})
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		leaf, chain, err := certmanager.ParseSecretChain(*secret)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		detail = certToDetail(id, leaf, chain)

	case "ingress":
		cc, err := deps.ClusterClientsFor(id.Cluster)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		routes, err := ingress.Scan(ctx, cc.Label, cc.Typed)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		ingressName, _ := parseIngressRowName(id.Name)
		var secretName string
		found := false
		for _, route := range routes {
			if route.Namespace == id.Namespace && route.Ingress == ingressName {
				secretName = route.SecretName
				found = true
				break
			}
		}
		if !found || secretName == "" {
			http.Error(w, "ingress route not found", http.StatusNotFound)
			return
		}
		secret, err := cc.Typed.CoreV1().Secrets(id.Namespace).Get(ctx, secretName, metav1.GetOptions{})
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		leaf, chain, err := certmanager.ParseSecretChain(*secret)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		detail = certToDetail(id, leaf, chain)

	default:
		http.Error(w, "unknown source", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(detail); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
