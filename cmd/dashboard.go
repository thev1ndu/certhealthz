package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
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

var (
	dashboardAddr       string
	dashboardWarnDays   int
	dashboardIncludeRaw bool
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

var dashboardClusters = NewClusterRegistry()

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Serve the bundled web dashboard, backed by a live scan",
	RunE:  runDashboard,
}

func init() {
	dashboardCmd.Flags().StringVar(&dashboardAddr, "addr", ":8090", "address to serve the dashboard on")
	dashboardCmd.Flags().IntVar(&dashboardWarnDays, "warn-days", 14, "flag certificates expiring within this many days")
	dashboardCmd.Flags().BoolVar(&dashboardIncludeRaw, "include-secrets", true, "also scan raw kubernetes.io/tls Secrets for drift against Certificate status")
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

// NewDashboardMux builds the dashboard's HTTP routing: the embedded UI plus
// the /api/certs and /api/clusters endpoints. collect is injected so tests
// can back /api/certs with fake clientsets instead of a real cluster.
func NewDashboardMux(uiHandler http.Handler, collect func(context.Context) ([]output.Row, error), clusters *ClusterRegistry) *http.ServeMux {
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

		return CollectRowsFromClients(ctx, clients, dashboardWarnDays, dashboardIncludeRaw)
	}
	mux := NewDashboardMux(uiHandler, collect, dashboardClusters)

	fmt.Printf("certhealthz dashboard listening on %s\n", dashboardAddr)
	server := &http.Server{
		Addr:              dashboardAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return server.ListenAndServe()
}
