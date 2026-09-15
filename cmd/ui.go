package cmd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
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
	uiAddr             string
	uiWarnDays         int
	uiIncludeRaw       bool
	uiWebhookURL       string
	uiEndpoints        []string
	uiProbeTimeout     time.Duration
	uiDBPath           string
	uiRecordInterval   time.Duration
	uiHistoryRetention time.Duration
	uiCTDomains        []string
	uiCTInterval       time.Duration
)

// ClusterEntry identifies one cluster the dashboard scans. Kubeconfig is
// nil for clusters configured via --kubeconfig at startup (Label is then
// the filesystem path); it holds the raw kubeconfig content for clusters
// added later through the "Add cluster" upload, which never touches disk.
// Removable is true only for the latter — a --kubeconfig entry can't be
// removed from the UI since it would just reappear on the next restart.
type ClusterEntry struct {
	Label      string
	Kubeconfig []byte
	Removable  bool
}

// ClusterRegistry tracks kubeconfig uploads the dashboard scans, beyond
// whatever was passed via --kubeconfig at startup. Guarded by a mutex since
// the HTTP handlers run concurrently.
type ClusterRegistry struct {
	mu    sync.Mutex
	extra []ClusterEntry
	store *history.Store
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
// label (the uploaded file's name) for deduplication. If a store is
// attached, the upload is persisted so it's still there on the next run.
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
	if c.store != nil {
		if err := c.store.SaveCluster(label, kubeconfig); err != nil {
			return err
		}
	}
	c.extra = append(c.extra, ClusterEntry{Label: label, Kubeconfig: kubeconfig, Removable: true})
	return nil
}

// Remove drops a cluster previously added through the "Add cluster" UI
// (or restored from a previous run), and deletes it from the store if one
// is attached. Returns an error if label isn't a removable entry — either
// it isn't configured at all, or it came from a --kubeconfig flag, which
// can only be removed by restarting without that flag.
func (c *ClusterRegistry) Remove(label string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, e := range c.extra {
		if e.Label != label {
			continue
		}
		if c.store != nil {
			if err := c.store.DeleteCluster(label); err != nil {
				return err
			}
		}
		c.extra = append(c.extra[:i:i], c.extra[i+1:]...)
		return nil
	}
	for _, p := range kubeconfigPaths {
		if p == label {
			return fmt.Errorf("cluster %s was configured via --kubeconfig at startup and can't be removed from the UI", label)
		}
	}
	return fmt.Errorf("cluster %s is not configured", label)
}

// SetStore attaches the history database used to persist and reload
// clusters added at runtime. Left unset (nil), the registry behaves exactly
// as before: in-memory only, e.g. for tests.
func (c *ClusterRegistry) SetStore(store *history.Store) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store = store
}

// LoadFromStore hydrates the registry with clusters persisted by a previous
// run. No-op if no store is attached.
func (c *ClusterRegistry) LoadFromStore() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.store == nil {
		return nil
	}
	stored, err := c.store.ListClusters()
	if err != nil {
		return err
	}
	for _, sc := range stored {
		c.extra = append(c.extra, ClusterEntry{Label: sc.Label, Kubeconfig: sc.Kubeconfig, Removable: true})
	}
	return nil
}

// EndpointRegistry tracks live TLS endpoints the dashboard probes, beyond
// whatever was passed via --probe at startup. Guarded by a mutex since the
// HTTP handlers run concurrently.
type EndpointRegistry struct {
	mu    sync.Mutex
	extra []string
	store *history.Store
}

// NewEndpointRegistry returns an empty registry, e.g. for tests that don't
// go through the --probe-backed global.
func NewEndpointRegistry() *EndpointRegistry {
	return &EndpointRegistry{}
}

func (e *EndpointRegistry) All() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	all := make([]string, 0, len(uiEndpoints)+len(e.extra))
	all = append(all, uiEndpoints...)
	return append(all, e.extra...)
}

