package certmanager

import (
	"os"
	"path/filepath"
	"testing"
)

const sampleKubeconfig = `
apiVersion: v1
kind: Config
current-context: kubernetes-admin@kubernetes
clusters:
  - name: kubernetes
    cluster:
      server: https://127.0.0.1:6443
contexts:
  - name: kubernetes-admin@kubernetes
    context:
      cluster: kubernetes
      user: kubernetes-admin
users:
  - name: kubernetes-admin
    user: {}
`

func TestClusterNameFromBytes(t *testing.T) {
	got := ClusterNameFromBytes([]byte(sampleKubeconfig))
	if got != "kubernetes" {
		t.Fatalf("ClusterNameFromBytes() = %q, want %q", got, "kubernetes")
	}
}

func TestClusterNameFromBytesInvalid(t *testing.T) {
	if got := ClusterNameFromBytes([]byte("not a kubeconfig")); got != "" {
		t.Fatalf("ClusterNameFromBytes() on invalid input = %q, want empty", got)
	}
}

func TestClusterNamePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "admin.conf")
	if err := os.WriteFile(path, []byte(sampleKubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}

	got := ClusterName(path)
	if got != "kubernetes" {
		t.Fatalf("ClusterName(%q) = %q, want %q", path, got, "kubernetes")
	}
}

func TestClusterNamePathMissing(t *testing.T) {
	if got := ClusterName(filepath.Join(t.TempDir(), "does-not-exist.conf")); got != "" {
		t.Fatalf("ClusterName() on missing file = %q, want empty", got)
	}
}
