package cloudcert

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeAzureClient is a hand-rolled fake azureVaultClient — no live Azure
// calls, no network.
type fakeAzureClient struct {
	certs []azureVaultCert
	err   error
}

func (f *fakeAzureClient) ListCertificates(_ context.Context) ([]azureVaultCert, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.certs, nil
}

func TestScanAzureKeyVault(t *testing.T) {
	expires := time.Now().Add(60 * 24 * time.Hour)
	fake := &fakeAzureClient{certs: []azureVaultCert{
		{Name: "my-cert", Expires: &expires},
	}}

	rows, err := scanAzureKeyVault(context.Background(), fake, "https://my-vault.vault.azure.net", 14)
	if err != nil {
		t.Fatalf("scanAzureKeyVault: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	row := rows[0]
	if row.Source != "azure-keyvault" {
		t.Errorf("expected Source azure-keyvault, got %q", row.Source)
	}
	if row.Name != "my-cert" {
		t.Errorf("expected Name my-cert, got %q", row.Name)
	}
	if row.Status != "ok" {
		t.Errorf("expected Status ok, got %q", row.Status)
	}
}

func TestScanAzureKeyVaultExpiring(t *testing.T) {
	expires := time.Now().Add(5 * 24 * time.Hour)
	fake := &fakeAzureClient{certs: []azureVaultCert{
		{Name: "soon-cert", Expires: &expires},
	}}

	rows, err := scanAzureKeyVault(context.Background(), fake, "https://my-vault.vault.azure.net", 14)
	if err != nil {
		t.Fatalf("scanAzureKeyVault: %v", err)
	}
	if rows[0].Status != "expiring" {
		t.Errorf("expected Status expiring, got %q", rows[0].Status)
	}
}

func TestScanAzureKeyVaultListError(t *testing.T) {
	fake := &fakeAzureClient{err: errors.New("unauthorized")}
	_, err := scanAzureKeyVault(context.Background(), fake, "https://my-vault.vault.azure.net", 14)
	if err == nil {
		t.Fatal("expected an error when listing fails")
	}
}