// EndpointEntry pairs an endpoint with whether it can be removed from the
// UI — true for ones added through "Add endpoint" (or restored from a
// previous run), false for --probe flag endpoints.
type EndpointEntry struct {
	Endpoint  string
	Removable bool
}

// AllEntries is All(), annotated with removability for the management UI.
func (e *EndpointRegistry) AllEntries() []EndpointEntry {
	e.mu.Lock()
	defer e.mu.Unlock()
	entries := make([]EndpointEntry, 0, len(uiEndpoints)+len(e.extra))
	for _, ep := range uiEndpoints {
		entries = append(entries, EndpointEntry{Endpoint: ep})
	}
	for _, ep := range e.extra {
		entries = append(entries, EndpointEntry{Endpoint: ep, Removable: true})
	}
	return entries
}

// Add registers an endpoint (host or host:port) added through the "Add
// endpoint" UI, rejecting an exact duplicate of one already configured. If
// a store is attached, the endpoint is persisted so it's still there on the
// next run.
func (e *EndpointRegistry) Add(endpoint string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, ep := range uiEndpoints {
		if ep == endpoint {
			return fmt.Errorf("endpoint %s is already configured", endpoint)
		}
	}
	for _, ep := range e.extra {
		if ep == endpoint {
			return fmt.Errorf("endpoint %s is already configured", endpoint)
		}
	}
	if e.store != nil {
		if err := e.store.SaveEndpoint(endpoint); err != nil {
			return err
		}
	}
	e.extra = append(e.extra, endpoint)
	return nil
}

// Remove drops an endpoint previously added through the "Add endpoint" UI
// (or restored from a previous run), and deletes it from the store if one
// is attached. Returns an error if endpoint isn't a removable entry —
// either it isn't configured at all, or it came from a --probe flag, which
// can only be removed by restarting without that flag.
func (e *EndpointRegistry) Remove(endpoint string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i, ep := range e.extra {
		if ep != endpoint {
			continue
		}
		if e.store != nil {
			if err := e.store.DeleteEndpoint(endpoint); err != nil {
				return err
			}
		}
		e.extra = append(e.extra[:i:i], e.extra[i+1:]...)
		return nil
	}
	for _, ep := range uiEndpoints {
		if ep == endpoint {
			return fmt.Errorf("endpoint %s was configured via --probe at startup and can't be removed from the UI", endpoint)
		}
	}
	return fmt.Errorf("endpoint %s is not configured", endpoint)
}

// SetStore attaches the history database used to persist and reload
// endpoints added at runtime. Left unset (nil), the registry behaves
// exactly as before: in-memory only, e.g. for tests.
func (e *EndpointRegistry) SetStore(store *history.Store) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.store = store
}

// LoadFromStore hydrates the registry with endpoints persisted by a
// previous run. No-op if no store is attached.
func (e *EndpointRegistry) LoadFromStore() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.store == nil {
		return nil
	}
	stored, err := e.store.ListEndpoints()
	if err != nil {
		return err
	}
	e.extra = append(e.extra, stored...)
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
	store          *history.Store
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

// Set updates the live settings and, if a store is attached, persists them
// so they're still there on the next run.
func (s *Settings) Set(warnDays int, includeSecrets bool, webhookURL string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.store != nil {
		if err := s.store.SaveSettings(warnDays, includeSecrets, webhookURL); err != nil {
			return err
		}
	}
	s.warnDays, s.includeSecrets, s.webhookURL = warnDays, includeSecrets, webhookURL
	return nil
}

// SetStore attaches the history database used to persist settings changed
// at runtime. Left unset (nil), Settings behaves exactly as before:
// in-memory only, e.g. for tests.
func (s *Settings) SetStore(store *history.Store) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store = store
}

// LoadFromStore overrides the current in-memory values with whatever was
// persisted by a previous run, if anything was. No-op if no store is
// attached or nothing has been saved yet.
func (s *Settings) LoadFromStore() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.store == nil {
		return nil
	}
	warnDays, includeSecrets, webhookURL, ok, err := s.store.LoadSettings()
	if err != nil {
		return err
	}
	if ok {
		s.warnDays, s.includeSecrets, s.webhookURL = warnDays, includeSecrets, webhookURL
	}
	return nil
}

