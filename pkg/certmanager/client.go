package certmanager

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
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

func restConfig(kubeconfigPath string) (*rest.Config, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfigPath != "" {
		loadingRules.ExplicitPath = kubeconfigPath
	}

	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules, &clientcmd.ConfigOverrides{},
	).ClientConfig()
}
