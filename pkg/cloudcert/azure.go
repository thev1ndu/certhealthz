package cloudcert

import (
	"context"
	"fmt"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azcertificates"

	"github.com/thev1ndu/certhealthz/pkg/output"
)

// azureVaultCert is what ScanAzureKeyVault needs to know about one
// certificate in a Key Vault: its name and expiry.
type azureVaultCert struct {
	Name    string
	Expires *time.Time
}

// azureVaultClient is the narrow surface ScanAzureKeyVault needs out of a
// Key Vault. It's deliberately not azcertificates.Client's raw method
// signatures (NewListCertificatePropertiesPager returns a concrete generic
// *runtime.Pager, awkward to fake) — azureClientAdapter below bridges the
// real client to this interface, so tests can substitute a fake
// implementing this interface directly, with no live Azure calls.
type azureVaultClient interface {
	ListCertificates(ctx context.Context) ([]azureVaultCert, error)
}

// azureClientAdapter adapts a real *azcertificates.Client to
// azureVaultClient: it pages through every certificate's properties, then
// fetches each one's full attributes (properties alone omit Expires) to
// build the final azureVaultCert list.
type azureClientAdapter struct {
	client *azcertificates.Client
}

func (a azureClientAdapter) ListCertificates(ctx context.Context) ([]azureVaultCert, error) {
	var names []string
	pager := a.client.NewListCertificatePropertiesPager(nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, item := range page.Value {
			if item.ID == nil {
				continue
			}
			names = append(names, item.ID.Name())
		}
	}

	certs := make([]azureVaultCert, 0, len(names))
	for _, name := range names {
		resp, err := a.client.GetCertificate(ctx, name, "", nil)
		if err != nil {
			certs = append(certs, azureVaultCert{Name: name})
			continue
		}
		var expires *time.Time
		if resp.Attributes != nil {
			expires = resp.Attributes.Expires
		}
		certs = append(certs, azureVaultCert{Name: name, Expires: expires})
	}
	return certs, nil
}

// ScanAzureKeyVault lists every certificate in the Key Vault at vaultURL
// (e.g. "https://my-vault.vault.azure.net") and returns one Row per
// certificate, Source "azure-keyvault". Credentials come from
// azidentity.NewDefaultAzureCredential (environment, managed identity,
// Azure CLI login, etc) — nothing is read from or written to disk by this
// package, and no credential is ever hardcoded.
func ScanAzureKeyVault(ctx context.Context, vaultURL string, warnDays int) ([]output.Row, error) {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("building Azure default credential: %w", err)
	}
	client, err := azcertificates.NewClient(vaultURL, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("building Key Vault client for %s: %w", vaultURL, err)
	}

	return scanAzureKeyVault(ctx, azureClientAdapter{client: client}, vaultURL, warnDays)
}

// scanAzureKeyVault is ScanAzureKeyVault's client-injected core, kept
// separate so unit tests can drive it against a fake azureVaultClient
// instead of a live Key Vault.
func scanAzureKeyVault(ctx context.Context, client azureVaultClient, vaultURL string, warnDays int) ([]output.Row, error) {
	certs, err := client.ListCertificates(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing certificates in %s: %w", vaultURL, err)
	}

	rows := make([]output.Row, 0, len(certs))
	for _, cert := range certs {
		row := output.Row{
			Source:  "azure-keyvault",
			Cluster: vaultURL,
			Name:    cert.Name,
		}
		if cert.Expires != nil {
			row.NotAfter = *cert.Expires
		}
		row = output.Classify(row, warnDays)
		rows = append(rows, row)
	}
	return rows, nil
}
