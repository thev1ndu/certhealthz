package cloudcert

import (
	"context"
	"testing"
	"time"

	"cloud.google.com/go/certificatemanager/apiv1/certificatemanagerpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// fakeGCPClient is a hand-rolled fake gcpCertClient — no live GCP calls,
// no network.
type fakeGCPClient struct {
	certs map[string][]*certificatemanagerpb.Certificate
	err   error
}

func (f *fakeGCPClient) ListCertificates(_ context.Context, parent string) ([]*certificatemanagerpb.Certificate, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.certs[parent], nil
}

func TestScanGCP(t *testing.T) {
	notAfter := time.Now().Add(60 * 24 * time.Hour)
	fake := &fakeGCPClient{
		certs: map[string][]*certificatemanagerpb.Certificate{
			"projects/my-project/locations/global": {
				{
					Name:        "projects/my-project/locations/global/certificates/a",
					ExpireTime:  timestamppb.New(notAfter),
					SanDnsnames: []string{"a.example.com"},
				},
			},
		},
	}

	rows, err := scanGCP(context.Background(), fake, "my-project", 14)
	if err != nil {
		t.Fatalf("scanGCP: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	row := rows[0]
	if row.Source != "gcp-certmanager" {
		t.Errorf("expected Source gcp-certmanager, got %q", row.Source)
	}
	if row.Status != "ok" {
		t.Errorf("expected Status ok, got %q", row.Status)
	}
	if row.Detail == "" {
		t.Errorf("expected SAN detail to be populated")
	}
}

func TestScanGCPEmpty(t *testing.T) {
	fake := &fakeGCPClient{certs: map[string][]*certificatemanagerpb.Certificate{}}
	rows, err := scanGCP(context.Background(), fake, "empty-project", 14)
	if err != nil {
		t.Fatalf("scanGCP: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(rows))
	}
}
