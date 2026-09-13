package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/thev1ndu/certhealthz/pkg/certmanager"
	"github.com/thev1ndu/certhealthz/pkg/output"
	"github.com/thev1ndu/certhealthz/pkg/ui"
)

// maxKubeconfigUploadSize bounds how much of an uploaded kubeconfig the
// dashboard will read into memory. Real kubeconfigs are a few KB; this is
// generous headroom without letting a client force unbounded allocation.
const maxKubeconfigUploadSize = 10 << 20 // 10 MiB

// maxEndpointBodySize bounds the POST /api/endpoints request body — it's
// one short JSON field, never legitimately more than a few hundred bytes.
const maxEndpointBodySize = 1 << 10 // 1 KiB

var (
	dashboardAddr         string
	dashboardWarnDays     int
	dashboardIncludeRaw   bool
	dashboardEndpoints    []string
	dashboardProbeTimeout time.Duration
)

// ClusterEntry identifies one cluster the dashboard scans. Kubeconfig is
// nil for clusters configured via --kubeconfig at startup (Label is then
// the filesystem path); it holds the raw kubeconfig content for clusters
// added later through the "Add cluster" upload, which never touches disk.
type ClusterEntry struct {
	Label      string
	Kubeconfig []byte
}

// ClusterRegistry tracks kubeconfig uploads the dashboard scans, beyond
// whatever was passed via --kubeconfig at startup. Guarded by a mutex since
// the HTTP handlers run concurrently.
type ClusterRegistry struct {
	mu    sync.Mutex
	extra []ClusterEntry
}

// NewClusterRegistry returns an empty registry, e.g. for tests that don't
// go through the --kubeconfig-backed global.
func NewClusterRegistry() *ClusterRegistry {
	return &ClusterRegistry{}
}

func (c *ClusterRegistry) All() []ClusterEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	all := make([]ClusterEntry, 0, len(kubeconfigPaths)+len(c.extra))
	for _, p := range kubeconfigPaths {
		all = append(all, ClusterEntry{Label: p})
	}
	return append(all, c.extra...)
}

// AddUpload registers a cluster from uploaded kubeconfig content, keyed by
// label (the uploaded file's name) for deduplication.
func (c *ClusterRegistry) AddUpload(label string, kubeconfig []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, p := range kubeconfigPaths {
		if p == label {
			return fmt.Errorf("cluster %s is already configured", label)
		}
	}
	for _, e := range c.extra {
		if e.Label == label {
			return fmt.Errorf("cluster %s is already configured", label)
		}
	}
	c.extra = append(c.extra, ClusterEntry{Label: label, Kubeconfig: kubeconfig})
	return nil
}

// EndpointRegistry tracks live TLS endpoints the dashboard probes, beyond
// whatever was passed via --probe at startup. Guarded by a mutex since the
// HTTP handlers run concurrently.
type EndpointRegistry struct {
	mu    sync.Mutex
	extra []string
}

// NewEndpointRegistry returns an empty registry, e.g. for tests that don't
// go through the --probe-backed global.
func NewEndpointRegistry() *EndpointRegistry {
	return &EndpointRegistry{}
}

func (e *EndpointRegistry) All() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	all := make([]string, 0, len(dashboardEndpoints)+len(e.extra))
	all = append(all, dashboardEndpoints...)
	return append(all, e.extra...)
}

// Add registers an endpoint (host or host:port) added through the "Add
// endpoint" UI, rejecting an exact duplicate of one already configured.
func (e *EndpointRegistry) Add(endpoint string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, ep := range dashboardEndpoints {
		if ep == endpoint {
			return fmt.Errorf("endpoint %s is already configured", endpoint)
		}
	}
	for _, ep := range e.extra {
		if ep == endpoint {
			return fmt.Errorf("endpoint %s is already configured", endpoint)
		}
	}
	e.extra = append(e.extra, endpoint)
	return nil
}

var (
	dashboardClusters          = NewClusterRegistry()
	dashboardEndpointsRegistry = NewEndpointRegistry()
)

var dashboardCmd = &cobra.Command{
	Use:     "ui",
	Aliases: []string{"dashboard"},
	Short:   "Serve the bundled web dashboard, backed by a live scan",
	RunE:    runDashboard,
}

func init() {
	dashboardCmd.Flags().StringVar(&dashboardAddr, "addr", ":8090", "address to serve the dashboard on")
	dashboardCmd.Flags().IntVar(&dashboardWarnDays, "warn-days", 14, "flag certificates expiring within this many days")
	dashboardCmd.Flags().BoolVar(&dashboardIncludeRaw, "include-secrets", true, "also scan raw kubernetes.io/tls Secrets, for Certificate drift detection and Ingress cross-referencing")
	dashboardCmd.Flags().StringSliceVar(&dashboardEndpoints, "probe", nil, "live TLS endpoint (host or host:port) to probe on every scan; repeat flag for multiple")
	dashboardCmd.Flags().DurationVar(&dashboardProbeTimeout, "probe-timeout", 5*time.Second, "per-endpoint dial timeout for --probe endpoints")
	rootCmd.AddCommand(dashboardCmd)
}

// apiRow is the JSON shape the dashboard frontend expects, matching its
// mock data fixture (src/data/certs.js) so the same UI code works against
// either.
type apiRow struct {
	ID        string `json:"id"`
	Source    string `json:"source"`
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Days      *int   `json:"days"`
	Status    string `json:"status"`
	Detail    string `json:"detail"`
}

