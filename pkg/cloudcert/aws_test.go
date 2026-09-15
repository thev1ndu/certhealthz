package cloudcert

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/acm"
	"github.com/aws/aws-sdk-go-v2/service/acm/types"
)

// fakeACM is a hand-rolled fake acmAPI — no live AWS calls, no network.
type fakeACM struct {
	pages        [][]types.CertificateSummary
	descriptions map[string]*types.CertificateDetail
	describeErr  map[string]error
}

func (f *fakeACM) ListCertificates(_ context.Context, params *acm.ListCertificatesInput, _ ...func(*acm.Options)) (*acm.ListCertificatesOutput, error) {
	pageIdx := 0
	if params.NextToken != nil {
		var err error
		pageIdx, err = parsePageToken(*params.NextToken)
		if err != nil {
			return nil, err
		}
	}
	if pageIdx >= len(f.pages) {
		return &acm.ListCertificatesOutput{}, nil
	}
	out := &acm.ListCertificatesOutput{CertificateSummaryList: f.pages[pageIdx]}
	if pageIdx+1 < len(f.pages) {
		token := pageToken(pageIdx + 1)
		out.NextToken = &token
	}
	return out, nil
}

func (f *fakeACM) DescribeCertificate(_ context.Context, params *acm.DescribeCertificateInput, _ ...func(*acm.Options)) (*acm.DescribeCertificateOutput, error) {
	arn := aws.ToString(params.CertificateArn)
	if err, ok := f.describeErr[arn]; ok {
		return nil, err
	}
	detail, ok := f.descriptions[arn]
	if !ok {
		return nil, errors.New("not found")
	}
	return &acm.DescribeCertificateOutput{Certificate: detail}, nil
}

func pageToken(i int) string { return string(rune('0' + i)) }
func parsePageToken(s string) (int, error) {
	if len(s) != 1 {
		return 0, errors.New("bad token")
	}
	return int(s[0] - '0'), nil
}

func TestScanACM(t *testing.T) {
	notAfter := time.Now().Add(60 * 24 * time.Hour)
	fake := &fakeACM{
		pages: [][]types.CertificateSummary{
			{
				{CertificateArn: strPtr("arn:aws:acm:us-east-1:1:certificate/a"), DomainName: strPtr("a.example.com")},
			},
		},
		descriptions: map[string]*types.CertificateDetail{
			"arn:aws:acm:us-east-1:1:certificate/a": {
				DomainName: strPtr("a.example.com"),
				NotAfter:   &notAfter,
				Issuer:     strPtr("Amazon"),
			},
		},
	}

	rows, err := scanACM(context.Background(), fake, "us-east-1", 14)
	if err != nil {
		t.Fatalf("scanACM: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	row := rows[0]
	if row.Source != "aws-acm" {
		t.Errorf("expected Source aws-acm, got %q", row.Source)
	}
	if row.Name != "a.example.com" {
		t.Errorf("expected Name a.example.com, got %q", row.Name)
	}
	if row.Status != "ok" {
		t.Errorf("expected Status ok, got %q", row.Status)
	}
	if row.Detail != "issuer: Amazon" {
		t.Errorf("expected issuer detail, got %q", row.Detail)
	}
}

func TestScanACMPagination(t *testing.T) {
	notAfter := time.Now().Add(60 * 24 * time.Hour)
	fake := &fakeACM{
		pages: [][]types.CertificateSummary{
			{{CertificateArn: strPtr("arn:1"), DomainName: strPtr("one.example.com")}},
			{{CertificateArn: strPtr("arn:2"), DomainName: strPtr("two.example.com")}},
		},
		descriptions: map[string]*types.CertificateDetail{
			"arn:1": {DomainName: strPtr("one.example.com"), NotAfter: &notAfter},
			"arn:2": {DomainName: strPtr("two.example.com"), NotAfter: &notAfter},
		},
	}

	rows, err := scanACM(context.Background(), fake, "us-east-1", 14)
	if err != nil {
		t.Fatalf("scanACM: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows across pages, got %d", len(rows))
	}
}

func TestScanACMDescribeError(t *testing.T) {
	fake := &fakeACM{
		pages: [][]types.CertificateSummary{
			{{CertificateArn: strPtr("arn:broken"), DomainName: strPtr("broken.example.com")}},
		},
		describeErr: map[string]error{"arn:broken": errors.New("access denied")},
	}

	rows, err := scanACM(context.Background(), fake, "us-east-1", 14)
	if err != nil {
		t.Fatalf("scanACM: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 error row, got %d", len(rows))
	}
	if rows[0].Status != "error" {
		t.Errorf("expected Status error, got %q", rows[0].Status)
	}
}

func strPtr(s string) *string { return &s }
