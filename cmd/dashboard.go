package cmd

import (
	"context"
	"encoding/json"
	"fmt"
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

var (
	dashboardAddr       string
	dashboardWarnDays   int
	dashboardIncludeRaw bool
)

// ClusterRegistry tracks kubeconfig paths the dashboard scans, beyond
// whatever was passed via --kubeconfig at startup. Guarded by a mutex since
// the HTTP handlers run concurrently.
type ClusterRegistry struct {
	mu    sync.Mutex
	extra []string
}

// NewClusterRegistry returns an empty registry, e.g. for tests that don't
// go through the --kubeconfig-backed global.
func NewClusterRegistry() *ClusterRegistry {
	return &ClusterRegistry{}
}

func (c *ClusterRegistry) All() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	all := append([]string(nil), kubeconfigPaths...)
	return append(all, c.extra...)
}

func (c *ClusterRegistry) Add(path string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, p := range kubeconfigPaths {
		if p == path {
			return fmt.Errorf("cluster %s is already configured", path)
		}
	}
	for _, p := range c.extra {
		if p == path {
			return fmt.Errorf("cluster %s is already configured", path)
		}
	}
	c.extra = append(c.extra, path)
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

func clusterLabels(paths []string) []string {
	labels := make([]string, len(paths))
	for i, p := range paths {
		if p == "" {
			labels[i] = "default"
		} else {
			labels[i] = p
		}
	}
	return labels
}

type addClusterRequest struct {
	Path string `json:"path"`
}

func handleAddCluster(clusters *ClusterRegistry, w http.ResponseWriter, r *http.Request) {
	var req addClusterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	path := strings.TrimSpace(req.Path)
	if path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}

	// Fail fast on a bad kubeconfig path/context rather than silently adding
	// a cluster that will only ever produce warnings on every scan.
	if _, err := certmanager.NewDynamicClient(path); err != nil {
		http.Error(w, fmt.Sprintf("could not connect using %s: %v", path, err), http.StatusBadRequest)
		return
	}

	if err := clusters.Add(path); err != nil {
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
		return collectRows(ctx, dashboardClusters.All(), dashboardWarnDays, dashboardIncludeRaw)
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