func toAPIRows(rows []output.Row) []apiRow {
	out := make([]apiRow, 0, len(rows))
	for i, r := range rows {
		cluster := r.Cluster
		if cluster == "" {
			cluster = "-"
		}
		namespace := r.Namespace
		if namespace == "" {
			namespace = "-"
		}
		var days *int
		if !r.NotAfter.IsZero() {
			d := r.DaysRemaining()
			days = &d
		}
		out = append(out, apiRow{
			ID:        fmt.Sprintf("%s-%s-%d", r.Source, r.Name, i),
			Source:    r.Source,
			Cluster:   cluster,
			Namespace: namespace,
			Name:      r.Name,
			Days:      days,
			Status:    r.Status,
			Detail:    r.Detail,
		})
	}
	return out
}

func clusterLabels(entries []ClusterEntry) []string {
	labels := make([]string, len(entries))
	for i, e := range entries {
		if e.Label == "" {
			labels[i] = "default"
		} else {
			labels[i] = e.Label
		}
	}
	return labels
}

func handleAddCluster(clusters *ClusterRegistry, w http.ResponseWriter, r *http.Request) {
	// Cap the request body itself, not just the in-memory portion
	// ParseMultipartForm buffers — otherwise a client can still force an
	// unbounded read (into the temp-file-backed overflow) before that limit
	// kicks in.
	r.Body = http.MaxBytesReader(w, r.Body, maxKubeconfigUploadSize)
	if err := r.ParseMultipartForm(maxKubeconfigUploadSize); err != nil { //nolint:gosec // body is already capped by MaxBytesReader above
		http.Error(w, "invalid upload", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("kubeconfig")
	if err != nil {
		http.Error(w, "kubeconfig file is required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxKubeconfigUploadSize))
	if err != nil {
		http.Error(w, "reading uploaded file", http.StatusBadRequest)
		return
	}
	if len(data) == 0 {
		http.Error(w, "uploaded kubeconfig is empty", http.StatusBadRequest)
		return
	}

	// Fail fast on a bad kubeconfig rather than silently adding a cluster
	// that will only ever produce warnings on every scan.
	if _, err := certmanager.NewDynamicClientFromBytes(data); err != nil {
		http.Error(w, fmt.Sprintf("could not connect using %s: %v", header.Filename, err), http.StatusBadRequest)
		return
	}

	if err := clusters.AddUpload(header.Filename, data); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(clusterLabels(clusters.All())); err != nil {
		log.Printf("encoding /api/clusters response: %v", err)
	}
}

// handleAddEndpoint registers a live TLS endpoint (host or host:port) for
// the dashboard to probe on every scan. Unlike a cluster upload, there's no
// connectivity check here — an endpoint that's briefly unreachable is still
// worth tracking; it'll just show up as an "error" row until it recovers,
// same as it would from `certhealthz probe`.
func handleAddEndpoint(endpoints *EndpointRegistry, w http.ResponseWriter, r *http.Request) {
	var body struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, maxEndpointBodySize)).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	endpoint := strings.TrimSpace(body.Endpoint)
	if endpoint == "" {
		http.Error(w, "endpoint is required", http.StatusBadRequest)
		return
	}
	if strings.ContainsAny(endpoint, " \t\r\n") {
		http.Error(w, "endpoint must not contain whitespace", http.StatusBadRequest)
		return
	}

	if err := endpoints.Add(endpoint); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(endpoints.All()); err != nil {
		log.Printf("encoding /api/endpoints response: %v", err)
	}
}

// NewDashboardMux builds the dashboard's HTTP routing: the embedded UI plus
// the /api/certs, /api/clusters, and /api/endpoints endpoints. collect is
// injected so tests can back /api/certs with fake clientsets instead of a
// real cluster.
func NewDashboardMux(uiHandler http.Handler, collect func(context.Context) ([]output.Row, error), clusters *ClusterRegistry, endpoints *EndpointRegistry) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/certs", func(w http.ResponseWriter, r *http.Request) {
		rows, err := collect(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(toAPIRows(rows)); err != nil {
			log.Printf("encoding /api/certs response: %v", err)
		}
	})
	mux.HandleFunc("/api/clusters", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(clusterLabels(clusters.All())); err != nil {
				log.Printf("encoding /api/clusters response: %v", err)
			}
		case http.MethodPost:
			handleAddCluster(clusters, w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/endpoints", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(endpoints.All()); err != nil {
				log.Printf("encoding /api/endpoints response: %v", err)
			}
		case http.MethodPost:
			handleAddEndpoint(endpoints, w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.Handle("/", uiHandler)
	return mux
}

func runDashboard(_ *cobra.Command, _ []string) error {
	uiHandler, err := ui.Handler()
	if err != nil {
		return fmt.Errorf("loading embedded dashboard: %w", err)
	}

	collect := func(ctx context.Context) ([]output.Row, error) {
		entries := dashboardClusters.All()
		if len(entries) == 0 {
			entries = []ClusterEntry{{}} // empty label => default loading rules
		}

		clients := make([]ClusterClients, 0, len(entries))
		for _, e := range entries {
			label := e.Label
			if label == "" {
				label = "default"
			}
			cc, err := buildClusterClients(label, e.Kubeconfig, e.Label, dashboardIncludeRaw)
			if err != nil {
				return nil, err
			}
			clients = append(clients, cc)
		}

		rows, err := CollectRowsFromClients(ctx, clients, dashboardWarnDays, dashboardIncludeRaw)
		if err != nil {
			return nil, err
		}

		if endpoints := dashboardEndpointsRegistry.All(); len(endpoints) > 0 {
			rows = append(rows, probeRows(endpoints, dashboardProbeTimeout, dashboardWarnDays)...)
			output.Sort(rows)
		}
		return rows, nil
	}
	mux := NewDashboardMux(uiHandler, collect, dashboardClusters, dashboardEndpointsRegistry)

	fmt.Printf("certhealthz ui listening on %s\n", dashboardAddr)
	server := &http.Server{
		Addr:              dashboardAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return server.ListenAndServe()
}
