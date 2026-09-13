//go:build e2e

// Package e2e drives certhealthz's real scan/history/alert/probe/dashboard
// code paths end-to-end against fake Kubernetes clientsets and local TLS
// listeners — no real cluster, no network dependency. Run with:
//
//	go test -tags=e2e ./e2e/...
package e2e

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"testing"
	"time"

	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"

	"github.com/thev1ndu/certhealthz/pkg/history"
)

var certGVR = schema.GroupVersionResource{Group: "cert-manager.io", Version: "v1", Resource: "certificates"}

// generatedCert is a self-signed leaf certificate produced for a test
// fixture, in both x509 and tls.Certificate form.
type generatedCert struct {
	Leaf    *x509.Certificate
	CertPEM []byte
	KeyPEM  []byte
	TLS     tls.Certificate
}

// generateCert creates a self-signed ECDSA leaf certificate for host,
// expiring at notAfter. Used to drive real x509 parsing/classification
// through the actual code paths instead of asserting against fake dates.
func generateCert(t testing.TB, host string, notAfter time.Time) generatedCert {
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
		Subject:      pkix.Name{CommonName: host, Organization: []string{"certhealthz e2e"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:         true,
		DNSNames:     []string{host},
	}
	if ip := net.ParseIP(host); ip != nil {
		template.IPAddresses = []net.IP{ip}
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

	return generatedCert{Leaf: leaf, CertPEM: certPEM, KeyPEM: keyPEM, TLS: tlsCert}
}

// certPool builds a trust store containing the given (self-signed) certs,
// so probe.WithRootCAs can be used to verify a test TLS server without
// weakening probe's default system-trust verification.
func certPool(certs ...generatedCert) *x509.CertPool {
	pool := x509.NewCertPool()
	for _, c := range certs {
		pool.AddCert(c.Leaf)
	}
	return pool
}

// newTLSSecret builds a kubernetes.io/tls Secret carrying the given
// generated certificate, matching the shape certmanager.ScanSecrets parses.
// All fixtures live in a single namespace; add a namespace param back if a
// future test needs cross-namespace scanning.
func newTLSSecret(name string, cert generatedCert) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: name},
		Type:       corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       cert.CertPEM,
			corev1.TLSPrivateKeyKey: cert.KeyPEM,
		},
	}
}

// newCertificateCR builds an unstructured cert-manager.io/v1 Certificate,
// matching the fields certmanager.Scan reads (spec.secretName,
// status.notAfter, status.renewalTime, status.conditions[].{type,status,reason}).
func newCertificateCR(namespace, name, secretName string, notAfter time.Time, ready bool, failReason string) *unstructured.Unstructured {
	condStatus := "False"
	if ready {
		condStatus = "True"
	}
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "cert-manager.io/v1",
			"kind":       "Certificate",
			"metadata": map[string]interface{}{
				"namespace": namespace,
				"name":      name,
			},
			"spec": map[string]interface{}{
				"secretName": secretName,
			},
			"status": map[string]interface{}{
				"notAfter": notAfter.UTC().Format(time.RFC3339),
				"conditions": []interface{}{
					map[string]interface{}{
						"type":   "Ready",
						"status": condStatus,
						"reason": failReason,
					},
				},
			},
		},
	}
	return obj
}

// newFakeDynamicClient builds a dynamic.Interface seeded with the given
// Certificate CRs, in place of certmanager.NewDynamicClient's real
// kubeconfig-backed client.
func newFakeDynamicClient(certs ...*unstructured.Unstructured) *dynamicfake.FakeDynamicClient {
	objs := make([]runtime.Object, len(certs))
	for i, c := range certs {
		objs[i] = c
	}
	// WithCustomListKinds (rather than NewSimpleDynamicClient) is required
	// even in the zero-object case: the plain constructor infers list kinds
	// from the seeded objects, so it panics on a List call when no
	// Certificate was seeded at all (see e.g. the empty-cluster history test).
	listKinds := map[schema.GroupVersionResource]string{certGVR: "CertificateList"}
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds, objs...)
}

// newFakeTypedClient builds a kubernetes.Interface seeded with the given
// objects (Secrets, Ingresses, ...), in place of certmanager.NewTypedClient's
// real kubeconfig-backed client.
func newFakeTypedClient(objs ...runtime.Object) kubernetes.Interface {
	return kubernetesfake.NewSimpleClientset(objs...)
}

// newIngress builds an Ingress with a single TLS block, matching the shape
// ingress.Scan reads (spec.tls[].secretName, spec.tls[].hosts).
func newIngress(namespace, name, secretName string, hosts ...string) *networkingv1.Ingress {
	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec: networkingv1.IngressSpec{
			TLS: []networkingv1.IngressTLS{
				{Hosts: hosts, SecretName: secretName},
			},
		},
	}
}

// newTestHistoryStore opens a history.Store backed by a temp file, closed
// automatically at test cleanup — in place of the real --db-backed store
// runUI opens.
func newTestHistoryStore(t testing.TB) *history.Store {
	t.Helper()
	store, err := history.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatalf("opening test history store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// startTLSServer listens on 127.0.0.1 with the given certificate and
// returns its address plus a cleanup func. Used to drive probe.Probe
// against a real TLS handshake.
func startTLSServer(t testing.TB, cert generatedCert) string {
	t.Helper()

	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{cert.TLS},
	})
	if err != nil {
		t.Fatalf("starting tls listener: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Probe only needs the handshake (to read the peer cert), but
			// closing before it completes races the client into a reset
			// instead of a clean cert readout, so drive it to completion
			// server-side before tearing the connection down.
			if tlsConn, ok := conn.(*tls.Conn); ok {
				_ = tlsConn.Handshake()
			}
			conn.Close()
		}
	}()

	return ln.Addr().String()
}
