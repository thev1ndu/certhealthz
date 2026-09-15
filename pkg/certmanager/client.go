package certmanager

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	gatewayclientset "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"
)

func metaListOpts() metav1.ListOptions {
	return metav1.ListOptions{}
}

// NewDynamicClient builds a dynamic client from a kubeconfig path.
// Passing an empty path falls back to the default loading rules
// ($KUBECONFIG, then ~/.kube/config).
func NewDynamicClient(kubeconfigPath string) (dynamic.Interface, error) {
	cfg, err := restConfig(kubeconfigPath)
	if err != nil {
		return nil, err
	}
	return dynamic.NewForConfig(cfg)
}

// NewTypedClient builds a standard client-go clientset, used for scanning
// raw kubernetes.io/tls Secrets (cert-manager's own API doesn't expose those).
func NewTypedClient(kubeconfigPath string) (kubernetes.Interface, error) {
	cfg, err := restConfig(kubeconfigPath)
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(cfg)
}

// NewGatewayClient builds a Gateway API generated clientset from a
// kubeconfig path, the same way NewTypedClient builds a standard
// client-go clientset. Passing an empty path falls back to the default
// loading rules.
func NewGatewayClient(kubeconfigPath string) (gatewayclientset.Interface, error) {
	cfg, err := restConfig(kubeconfigPath)
	if err != nil {
		return nil, err
	}
	return gatewayclientset.NewForConfig(cfg)
}

// NewGatewayClientFromBytes is the in-memory-kubeconfig counterpart to
// NewGatewayClient.
func NewGatewayClientFromBytes(kubeconfig []byte) (gatewayclientset.Interface, error) {
	cfg, err := restConfigFromBytes(kubeconfig)
	if err != nil {
		return nil, err
	}
	return gatewayclientset.NewForConfig(cfg)
}

func restConfig(kubeconfigPath string) (*rest.Config, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfigPath != "" {
		loadingRules.ExplicitPath = kubeconfigPath
	}

	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules, &clientcmd.ConfigOverrides{},
	).ClientConfig()
}

// NewDynamicClientFromBytes builds a dynamic client from in-memory
// kubeconfig content, e.g. a file uploaded through the dashboard rather
// than read off the server's filesystem.
func NewDynamicClientFromBytes(kubeconfig []byte) (dynamic.Interface, error) {
	cfg, err := restConfigFromBytes(kubeconfig)
	if err != nil {
		return nil, err
	}
	return dynamic.NewForConfig(cfg)
}

// NewTypedClientFromBytes is the in-memory-kubeconfig counterpart to
// NewTypedClient.
func NewTypedClientFromBytes(kubeconfig []byte) (kubernetes.Interface, error) {
	cfg, err := restConfigFromBytes(kubeconfig)
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(cfg)
}

func restConfigFromBytes(kubeconfig []byte) (*rest.Config, error) {
	return clientcmd.RESTConfigFromKubeConfig(kubeconfig)
}

// ClusterName resolves the actual cluster name (e.g. "kubernetes") from a
// kubeconfig path's current context, rather than the kubeconfig file itself
// — a kubeconfig is usually named after its role (admin.conf, staging.yaml),
// not the cluster it points at. Returns "" if it can't be resolved (missing
// file, no current context, etc); callers should fall back to something
// else in that case.
func ClusterName(kubeconfigPath string) string {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfigPath != "" {
		loadingRules.ExplicitPath = kubeconfigPath
	}
	cc := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, &clientcmd.ConfigOverrides{})
	return clusterNameFromConfig(cc)
}

// ClusterNameFromBytes is the in-memory-kubeconfig counterpart to
// ClusterName, for kubeconfigs uploaded rather than read off disk.
func ClusterNameFromBytes(kubeconfig []byte) string {
	cc, err := clientcmd.NewClientConfigFromBytes(kubeconfig)
	if err != nil {
		return ""
	}
	return clusterNameFromConfig(cc)
}

func clusterNameFromConfig(cc clientcmd.ClientConfig) string {
	raw, err := cc.RawConfig()
	if err != nil || raw.CurrentContext == "" {
		return ""
	}
	ctx, ok := raw.Contexts[raw.CurrentContext]
	if !ok {
		return ""
	}
	return ctx.Cluster
}
