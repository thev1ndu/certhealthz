# Installing and updating from a release

Every push of a `v*` tag triggers `.github/workflows/release.yml`, which runs
`goreleaser` and publishes binaries + `checksums.txt` to the
[releases page](https://github.com/thev1ndu/certhealthz/releases). This
guide covers downloading, verifying, installing, and updating one of those
release binaries directly (as opposed to `go install` or building from
source — see the main [README](../README.md#install) for those).

## 1. Pick your asset

| OS | Arch | Asset |
|---|---|---|
| macOS (Apple Silicon) | arm64 | `certhealthz_darwin_arm64.tar.gz` |
| macOS (Intel) | amd64 | `certhealthz_darwin_amd64.tar.gz` |
| Linux | amd64 | `certhealthz_linux_amd64.tar.gz` |
| Linux | arm64 | `certhealthz_linux_arm64.tar.gz` |
| Windows | amd64 | `certhealthz_windows_amd64.zip` |
| Windows | arm64 | `certhealthz_windows_arm64.zip` |

Download it, plus `checksums.txt`, from the release's asset list.

## 2. Verify the checksum (recommended)

macOS / Linux:

```sh
shasum -a 256 -c <(grep certhealthz_darwin_arm64.tar.gz checksums.txt)
```

(swap the filename for whichever asset you downloaded)

Windows (PowerShell):

```powershell
Get-FileHash certhealthz_windows_amd64.zip -Algorithm SHA256
# compare the output against the matching line in checksums.txt
```

## 3. Install

### macOS / Linux

```sh
tar -xzf certhealthz_darwin_arm64.tar.gz   # produces ./certhealthz
```

macOS only — Gatekeeper blocks an unsigned binary from an unidentified
developer, so clear the quarantine flag before running it:

```sh
xattr -d com.apple.quarantine certhealthz
```

Make it executable and put it on your `PATH`:

```sh
chmod +x certhealthz
sudo mv certhealthz /usr/local/bin/
```

Verify:

```sh
certhealthz --help
```

### Windows

```powershell
Expand-Archive certhealthz_windows_amd64.zip -DestinationPath .
```

Move `certhealthz.exe` into a directory on your `PATH`, or run it in place:

```powershell
.\certhealthz.exe --help
```

## 4. Updating to a newer release

Check what you're running:

```sh
certhealthz --version
```

Then update in place:

```sh
certhealthz update           # check, confirm, and install
certhealthz update --check   # just report whether an update is available
certhealthz update -y        # skip the confirmation prompt
```

`update` fetches the [latest release](https://github.com/thev1ndu/certhealthz/releases/latest),
downloads the asset matching your OS/arch, verifies it against
`checksums.txt`, and replaces the running binary in place — the same binary
this guide's steps 1–3 install manually, so there's no separate
uninstall/migration step. The old binary is kept as `<path>.old` until it
can be removed (immediately on macOS/Linux; after you next restart the
process on Windows, since it can't delete a binary that's still running).

You can still update manually instead: repeat steps 1–3 with the new
release's assets, and overwrite the old binary at the same path.

If you installed via `go install github.com/thev1ndu/certhealthz@latest`,
updating is just re-running that command — `certhealthz update` also works
for that install path (it replaces whatever binary is currently running),
but won't keep the Go module cache in sync with it.
