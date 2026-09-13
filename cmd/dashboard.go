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
	"github.com/thev1ndu/certhealthz/pkg/alert"
	"github.com/thev1ndu/certhealthz/pkg/certmanager"
	"github.com/thev1ndu/certhealthz/pkg/history"
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

// maxSettingsBodySize bounds the POST /api/settings request body — a
// handful of short fields.
const maxSettingsBodySize = 1 << 10 // 1 KiB

// maxCTBodySize bounds the POST /api/ct request body — a list of domains,
// generous headroom for a few dozen without letting a client force
// unbounded allocation.
const maxCTBodySize = 16 << 10 // 16 KiB

var (
	dashboardAddr         string
	dashboardWarnDays     int
	dashboardIncludeRaw   bool
	dashboardWebhookURL   string
	dashboardEndpoints    []string
	dashboardProbeTimeout time.Duration
	dashboardDBPath       string
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

// Settings is the dashboard's live-editable scan configuration. Seeded from
// --warn-days/--include-secrets/--webhook at startup, mutable at runtime via
// /api/settings so the UI can change them without a restart. Guarded by a
// mutex since HTTP handlers run concurrently with collect reading it on
// every scan.
type Settings struct {
	mu             sync.Mutex
	warnDays       int
	includeSecrets bool
	webhookURL     string
}

// NewSettings returns a Settings seeded with the given initial values, e.g.
// for tests that don't go through the CLI flag-backed defaults.
func NewSettings(warnDays int, includeSecrets bool, webhookURL string) *Settings {
	return &Settings{warnDays: warnDays, includeSecrets: includeSecrets, webhookURL: webhookURL}
}

func (s *Settings) Get() (warnDays int, includeSecrets bool, webhookURL string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.warnDays, s.includeSecrets, s.webhookURL
}

func (s *Settings) Set(warnDays int, includeSecrets bool, webhookURL string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.warnDays, s.includeSecrets, s.webhookURL = warnDays, includeSecrets, webhookURL
}

var (
	dashboardClusters          = NewClusterRegistry()
	dashboardEndpointsRegistry = NewEndpointRegistry()
	dashboardSettings          *Settings
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
	dashboardCmd.Flags().StringVar(&dashboardWebhookURL, "webhook", "", "webhook URL to POST flagged rows to; changeable at runtime from the UI")
	dashboardCmd.Flags().StringVar(&dashboardDBPath, "db", defaultHistoryDBPath, "path to the SQLite history database used by the UI's Record/Diff panel")
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

// apiSettings is the JSON shape of GET/POST /api/settings.
type apiSettings struct {
	WarnDays       int    `json:"warnDays"`
	IncludeSecrets bool   `json:"includeSecrets"`
	WebhookURL     string `json:"webhookURL"`
}

func handleSettings(settings *Settings, w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		warnDays, includeSecrets, webhookURL := settings.Get()
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(apiSettings{warnDays, includeSecrets, webhookURL}); err != nil {
			log.Printf("encoding /api/settings response: %v", err)
		}
	case http.MethodPost:
		var body apiSettings
		if err := json.NewDecoder(io.LimitReader(r.Body, maxSettingsBodySize)).Decode(&body); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if body.WarnDays <= 0 {
			http.Error(w, "warnDays must be positive", http.StatusBadRequest)
			return
		}
		settings.Set(body.WarnDays, body.IncludeSecrets, strings.TrimSpace(body.WebhookURL))
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(body); err != nil {
			log.Printf("encoding /api/settings response: %v", err)
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// apiAlertResult is the JSON shape of POST /api/alert.
type apiAlertResult struct {
	Flagged int  `json:"flagged"`
	Sent    bool `json:"sent"`
}

func handleAlert(collect func(context.Context) ([]output.Row, error), settings *Settings, w http.ResponseWriter, r *http.Request) {
	_, _, webhookURL := settings.Get()
	if webhookURL == "" {
		http.Error(w, "no webhook URL configured — set one in settings first", http.StatusBadRequest)
		return
	}

	rows, err := collect(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	flagged := alert.Flagged(rows)
	sent := false
	if len(flagged) > 0 {
		if err := alert.Send(webhookURL, rows); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		sent = true
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(apiAlertResult{Flagged: len(flagged), Sent: sent}); err != nil {
		log.Printf("encoding /api/alert response: %v", err)
	}
}

// apiChange is the JSON shape of one entry in GET /api/history/diff.
type apiChange struct {
	Kind      string `json:"kind"`
	Source    string `json:"source"`
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	FromDays  *int   `json:"fromDays"`
	ToDays    *int   `json:"toDays"`
	FromState string `json:"fromState"`
	ToState   string `json:"toState"`
}

// apiDiff is the JSON shape of GET /api/history/diff.
type apiDiff struct {
	LatestAt   *string     `json:"latestAt"`
	PreviousAt *string     `json:"previousAt"`
	Changes    []apiChange `json:"changes"`
	Message    string      `json:"message,omitempty"`
}

func handleHistoryRecord(collect func(context.Context) ([]output.Row, error), store *history.Store, w http.ResponseWriter, r *http.Request) {
	rows, err := collect(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	runID, err := store.RecordRun(rows)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"runId": runID}); err != nil {
		log.Printf("encoding /api/history/record response: %v", err)
	}
}

func handleHistoryDiff(store *history.Store, w http.ResponseWriter, _ *http.Request) {
	latest, previous, latestAt, previousAt, err := store.LastTwoRuns()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := apiDiff{}
	switch {
	case latest == nil:
		resp.Message = "no recorded runs yet — click Record to take a snapshot"
	case previous == nil:
		at := latestAt.UTC().Format(time.RFC3339)
		resp.LatestAt = &at
		resp.Message = "only one recorded run so far — nothing to diff against yet"
	default:
		latestStr := latestAt.UTC().Format(time.RFC3339)
		previousStr := previousAt.UTC().Format(time.RFC3339)
		resp.LatestAt = &latestStr
		resp.PreviousAt = &previousStr
		for _, c := range history.Diff(latest, previous) {
			resp.Changes = append(resp.Changes, apiChange{
				Kind:      c.Kind,
				Source:    c.Identity.Source,
				Cluster:   c.Identity.Cluster,
				Namespace: c.Identity.Namespace,
				Name:      c.Identity.Name,
				FromDays:  c.FromDays,
				ToDays:    c.ToDays,
				FromState: c.FromState,
				ToState:   c.ToState,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("encoding /api/history/diff response: %v", err)
	}
}

// dashboardKnownDNSNames flattens the DNS names on every kubernetes.io/tls
// Secret across every configured dashboard cluster (startup --kubeconfig
// and uploads alike), so a CT check can tell known from unknown against
// what's actually deployed here — the cmd/ct.go CLI variant only walks the
// raw --kubeconfig path list, which uploaded clusters aren't part of.
func dashboardKnownDNSNames(ctx context.Context, clusters *ClusterRegistry) []string {
	var names []string
	for _, e := range clusters.All() {
		label := e.Label
		if label == "" {
			label = "default"
		}
		cc, err := buildClusterClients(label, e.Kubeconfig, e.Label, true)
		if err != nil {
			log.Printf("building client for %s: %v", label, err)
			continue
		}
		secrets, err := certmanager.ScanSecrets(ctx, label, cc.Typed)
		if err != nil {
			log.Printf("scanning secrets on %s: %v", label, err)
			continue
		}
		for _, s := range secrets {
			names = append(names, s.DNSNames...)
		}
	}
	return names
}

func handleCT(clusters *ClusterRegistry, settings *Settings, w http.ResponseWriter, r *http.Request) {
	var body struct {
		Domains []string `json:"domains"`
		Since   string   `json:"since"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, maxCTBodySize)).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if len(body.Domains) == 0 {
		http.Error(w, "at least one domain is required", http.StatusBadRequest)
		return
	}

	since := 24 * time.Hour
	if body.Since != "" {
		d, err := time.ParseDuration(body.Since)
		if err != nil {
			http.Error(w, "invalid since duration", http.StatusBadRequest)
			return
		}
		since = d
	}

	warnDays, _, _ := settings.Get()
	known := dashboardKnownDNSNames(r.Context(), clusters)
	rows := ctRows(r.Context(), body.Domains, since, warnDays, known)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(toAPIRows(rows)); err != nil {
		log.Printf("encoding /api/ct response: %v", err)
	}
}

// DashboardDeps bundles NewDashboardMux's dependencies so its constructor
// doesn't grow an ever-longer positional parameter list as the dashboard's
// API surface grows.
type DashboardDeps struct {
	Collect   func(context.Context) ([]output.Row, error)
	Clusters  *ClusterRegistry
	Endpoints *EndpointRegistry
	History   *history.Store
	Settings  *Settings
}

// NewDashboardMux builds the dashboard's HTTP routing: the embedded UI plus
// the /api/certs, /api/clusters, /api/endpoints, /api/settings, /api/alert,
// /api/history/record, /api/history/diff, and /api/ct endpoints.
func NewDashboardMux(uiHandler http.Handler, deps DashboardDeps) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/certs", func(w http.ResponseWriter, r *http.Request) {
		rows, err := deps.Collect(r.Context())
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
			if err := json.NewEncoder(w).Encode(clusterLabels(deps.Clusters.All())); err != nil {
				log.Printf("encoding /api/clusters response: %v", err)
			}
		case http.MethodPost:
			handleAddCluster(deps.Clusters, w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/endpoints", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(deps.Endpoints.All()); err != nil {
				log.Printf("encoding /api/endpoints response: %v", err)
			}
		case http.MethodPost:
			handleAddEndpoint(deps.Endpoints, w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/settings", func(w http.ResponseWriter, r *http.Request) {
		handleSettings(deps.Settings, w, r)
	})
	mux.HandleFunc("/api/alert", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handleAlert(deps.Collect, deps.Settings, w, r)
	})
	mux.HandleFunc("/api/history/record", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handleHistoryRecord(deps.Collect, deps.History, w, r)
	})
	mux.HandleFunc("/api/history/diff", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handleHistoryDiff(deps.History, w, r)
	})
	mux.HandleFunc("/api/ct", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handleCT(deps.Clusters, deps.Settings, w, r)
	})
	mux.Handle("/", uiHandler)
	return mux
}

func runDashboard(_ *cobra.Command, _ []string) error {
	uiHandler, err := ui.Handler()
	if err != nil {
		return fmt.Errorf("loading embedded dashboard: %w", err)
	}

	dashboardSettings = NewSettings(dashboardWarnDays, dashboardIncludeRaw, dashboardWebhookURL)

	store, err := history.Open(dashboardDBPath)
	if err != nil {
		return fmt.Errorf("opening history database: %w", err)
	}
	defer store.Close()

	collect := func(ctx context.Context) ([]output.Row, error) {
		warnDays, includeSecrets, _ := dashboardSettings.Get()

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
			cc, err := buildClusterClients(label, e.Kubeconfig, e.Label, includeSecrets)
			if err != nil {
				return nil, err
			}
			clients = append(clients, cc)
		}

		rows, err := CollectRowsFromClients(ctx, clients, warnDays, includeSecrets)
		if err != nil {
			return nil, err
		}

		if endpoints := dashboardEndpointsRegistry.All(); len(endpoints) > 0 {
			rows = append(rows, probeRows(endpoints, dashboardProbeTimeout, warnDays)...)
			output.Sort(rows)
		}
		return rows, nil
	}
	mux := NewDashboardMux(uiHandler, DashboardDeps{
		Collect:   collect,
		Clusters:  dashboardClusters,
		Endpoints: dashboardEndpointsRegistry,
		History:   store,
		Settings:  dashboardSettings,
	})

	fmt.Printf("certhealthz ui listening on %s\n", dashboardAddr)
	server := &http.Server{
		Addr:              dashboardAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return server.ListenAndServe()
}
