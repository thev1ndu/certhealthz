package certmanager

import (
	"fmt"
	"slices"
	"time"
)

// DriftTolerance bounds how far a Ready Certificate's reported NotAfter may
// diverge from its backing Secret's actual leaf certificate before it's
// flagged as drift. cert-manager writes status.notAfter and the Secret in
// the same reconcile, so any gap beyond clock/propagation noise means the
// Secret didn't actually receive the cert the status claims.
const DriftTolerance = 2 * time.Minute

// CheckDrift reports whether a Ready Certificate disagrees with the actual
// leaf certificate in its backing Secret — the Secret is missing, or its
// real NotAfter doesn't match what the Certificate's status claims. Only
// meaningful for a Ready Certificate; callers should skip this check
// otherwise (a not-Ready Certificate is already flagged via FailReason).
// secretsByKey is keyed "namespace/name", matching the Certificate's own
// namespace and its spec.secretName.
func CheckDrift(c Certificate, secretsByKey map[string]SecretCert) (drifted bool, detail string) {
	if c.SecretName == "" {
		return false, ""
	}
	key := c.Namespace + "/" + c.SecretName
	secret, ok := secretsByKey[key]
	if !ok {
		return true, fmt.Sprintf("Certificate reports Ready but backing Secret %s was not found", key)
	}
	if c.NotAfter.IsZero() || secret.NotAfter.IsZero() {
		return false, ""
	}

	diff := c.NotAfter.Sub(secret.NotAfter)
	if diff < 0 {
		diff = -diff
	}
	if diff > DriftTolerance {
		return true, fmt.Sprintf(
			"Certificate reports Ready with expiry %s, but Secret %s's actual leaf cert expires %s",
			c.NotAfter.Format(time.RFC3339), key, secret.NotAfter.Format(time.RFC3339),
		)
	}

	// Only meaningful when the spec actually sets dnsNames — a Certificate
	// can also get its SANs from spec.ipAddresses/uris/emailAddresses/
	// otherNames, none of which this checks, so an empty spec.dnsNames
	// isn't itself a sign of drift.
	if len(c.DNSNames) > 0 && !sameDNSNames(c.DNSNames, secret.DNSNames) {
		return true, fmt.Sprintf(
			"Certificate spec.dnsNames %v no longer matches Secret %s's actual leaf cert SANs %v",
			c.DNSNames, key, secret.DNSNames,
		)
	}
	return false, ""
}

// sameDNSNames reports whether two DNS name lists contain the same names,
// order-independent.
func sameDNSNames(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}
