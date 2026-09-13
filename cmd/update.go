package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/thev1ndu/certhealthz/pkg/selfupdate"
)

var (
	updateCheckOnly bool
	updateYes       bool
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update certhealthz to the latest release",
	Long: `Checks the latest GitHub release, and — unless --check is given — downloads
it, verifies its checksum, and replaces this binary in place.

A dev build (no embedded version) can't tell if it's current, so update
always offers to install the latest release for it.`,
	RunE: runUpdate,
}

func init() {
	updateCmd.Flags().BoolVar(&updateCheckOnly, "check", false, "only report whether an update is available; don't install it")
	updateCmd.Flags().BoolVarP(&updateYes, "yes", "y", false, "install without asking for confirmation")
	rootCmd.AddCommand(updateCmd)
}

func runUpdate(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()

	rel, err := selfupdate.FetchLatest(ctx, selfupdate.Repo)
	if err != nil {
		return err
	}

	known := Version != "dev"
	if known && selfupdate.CompareVersions(Version, rel.TagName) >= 0 {
		fmt.Printf("certhealthz %s is already up to date (latest: %s)\n", Version, rel.TagName)
		return nil
	}
	if known {
		fmt.Printf("update available: %s -> %s\n", Version, rel.TagName)
	} else {
		fmt.Printf("running a dev build; latest release is %s\n", rel.TagName)
	}
	if updateCheckOnly {
		return nil
	}

	assetName := selfupdate.AssetName(selfupdate.GOOS, selfupdate.GOARCH)
	asset, ok := rel.Find(assetName)
	if !ok {
		return fmt.Errorf("no release asset for %s/%s (%s)", selfupdate.GOOS, selfupdate.GOARCH, assetName)
	}
	checksumsAsset, ok := rel.Find("checksums.txt")
	if !ok {
		return fmt.Errorf("release %s has no checksums.txt", rel.TagName)
	}

	if !updateYes && !confirm(fmt.Sprintf("Install %s over the running binary? [y/N] ", rel.TagName)) {
		fmt.Println("aborted")
		return nil
	}

	fmt.Printf("downloading %s...\n", asset.Name)
	archive, err := selfupdate.Download(ctx, asset.URL)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", asset.Name, err)
	}
	checksums, err := selfupdate.Download(ctx, checksumsAsset.URL)
	if err != nil {
		return fmt.Errorf("downloading checksums.txt: %w", err)
	}
	if err := selfupdate.VerifyChecksum(archive, string(checksums), asset.Name); err != nil {
		return err
	}

	binaryName := selfupdate.BinaryName(selfupdate.GOOS)
	newBinary, err := selfupdate.ExtractBinary(archive, asset.Name, binaryName)
	if err != nil {
		return fmt.Errorf("extracting %s: %w", binaryName, err)
	}

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating running binary: %w", err)
	}

	backupKept, err := selfupdate.Apply(execPath, newBinary)
	if err != nil {
		return fmt.Errorf("installing update: %w", err)
	}

	fmt.Printf("updated %s to %s\n", execPath, rel.TagName)
	if backupKept {
		fmt.Printf("old binary kept at %s.old (couldn't be removed while running); safe to delete after restarting\n", execPath)
	}
	fmt.Println("the new binary takes effect on your next run of certhealthz")
	return nil
}

func confirm(prompt string) bool {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}
