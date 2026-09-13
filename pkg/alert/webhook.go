package alert

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/thev1ndu/certhealthz/pkg/output"
)

// Payload is the generic JSON body posted to a webhook (Slack incoming
// webhooks, ServiceNow inbound REST, or any custom receiver).
type Payload struct {
	Text      string       `json:"text"`
	Generated time.Time    `json:"generated_at"`
	Rows      []output.Row `json:"certificates"`
}

// Send posts every row with status "expiring" or "expired" to the given
// webhook URL as a single JSON payload. Returns early with nil if there's
// nothing to alert on.
func Send(url string, rows []output.Row) error {
	var flagged []output.Row
	for _, r := range rows {
		if r.Status == "expiring" || r.Status == "expired" || r.Status == "error" || r.Status == "drift" {
			flagged = append(flagged, r)
		}
	}
	if len(flagged) == 0 {
		return nil
	}

	body := Payload{
		Text:      fmt.Sprintf("CertCTL: %d certificate(s) need attention", len(flagged)),
		Generated: time.Now().UTC(),
		Rows:      flagged,
	}

	buf, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal alert payload: %w", err)
	}

	//nolint:gosec // url is the user-supplied --webhook destination by design, not attacker input
	resp, err := http.Post(url, "application/json", bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("posting webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}
