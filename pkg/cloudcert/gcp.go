package cloudcert

import (
	"context"
	"fmt"

	certificatemanager "cloud.google.com/go/certificatemanager/apiv1"
	"cloud.google.com/go/certificatemanager/apiv1/certificatemanagerpb"
	"google.golang.org/api/iterator"

	"github.com/thev1ndu/certhealthz/pkg/output"
)

// gcpCertClient is the narrow surface ScanGCP needs out of GCP's
// Certificate Manager API. It's deliberately not the raw
// *certificatemanager.Client method signature (which returns a
// *CertificateIterator, a concrete type awkward to fake) — gcpClientAdapter
// below bridges the real client to this interface by draining that
// iterator, so tests can substitute a fake implementing this interface
// directly, with no live GCP calls.
type gcpCertClient interface {
	ListCertificates(ctx context.Context, parent string) ([]*certificatemanagerpb.Certificate, error)
}

// gcpClientAdapter adapts a real *certificatemanager.Client to
// gcpCertClient by draining its ListCertificates iterator into a slice.
type gcpClientAdapter struct {
	client *certificatemanager.Client
}

func (a gcpClientAdapter) ListCertificates(ctx context.Context, parent string) ([]*certificatemanagerpb.Certificate, error) {
	it := a.client.ListCertificates(ctx, &certificatemanagerpb.ListCertificatesRequest{Parent: parent})
	var certs []*certificatemanagerpb.Certificate
	for {
		cert, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		certs = append(certs, cert)
	}
	return certs, nil
}

// ScanGCP lists every certificate in GCP Certificate Manager's "global"
// location for project and returns one Row per certificate, Source
// "gcp-certmanager". Credentials come from Application Default Credentials
// — nothing is read from or written to disk by this package, and no
// credential is ever hardcoded.
func ScanGCP(ctx context.Context, project string, warnDays int) ([]output.Row, error) {
	client, err := certificatemanager.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("building GCP Certificate Manager client: %w", err)
	}
	defer client.Close()

	return scanGCP(ctx, gcpClientAdapter{client: client}, project, warnDays)
}

// scanGCP is ScanGCP's client-injected core, kept separate so unit tests
// can drive it against a fake gcpCertClient instead of a live GCP project.
func scanGCP(ctx context.Context, client gcpCertClient, project string, warnDays int) ([]output.Row, error) {
	parent := fmt.Sprintf("projects/%s/locations/global", project)
	certs, err := client.ListCertificates(ctx, parent)
	if err != nil {
		return nil, fmt.Errorf("listing GCP certificates in %s: %w", parent, err)
	}

	rows := make([]output.Row, 0, len(certs))
	for _, cert := range certs {
		row := output.Row{
			Source:  "gcp-certmanager",
			Cluster: project,
			Name:    cert.GetName(),
		}
		if t := cert.GetExpireTime(); t != nil {
			row.NotAfter = t.AsTime()
		}
		row = output.Classify(row, warnDays)
		if len(cert.GetSanDnsnames()) > 0 {
			row.Detail = fmt.Sprintf("SANs: %v", cert.GetSanDnsnames())
		}
		rows = append(rows, row)
	}
	return rows, nil
}
