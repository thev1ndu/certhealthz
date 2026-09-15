// Package cloudcert scans cloud provider certificate stores — AWS
// Certificate Manager, GCP Certificate Manager, Azure Key Vault — for
// expiry, the same role pkg/certmanager plays for cert-manager Certificates
// and pkg/probe plays for live TLS endpoints. Every certificate found is
// flattened into the same output.Row shape everything else in this project
// uses, so the dashboard/table/webhook/Prometheus output paths need no
// changes to carry cloud-sourced rows.
package cloudcert

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/acm"

	"github.com/thev1ndu/certhealthz/pkg/output"
)

// acmAPI is the subset of *acm.Client ScanACM needs. It matches the real
// client's method signatures exactly, so *acm.Client satisfies it with no
// adapter — the seam exists purely so tests can substitute a fake
// implementing the same two methods, with no live AWS calls.
type acmAPI interface {
	ListCertificates(ctx context.Context, params *acm.ListCertificatesInput, optFns ...func(*acm.Options)) (*acm.ListCertificatesOutput, error)
	DescribeCertificate(ctx context.Context, params *acm.DescribeCertificateInput, optFns ...func(*acm.Options)) (*acm.DescribeCertificateOutput, error)
}

// ScanACM lists every certificate in AWS Certificate Manager for region
// and returns one Row per certificate, Source "aws-acm". Credentials come
// from the default AWS SDK credential chain (environment, shared config,
// EC2/ECS/EKS instance role, etc) — nothing is read from or written to
// disk by this package, and no credential is ever hardcoded.
func ScanACM(ctx context.Context, region string, warnDays int) ([]output.Row, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("loading AWS config for region %s: %w", region, err)
	}
	client := acm.NewFromConfig(cfg)
	return scanACM(ctx, client, region, warnDays)
}

// scanACM is ScanACM's client-injected core, kept separate so unit tests
// can drive it against a fake acmAPI instead of a live AWS account.
func scanACM(ctx context.Context, client acmAPI, region string, warnDays int) ([]output.Row, error) {
	var rows []output.Row
	var nextToken *string

	for {
		out, err := client.ListCertificates(ctx, &acm.ListCertificatesInput{NextToken: nextToken})
		if err != nil {
			return nil, fmt.Errorf("listing ACM certificates in %s: %w", region, err)
		}

		for _, summary := range out.CertificateSummaryList {
			if summary.CertificateArn == nil {
				continue
			}
			detail, err := client.DescribeCertificate(ctx, &acm.DescribeCertificateInput{CertificateArn: summary.CertificateArn})
			if err != nil {
				rows = append(rows, output.Row{
					Source:  "aws-acm",
					Cluster: region,
					Name:    aws.ToString(summary.DomainName),
					Status:  "error",
					Detail:  fmt.Sprintf("describing %s: %v", aws.ToString(summary.CertificateArn), err),
				})
				continue
			}

			cert := detail.Certificate
			row := output.Row{
				Source:  "aws-acm",
				Cluster: region,
				Name:    aws.ToString(cert.DomainName),
			}
			if cert.NotAfter != nil {
				row.NotAfter = *cert.NotAfter
			}
			row = output.Classify(row, warnDays)
			if cert.Issuer != nil {
				row.Detail = "issuer: " + aws.ToString(cert.Issuer)
			}
			rows = append(rows, row)
		}

		if out.NextToken == nil {
			break
		}
		nextToken = out.NextToken
	}

	return rows, nil
}
