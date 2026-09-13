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

certhealthz doesn't have a `--version` flag yet, so there's no in-binary way
to check what you're running — compare the tag you downloaded against the
[latest release](https://github.com/thev1ndu/certhealthz/releases/latest).

To update: repeat steps 1–3 with the new release's assets, and replace the
old binary at the same path (`/usr/local/bin/certhealthz`, or wherever you
put `certhealthz.exe`). There's no separate uninstall/migration step —
overwriting the binary is the entire upgrade.

If you installed via `go install github.com/thev1ndu/certhealthz@latest`
instead, updating is just re-running that command.
