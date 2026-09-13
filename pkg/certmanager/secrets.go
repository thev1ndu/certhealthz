package certmanager

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// SecretCert is the parsed x509 reality behind a kubernetes.io/tls Secret,
// used to catch Certificate objects reporting Ready while the backing
// secret has actually drifted (stale cert, failed silent renewal, etc).
type SecretCert struct {
	Cluster   string
	Namespace string
	Name      string
	NotAfter  time.Time
}

// ScanSecrets lists every kubernetes.io/tls Secret and parses its leaf
// certificate's NotAfter, independent of any Certificate object owning it.
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
	raw := s.Data[corev1.TLSCertKey]
	if len(raw) == 0 {
		return SecretCert{}, false
	}

	block, _ := pem.Decode(raw)
	if block == nil {
		return SecretCert{}, false
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return SecretCert{}, false
	}

	return SecretCert{
		Cluster:   cluster,
		Namespace: s.Namespace,
		Name:      s.Name,
		NotAfter:  cert.NotAfter,
	}, true
}
