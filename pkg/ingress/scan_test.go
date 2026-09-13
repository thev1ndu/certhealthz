package ingress

import "testing"

func TestHostCovered(t *testing.T) {
	tests := []struct {
		san, host string
		want      bool
	}{
		{"example.com", "example.com", true},
		{"Example.com", "example.com", true},
		{"example.com", "example.com.", true},
		{"example.com", "api.example.com", false},
		{"*.example.com", "api.example.com", true},
		{"*.example.com", "example.com", false},
		{"*.example.com", "a.b.example.com", false},
		{"*.example.com", "api.other.com", false},
		{"api.example.com", "other.example.com", false},
	}
	for _, tt := range tests {
		if got := HostCovered(tt.san, tt.host); got != tt.want {
			t.Errorf("HostCovered(%q, %q) = %v, want %v", tt.san, tt.host, got, tt.want)
		}
	}
}

func TestAnyHostCovered(t *testing.T) {
	sans := []string{"example.com", "*.example.com"}
	if !AnyHostCovered(sans, "api.example.com") {
		t.Error("expected api.example.com to be covered")
	}
	if AnyHostCovered(sans, "api.other.com") {
		t.Error("expected api.other.com to not be covered")
	}
	if AnyHostCovered(nil, "example.com") {
		t.Error("expected no SANs to cover nothing")
	}
}