var (
	uiClusters          = NewClusterRegistry()
	uiEndpointsRegistry = NewEndpointRegistry()
	uiSettings          *Settings
)

var uiCmd = &cobra.Command{
	Use:     "ui",
	Aliases: []string{"dashboard"},
	Short:   "Serve the bundled web dashboard, backed by a live scan",
	RunE:    runUI,
}

func init() {
	uiCmd.Flags().StringVar(&uiAddr, "addr", ":8090", "address to serve the dashboard on")
	uiCmd.Flags().IntVar(&uiWarnDays, "warn-days", 14, "flag certificates expiring within this many days")
	uiCmd.Flags().BoolVar(&uiIncludeRaw, "include-secrets", true, "also scan raw kubernetes.io/tls Secrets, for Certificate drift detection and Ingress cross-referencing")
	uiCmd.Flags().StringSliceVar(&uiEndpoints, "probe", nil, "live TLS endpoint (host or host:port) to probe on every scan; repeat flag for multiple")
	uiCmd.Flags().DurationVar(&uiProbeTimeout, "probe-timeout", 5*time.Second, "per-endpoint dial timeout for --probe endpoints")
	uiCmd.Flags().StringVar(&uiWebhookURL, "webhook", "", "webhook URL to POST flagged rows to; changeable at runtime from the UI")
	uiCmd.Flags().StringVar(&uiDBPath, "db", defaultDBPath(), "path to the SQLite database used by the dashboard's audit log and persisted settings")
	uiCmd.Flags().DurationVar(&uiRecordInterval, "record-interval", 15*time.Minute, "how often the dashboard automatically records a scan to the audit log")
	uiCmd.Flags().DurationVar(&uiHistoryRetention, "history-retention", 720*time.Hour, "how long recorded runs are kept before being pruned; 0 disables pruning")
	uiCmd.Flags().StringSliceVar(&uiCTDomains, "ct-domains", nil, "domain to periodically check Certificate Transparency logs for; repeat flag for multiple. Unset disables CT monitoring")
	uiCmd.Flags().DurationVar(&uiCTInterval, "ct-interval", 6*time.Hour, "how often to check --ct-domains against CT logs")
	rootCmd.AddCommand(uiCmd)
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
	for _, r := range rows {
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
			ID:        encodeRowID(r),
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

// rowIdentity is the subset of output.Row that uniquely identifies a cert
// across scans, used to build a stable apiRow.ID and to resolve that ID back
// to a specific cert for the detail endpoint (handleCertDetail).
type rowIdentity struct {
	Source    string `json:"s"`
	Cluster   string `json:"c"`
	Namespace string `json:"n"`
	Name      string `json:"m"`
}

// encodeRowID builds a stable, opaque, URL-safe ID from a row's identity
// fields (source/cluster/namespace/name), replacing the old index-based ID
// which changed whenever the row set changed shape.
func encodeRowID(r output.Row) string {
	return rowIdentity{Source: r.Source, Cluster: r.Cluster, Namespace: r.Namespace, Name: r.Name}.encode()
}

// encode is the inverse of decodeRowID, also used directly by
// handleCertDetail (cmd/certdetail.go) to echo an id back in its response.
func (id rowIdentity) encode() string {
	b, err := json.Marshal(id)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// decodeRowID reverses encodeRowID, e.g. for handleCertDetail to resolve an
// incoming ?id= back to the cert it names.
func decodeRowID(id string) (rowIdentity, error) {
	b, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return rowIdentity{}, fmt.Errorf("invalid id: %w", err)
	}
	var ri rowIdentity
	if err := json.Unmarshal(b, &ri); err != nil {
		return rowIdentity{}, fmt.Errorf("invalid id: %w", err)
	}
	return ri, nil
}

// apiCluster is the JSON shape of one entry in GET /api/clusters.
type apiCluster struct {
	Label     string `json:"label"`
	Removable bool   `json:"removable"`
}

func apiClusters(entries []ClusterEntry) []apiCluster {
	out := make([]apiCluster, len(entries))
	for i, e := range entries {
		label := e.Label
		if label == "" {
			label = "default"
		}
		out[i] = apiCluster{Label: label, Removable: e.Removable}
	}
	return out
}

// apiEndpoint is the JSON shape of one entry in GET /api/endpoints.
type apiEndpoint struct {
	Endpoint  string `json:"endpoint"`
	Removable bool   `json:"removable"`
}

func apiEndpoints(entries []EndpointEntry) []apiEndpoint {
	out := make([]apiEndpoint, len(entries))
	for i, e := range entries {
		out[i] = apiEndpoint(e)
	}
	return out
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
	if err := json.NewEncoder(w).Encode(apiClusters(clusters.All())); err != nil {
		log.Printf("encoding /api/clusters response: %v", err)
	}
}

// handleRemoveCluster drops a cluster previously added through the "Add
// cluster" UI. Clusters configured via --kubeconfig at startup can't be
// removed this way — the registry rejects those with a 400.
func handleRemoveCluster(clusters *ClusterRegistry, w http.ResponseWriter, r *http.Request) {
	label := r.PathValue("label")
	if err := clusters.Remove(label); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(apiClusters(clusters.All())); err != nil {
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
	if err := json.NewEncoder(w).Encode(apiEndpoints(endpoints.AllEntries())); err != nil {
		log.Printf("encoding /api/endpoints response: %v", err)
	}
}

// handleRemoveEndpoint drops an endpoint previously added through the "Add
// endpoint" UI. Endpoints configured via --probe at startup can't be
// removed this way — the registry rejects those with a 400.
func handleRemoveEndpoint(endpoints *EndpointRegistry, w http.ResponseWriter, r *http.Request) {
	endpoint := r.PathValue("endpoint")
	if err := endpoints.Remove(endpoint); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(apiEndpoints(endpoints.AllEntries())); err != nil {
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
		if err := settings.Set(body.WarnDays, body.IncludeSecrets, strings.TrimSpace(body.WebhookURL)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
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
		if len(resp.Changes) == 0 {
			resp.Message = "no changes since the last recorded snapshot"
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("encoding /api/history/diff response: %v", err)
	}
}

// apiEvent is the JSON shape of one entry in GET /api/history/events — an
// apiChange plus the timestamp it was detected at, for the dashboard's
// audit-trail view.
type apiEvent struct {
	At        string `json:"at"`
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

// apiEventsResp is the JSON shape of GET /api/history/events. NextBefore is
// the cursor to pass as ?before= to fetch the next page, omitted once
// there's no more history to walk.
type apiEventsResp struct {
	Events     []apiEvent `json:"events"`
	NextBefore int64      `json:"nextBefore,omitempty"`
}

const (
	defaultEventsLimit = 50
	maxEventsLimit     = 200
)

// handleHistoryEvents serves the dashboard's audit-trail feed: every change
// ever detected between automatically recorded runs, newest first, paged
// via ?limit= and ?before= (the previous response's nextBefore).
func handleHistoryEvents(store *history.Store, w http.ResponseWriter, r *http.Request) {
	limit := defaultEventsLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
		limit = min(n, maxEventsLimit)
	}
	var before int64
	if v := r.URL.Query().Get("before"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			http.Error(w, "invalid before", http.StatusBadRequest)
			return
		}
		before = n
	}

	events, nextBefore, err := store.Events(limit, before)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := apiEventsResp{Events: make([]apiEvent, 0, len(events)), NextBefore: nextBefore}
	for _, e := range events {
		resp.Events = append(resp.Events, apiEvent{
			At:        e.At.UTC().Format(time.RFC3339),
			Kind:      e.Kind,
			Source:    e.Identity.Source,
			Cluster:   e.Identity.Cluster,
			Namespace: e.Identity.Namespace,
			Name:      e.Identity.Name,
			FromDays:  e.FromDays,
			ToDays:    e.ToDays,
			FromState: e.FromState,
			ToState:   e.ToState,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("encoding /api/history/events response: %v", err)
	}
}

// uiKnownDNSNames flattens the DNS names on every kubernetes.io/tls
// Secret across every configured dashboard cluster (startup --kubeconfig
// and uploads alike), so a CT check can tell known from unknown against
// what's actually deployed here — the cmd/ct.go CLI variant only walks the
// raw --kubeconfig path list, which uploaded clusters aren't part of.
func uiKnownDNSNames(ctx context.Context, clusters *ClusterRegistry) []string {
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
	known := uiKnownDNSNames(r.Context(), clusters)
	rows := ctRows(r.Context(), body.Domains, since, warnDays, known)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(toAPIRows(rows)); err != nil {
		log.Printf("encoding /api/ct response: %v", err)
	}
}

// UIDeps bundles NewUIMux's dependencies so its constructor
// doesn't grow an ever-longer positional parameter list as the dashboard's
// API surface grows.
type UIDeps struct {
	Collect   func(context.Context) ([]output.Row, error)
	Clusters  *ClusterRegistry
	Endpoints *EndpointRegistry
	History   *history.Store
	Settings  *Settings

	// ClusterClientsFor resolves a cluster's real (possibly
	// kubeconfig-renamed) label to the clients needed to fetch one
	// certificate on demand, for handleCertDetail (cmd/certdetail.go). Like
	// Collect, this is a seam: production wires it to
	// findClusterClients(uiClusters.All(), ...) but e2e tests can point it at
	// fake clientsets directly, the same way they override Collect.
	ClusterClientsFor func(cluster string) (ClusterClients, error)
}

// NewUIMux builds the dashboard's HTTP routing: the embedded UI plus
// the /api/certs, /api/certs/detail, /api/certs/reissue, /api/clusters (+ DELETE
// /api/clusters/{label}), /api/endpoints (+ DELETE /api/endpoints/{endpoint}),
// /api/settings, /api/alert, /api/history/record, /api/history/diff,
// /api/history/events, and
// /api/ct endpoints.
func NewUIMux(uiHandler http.Handler, deps UIDeps) *http.ServeMux {
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
	mux.HandleFunc("/api/certs/detail", func(w http.ResponseWriter, r *http.Request) {
		handleCertDetail(deps, w, r)
	})
	mux.HandleFunc("/api/certs/reissue", func(w http.ResponseWriter, r *http.Request) {
		handleReissueCert(deps, w, r)
	})
	mux.HandleFunc("/api/clusters", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(apiClusters(deps.Clusters.All())); err != nil {
				log.Printf("encoding /api/clusters response: %v", err)
			}
		case http.MethodPost:
			handleAddCluster(deps.Clusters, w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("DELETE /api/clusters/{label}", func(w http.ResponseWriter, r *http.Request) {
		handleRemoveCluster(deps.Clusters, w, r)
	})
	mux.HandleFunc("/api/endpoints", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(apiEndpoints(deps.Endpoints.AllEntries())); err != nil {
				log.Printf("encoding /api/endpoints response: %v", err)
			}
		case http.MethodPost:
			handleAddEndpoint(deps.Endpoints, w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("DELETE /api/endpoints/{endpoint}", func(w http.ResponseWriter, r *http.Request) {
		handleRemoveEndpoint(deps.Endpoints, w, r)
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
	mux.HandleFunc("/api/history/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handleHistoryEvents(deps.History, w, r)
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

func runUI(_ *cobra.Command, _ []string) error {
	uiHandler, err := ui.Handler()
	if err != nil {
		return fmt.Errorf("loading embedded dashboard: %w", err)
	}

	store, err := history.Open(uiDBPath)
	if err != nil {
		return fmt.Errorf("opening history database: %w", err)
	}
	defer store.Close()

	uiSettings = NewSettings(uiWarnDays, uiIncludeRaw, uiWebhookURL)
	uiSettings.SetStore(store)
	if err := uiSettings.LoadFromStore(); err != nil {
		log.Printf("loading persisted settings: %v", err)
	}

	uiClusters.SetStore(store)
	if err := uiClusters.LoadFromStore(); err != nil {
		log.Printf("loading persisted clusters: %v", err)
	}

	uiEndpointsRegistry.SetStore(store)
	if err := uiEndpointsRegistry.LoadFromStore(); err != nil {
		log.Printf("loading persisted endpoints: %v", err)
	}

	collect := func(ctx context.Context) ([]output.Row, error) {
		warnDays, includeSecrets, _ := uiSettings.Get()

		clients, err := buildAllClusterClients(uiClusters.All(), includeSecrets)
		if err != nil {
			return nil, err
		}

		rows, err := CollectRowsFromClients(ctx, clients, warnDays, includeSecrets)
		if err != nil {
			return nil, err
		}

		if endpoints := uiEndpointsRegistry.All(); len(endpoints) > 0 {
			rows = append(rows, probeRows(endpoints, uiProbeTimeout, warnDays)...)
			output.Sort(rows)
		}
		return rows, nil
	}
	mux := NewUIMux(uiHandler, UIDeps{
		Collect:   collect,
		Clusters:  uiClusters,
		Endpoints: uiEndpointsRegistry,
		History:   store,
		Settings:  uiSettings,
		ClusterClientsFor: func(cluster string) (ClusterClients, error) {
			return findClusterClients(uiClusters.All(), cluster, true)
		},
	})

	store.SetRetention(uiHistoryRetention)
	go autoRecordHistory(collect, store, uiRecordInterval)

	if len(uiCTDomains) > 0 {
		go autoCheckCT(uiClusters, uiSettings, uiCTDomains, uiCTInterval)
	}

	fmt.Printf("certhealthz ui listening on %s\n", uiAddr)
	server := &http.Server{
		Addr:              uiAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return server.ListenAndServe()
}

// autoRecordHistory records a run immediately (so the audit log isn't empty
// on first load) and again on every interval tick, for as long as the
// process runs. This is the dashboard's only recording path — there is no
// manual "record snapshot" trigger in the UI; a transient collect error is
// logged and skipped rather than stopping the loop, since the next tick
// will simply try again.
func autoRecordHistory(collect func(context.Context) ([]output.Row, error), store *history.Store, interval time.Duration) {
	record := func() {
		rows, err := collect(context.Background())
		if err != nil {
			log.Printf("auto-recording scan: %v", err)
			return
		}
		if _, err := store.RecordRun(rows); err != nil {
			log.Printf("auto-recording scan: %v", err)
		}
	}

	record()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		record()
	}
}

// autoCheckCT periodically checks domains against Certificate Transparency
// logs, mirroring autoRecordHistory's ticker shape (run once immediately,
// then on every interval tick). Unlike history recording, CT findings
// aren't persisted to the audit log — that store's shape is a scan-snapshot
// diff, not a one-off discovery event — so a flagged (possible
// shadow/rogue issuance) result is instead sent through the same
// alert.Send webhook mechanism already used for flagged scan rows. since is
// widened to 2*interval so nothing is missed right at a tick boundary.
func autoCheckCT(clusters *ClusterRegistry, settings *Settings, domains []string, interval time.Duration) {
	check := func() {
		ctx := context.Background()
		warnDays, _, webhookURL := settings.Get()
		known := uiKnownDNSNames(ctx, clusters)
		rows := ctRows(ctx, domains, 2*interval, warnDays, known)

		flagged := alert.Flagged(rows)
		if len(flagged) == 0 || webhookURL == "" {
			return
		}
		if err := alert.Send(webhookURL, rows); err != nil {
			log.Printf("sending CT alert: %v", err)
		}
	}

	check()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		check()
	}
}
