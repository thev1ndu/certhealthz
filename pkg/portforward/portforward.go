// Package portforward opens a client-go SPDY port-forward to a ready pod
// behind a Kubernetes Service, for the dashboard's synthetic route test
// (see cmd.handleTestRoute) to reach a ClusterIP-only route — one whose
// Ingress/Gateway has no published external address, e.g. a bare-metal
// cluster with no LoadBalancer support.
package portforward

import (
	"context"
	"fmt"
	"net/http"

	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// resolveReadyPod finds one ready pod backing a Service, via its
// EndpointSlices (the current API — Endpoints is deprecated as of
// Kubernetes 1.33) rather than the Service's own selector, so this also
// works for Services with no selector backed by manually managed
// Endpoints/EndpointSlices.
func resolveReadyPod(ctx context.Context, kubeClient kubernetes.Interface, namespace, serviceName string) (podName string, err error) {
	slices, err := kubeClient.DiscoveryV1().EndpointSlices(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("kubernetes.io/service-name=%s", serviceName),
	})
	if err != nil {
		return "", fmt.Errorf("listing endpointslices for %s/%s: %w", namespace, serviceName, err)
	}

	for _, slice := range slices.Items {
		for _, ep := range slice.Endpoints {
			if !endpointReady(ep) {
				continue
			}
			if ep.TargetRef == nil || ep.TargetRef.Kind != "Pod" {
				continue
			}
			return ep.TargetRef.Name, nil
		}
	}
	return "", fmt.Errorf("no ready pod found behind Service %s/%s", namespace, serviceName)
}

// endpointReady reports whether ep should be treated as ready to receive
// traffic — a nil Ready condition means ready per the API's own documented
// default, matching how kube-proxy itself interprets it.
func endpointReady(ep discoveryv1.Endpoint) bool {
	return ep.Conditions.Ready == nil || *ep.Conditions.Ready
}

// ResolvePort picks the container port a port-forward to namespace/
// serviceName should target, reading it straight off the Service's
// EndpointSlices (the resolved target port, even when the Service itself
// references it by name) rather than the Service's own spec.ports, since a
// port-forward goes straight to the pod and bypasses the Service entirely.
// Prefers a port named "https" or numbered 443 (what every route this
// dashboard tracks terminates TLS on); falls back to the first port found.
func ResolvePort(ctx context.Context, kubeClient kubernetes.Interface, namespace, serviceName string) (int32, error) {
	slices, err := kubeClient.DiscoveryV1().EndpointSlices(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("kubernetes.io/service-name=%s", serviceName),
	})
	if err != nil {
		return 0, fmt.Errorf("listing endpointslices for %s/%s: %w", namespace, serviceName, err)
	}

	var fallback int32
	for _, slice := range slices.Items {
		for _, p := range slice.Ports {
			if p.Port == nil {
				continue
			}
			if fallback == 0 {
				fallback = *p.Port
			}
			if *p.Port == 443 || (p.Name != nil && *p.Name == "https") {
				return *p.Port, nil
			}
		}
	}
	if fallback != 0 {
		return fallback, nil
	}
	return 0, fmt.Errorf("no port found on EndpointSlices for Service %s/%s", namespace, serviceName)
}

// Dial opens a port-forward from a random local port to targetPort on one
// ready pod behind namespace/serviceName, and returns the local address to
// dial instead ("127.0.0.1:<port>") plus a stop func that tears the forward
// down. restConfig must be the *rest.Config the given kubeClient itself was
// built from — client-go's SPDY executor needs the raw REST config to
// negotiate the upgrade, not just a clientset built from it.
func Dial(ctx context.Context, restConfig *rest.Config, kubeClient kubernetes.Interface, namespace, serviceName string, targetPort int32) (localAddr string, stop func(), err error) {
	if restConfig == nil {
		return "", nil, fmt.Errorf("no REST config available for port-forward")
	}

	podName, err := resolveReadyPod(ctx, kubeClient, namespace, serviceName)
	if err != nil {
		return "", nil, err
	}

	req := kubeClient.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(namespace).
		Name(podName).
		SubResource("portforward")

	transport, upgrader, err := spdy.RoundTripperFor(restConfig)
	if err != nil {
		return "", nil, fmt.Errorf("building SPDY round tripper: %w", err)
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, req.URL())

	stopCh := make(chan struct{})
	readyCh := make(chan struct{})
	fw, err := portforward.New(dialer, []string{fmt.Sprintf("0:%d", targetPort)}, stopCh, readyCh, nil, nil)
	if err != nil {
		return "", nil, fmt.Errorf("setting up port-forward to pod %s/%s: %w", namespace, podName, err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- fw.ForwardPorts()
	}()

	select {
	case <-readyCh:
	case err := <-errCh:
		return "", nil, fmt.Errorf("port-forward to pod %s/%s failed to become ready: %w", namespace, podName, err)
	case <-ctx.Done():
		close(stopCh)
		return "", nil, ctx.Err()
	}

	ports, err := fw.GetPorts()
	if err != nil {
		close(stopCh)
		return "", nil, fmt.Errorf("reading forwarded port: %w", err)
	}
	if len(ports) == 0 {
		close(stopCh)
		return "", nil, fmt.Errorf("no port forwarded to pod %s/%s", namespace, podName)
	}

	localAddr = fmt.Sprintf("127.0.0.1:%d", ports[0].Local)
	stop = func() { close(stopCh) }
	return localAddr, stop, nil
}
