package certmanager

import (
	"testing"
	"time"
)

func TestCheckDrift(t *testing.T) {
	notAfter := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		cert       Certificate
		secrets    map[string]SecretCert
		wantDrift  bool
		wantDetail string
	}{
		{
			name: "no secretName, nothing to check",
			cert: Certificate{Namespace: "ns", Name: "c1"},
		},
		{
			name: "matching secret, no drift",
			cert: Certificate{Namespace: "ns", SecretName: "s1", NotAfter: notAfter},
			secrets: map[string]SecretCert{
				"ns/s1": {Namespace: "ns", Name: "s1", NotAfter: notAfter},
			},
		},
		{
			name: "within tolerance, no drift",
			cert: Certificate{Namespace: "ns", SecretName: "s1", NotAfter: notAfter},
			secrets: map[string]SecretCert{
				"ns/s1": {Namespace: "ns", Name: "s1", NotAfter: notAfter.Add(30 * time.Second)},
			},
		},
		{
			name:       "secret missing",
			cert:       Certificate{Namespace: "ns", SecretName: "s1", NotAfter: notAfter},
			secrets:    map[string]SecretCert{},
			wantDrift:  true,
			wantDetail: "Certificate reports Ready but backing Secret ns/s1 was not found",
		},
		{
			name: "secret expiry disagrees with certificate status",
			cert: Certificate{Namespace: "ns", SecretName: "s1", NotAfter: notAfter},
			secrets: map[string]SecretCert{
				"ns/s1": {Namespace: "ns", Name: "s1", NotAfter: notAfter.Add(48 * time.Hour)},
			},
			wantDrift: true,
		},
		{
			name: "certificate has no reported notAfter yet",
			cert: Certificate{Namespace: "ns", SecretName: "s1"},
			secrets: map[string]SecretCert{
				"ns/s1": {Namespace: "ns", Name: "s1", NotAfter: notAfter},
			},
		},
		{
			name: "matching dnsNames in any order, no drift",
			cert: Certificate{
				Namespace: "ns", SecretName: "s1", NotAfter: notAfter,
				DNSNames: []string{"b.example.com", "a.example.com"},
			},
			secrets: map[string]SecretCert{
				"ns/s1": {
					Namespace: "ns", Name: "s1", NotAfter: notAfter,
					DNSNames: []string{"a.example.com", "b.example.com"},
				},
			},
		},
		{
			name: "spec dnsNames no longer matches secret's actual SANs",
			cert: Certificate{
				Namespace: "ns", SecretName: "s1", NotAfter: notAfter,
				DNSNames: []string{"a.example.com", "b.example.com"},
			},
			secrets: map[string]SecretCert{
				"ns/s1": {
					Namespace: "ns", Name: "s1", NotAfter: notAfter,
					DNSNames: []string{"a.example.com"},
				},
			},
			wantDrift: true,
		},
		{
			name: "empty spec dnsNames is not itself drift",
			cert: Certificate{Namespace: "ns", SecretName: "s1", NotAfter: notAfter},
			secrets: map[string]SecretCert{
				"ns/s1": {
					Namespace: "ns", Name: "s1", NotAfter: notAfter,
					DNSNames: []string{"a.example.com"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			drifted, detail := CheckDrift(tt.cert, tt.secrets)
			if drifted != tt.wantDrift {
				t.Fatalf("drifted = %v, want %v (detail: %q)", drifted, tt.wantDrift, detail)
			}
			if tt.wantDetail != "" && detail != tt.wantDetail {
				t.Fatalf("detail = %q, want %q", detail, tt.wantDetail)
			}
		})
	}
}
