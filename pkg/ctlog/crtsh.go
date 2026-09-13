// Package ctlog queries public Certificate Transparency logs for certs
// issued for a domain, so callers can catch issuance no configured cluster
// or cloud account told them about — a compromised registrar, a forgotten
// CA account, or an attacker requesting a cert for your name.
package ctlog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Entry is one certificate logged in Certificate Transparency, as reported
// by crt.sh's JSON search API.
type Entry struct {
	ID        int64
	Domain    string // the domain queried
	NameValue string // the logged cert's SAN(s), newline-separated
	Issuer    string
	NotBefore time.Time
	NotAfter  time.Time
}

type crtshEntry struct {
	ID         int64  `json:"id"`
	IssuerName string `json:"issuer_name"`
	NameValue  string `json:"name_value"`
	NotBefore  string `json:"not_before"`
	NotAfter   string `json:"not_after"`
}

const crtshTimeLayout = "2006-01-02T15:04:05"

// Query fetches Certificate Transparency log entries for domain from
// crt.sh — no API key required, but best-effort: crt.sh throttles hard
// under load, so callers should treat an error as transient, not fatal.
// Results are deduplicated by certificate ID (crt.sh returns one row per
// CT log operator that carries a given cert, so the same cert often
// appears more than once).
func Query(ctx context.Context, domain string) ([]Entry, error) {
	reqURL := "https://crt.sh/?q=" + url.QueryEscape(domain) + "&output=json"
	return queryURL(ctx, reqURL, domain)
}

func queryURL(ctx context.Context, reqURL, domain string) ([]Entry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("querying crt.sh for %s: %w", domain, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("crt.sh returned %s for %s", resp.Status, domain)
	}

	var raw []crtshEntry
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decoding crt.sh response for %s: %w", domain, err)
	}

	seen := make(map[int64]bool, len(raw))
	entries := make([]Entry, 0, len(raw))
	for _, r := range raw {
		if seen[r.ID] {
			continue
		}
		seen[r.ID] = true

		notBefore, _ := time.Parse(crtshTimeLayout, r.NotBefore)
		notAfter, _ := time.Parse(crtshTimeLayout, r.NotAfter)
		entries = append(entries, Entry{
			ID:        r.ID,
			Domain:    domain,
			NameValue: r.NameValue,
			Issuer:    r.IssuerName,
			NotBefore: notBefore,
			NotAfter:  notAfter,
		})
	}
	return entries, nil
}
