// Package selfupdate fetches the latest certhealthz release from GitHub and
// replaces the running binary with it. It talks only to the GitHub REST API
// and the release's own asset URLs — no third-party update service.
package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Repo is the GitHub "owner/name" this binary is released from.
const Repo = "thev1ndu/certhealthz"

// Asset is one file attached to a GitHub release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// Release is the subset of the GitHub releases API response this package
// needs.
type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

// Find returns the asset with the given name, if present.
func (r *Release) Find(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

// FetchLatest fetches the latest published release for repo (e.g.
// "thev1ndu/certhealthz") from the GitHub API. If the GITHUB_TOKEN
// environment variable is set, it's sent as a bearer token to raise the
// unauthenticated rate limit.
func FetchLatest(ctx context.Context, repo string) (*Release, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching latest release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("fetching latest release: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("decoding release response: %w", err)
	}
	return &rel, nil
}

// AssetName returns the goreleaser archive name for the given OS/arch, e.g.
// "certhealthz_darwin_arm64.tar.gz" or "certhealthz_windows_amd64.zip".
func AssetName(goos, goarch string) string {
	ext := "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("certhealthz_%s_%s.%s", goos, goarch, ext)
}

// BinaryName is the name of the certhealthz executable inside a release
// archive for the given OS.
func BinaryName(goos string) string {
	if goos == "windows" {
		return "certhealthz.exe"
	}
	return "certhealthz"
}

func download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// Download fetches an asset's contents in full.
func Download(ctx context.Context, url string) ([]byte, error) {
	return download(ctx, url)
}

// VerifyChecksum checks data's sha256 against the entry for filename in a
// checksums.txt (standard "<hex digest>  <filename>" lines, one per file).
func VerifyChecksum(data []byte, checksumsTxt, filename string) error {
	var want string
	for _, line := range strings.Split(checksumsTxt, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == filename {
			want = fields[0]
			break
		}
	}
	if want == "" {
		return fmt.Errorf("no checksum entry for %s", filename)
	}
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("checksum mismatch for %s: got %s, want %s", filename, got, want)
	}
	return nil
}

// ExtractBinary pulls binaryName out of a .tar.gz or .zip archive, picking
// the format from archiveName's extension.
func ExtractBinary(archiveData []byte, archiveName, binaryName string) ([]byte, error) {
	if strings.HasSuffix(archiveName, ".zip") {
		return extractFromZip(archiveData, binaryName)
	}
	return extractFromTarGz(archiveData, binaryName)
}

func extractFromTarGz(data []byte, binaryName string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("opening tar.gz: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading tar entry: %w", err)
		}
		if filepath.Base(hdr.Name) == binaryName {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("%s not found in archive", binaryName)
}

func extractFromZip(data []byte, binaryName string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("opening zip: %w", err)
	}
	for _, f := range zr.File {
		if filepath.Base(f.Name) == binaryName {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("reading zip entry: %w", err)
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("%s not found in archive", binaryName)
}

// Apply atomically replaces the executable at path with newBinary's
// contents. The old binary is renamed to path+".old" first rather than
// deleted outright, so a failed second rename can be rolled back; on
// success the backup is removed where the OS allows it (it can't be, on
// Windows, while this process is still running from it — that's reported
// back, not treated as an error).
func Apply(path string, newBinary []byte) (backupKept bool, err error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".certhealthz-update-*")
	if err != nil {
		return false, fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds

	if _, err := tmp.Write(newBinary); err != nil {
		_ = tmp.Close()
		return false, fmt.Errorf("writing new binary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return false, fmt.Errorf("writing new binary: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil { //nolint:gosec // replacing an executable binary; it must stay executable
		return false, fmt.Errorf("making new binary executable: %w", err)
	}

	backup := path + ".old"
	_ = os.Remove(backup) // best-effort; a stale backup from a prior update shouldn't block this one
	if err := os.Rename(path, backup); err != nil {
		return false, fmt.Errorf("backing up current binary: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		// Roll back so the user isn't left without a working binary.
		_ = os.Rename(backup, path)
		return false, fmt.Errorf("installing new binary: %w", err)
	}

	if rmErr := os.Remove(backup); rmErr != nil {
		return true, nil
	}
	return false, nil
}

// CompareVersions returns -1, 0, or 1 as current is older, equal to, or
// newer than latest. Both are compared after stripping a leading "v" and
// any pre-release/build suffix (a "-" or "+" and everything after). Versions
// that don't parse as dotted integers fall back to a plain string compare.
func CompareVersions(current, latest string) int {
	c := strings.TrimPrefix(current, "v")
	l := strings.TrimPrefix(latest, "v")
	cp, cOk := parseVersion(c)
	lp, lOk := parseVersion(l)
	if !cOk || !lOk {
		switch {
		case c == l:
			return 0
		case c < l:
			return -1
		default:
			return 1
		}
	}
	for i := 0; i < len(cp) || i < len(lp); i++ {
		var a, b int
		if i < len(cp) {
			a = cp[i]
		}
		if i < len(lp) {
			b = lp[i]
		}
		if a != b {
			if a < b {
				return -1
			}
			return 1
		}
	}
	return 0
}

func parseVersion(v string) ([]int, bool) {
	v = strings.SplitN(v, "-", 2)[0]
	v = strings.SplitN(v, "+", 2)[0]
	parts := strings.Split(v, ".")
	nums := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, false
		}
		nums[i] = n
	}
	return nums, len(nums) > 0
}

// GOOS and GOARCH of the running binary, exposed so callers don't need to
// import "runtime" just for this.
var (
	GOOS   = runtime.GOOS
	GOARCH = runtime.GOARCH
)
