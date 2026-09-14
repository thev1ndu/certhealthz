package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/thev1ndu/certhealthz/pkg/history"
)

// defaultDBPath returns the SQLite database's default location: a fixed
// path under the OS's per-user config directory (~/.config, ~/Library/
// Application Support, %AppData%), so `scan --record`, `history diff`, and
// `ui` all agree on where it lives regardless of the current working
// directory each is run from — rather than each dropping (or expecting) a
// loose *.db file wherever it happens to be invoked. Named generically
// (not defaultHistoryDBPath) because the same file holds more than history:
// recorded runs, and the dashboard's persisted settings/clusters/endpoints
// (see pkg/history.Store's Save*/List* methods) — "history" is one tenant
// of it, not the whole schema. Falls back to a cwd-relative filename if the
// OS config directory can't be determined (e.g. no HOME set).
func defaultDBPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "certhealthz-state.db"
	}
	return filepath.Join(dir, "certhealthz", "state.db")
}

var historyDBPath string

var historyCmd = &cobra.Command{
	Use:   "history",
	Short: "Inspect trends recorded by `scan --record`",
}

var historyDiffCmd = &cobra.Command{
	Use:   "diff",
	Short: "Show what changed between the two most recent recorded scans",
	RunE:  runHistoryDiff,
}

func init() {
	historyCmd.PersistentFlags().StringVar(&historyDBPath, "db", defaultDBPath(), "path to the SQLite database")
	historyCmd.AddCommand(historyDiffCmd)
	rootCmd.AddCommand(historyCmd)
}

func runHistoryDiff(_ *cobra.Command, _ []string) error {
	store, err := history.Open(historyDBPath)
	if err != nil {
		return err
	}
	defer store.Close()

	latest, previous, latestAt, previousAt, err := store.LastTwoRuns()
	if err != nil {
		return err
	}
	if latest == nil {
		fmt.Println("no recorded runs yet — use `scan --record` first")
		return nil
	}
	if previous == nil {
		fmt.Printf("only one recorded run (%s) — nothing to diff against yet\n", latestAt.Format("2006-01-02 15:04:05 MST"))
		return nil
	}

	changes := history.Diff(latest, previous)
	if len(changes) == 0 {
		fmt.Printf("no change between %s and %s\n",
			previousAt.Format("2006-01-02 15:04:05 MST"), latestAt.Format("2006-01-02 15:04:05 MST"))
		return nil
	}

	fmt.Printf("changes between %s and %s:\n\n",
		previousAt.Format("2006-01-02 15:04:05 MST"), latestAt.Format("2006-01-02 15:04:05 MST"))

	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "CHANGE\tSOURCE\tCLUSTER\tNAMESPACE\tNAME\tFROM\tTO")
	for _, c := range changes {
		id := c.Identity
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			c.Kind, id.Source, id.Cluster, id.Namespace, id.Name,
			formatState(c.FromState, c.FromDays), formatState(c.ToState, c.ToDays))
	}
	return w.Flush()
}

func formatState(status string, days *int) string {
	if status == "" {
		return "-"
	}
	if days == nil {
		return status
	}
	return fmt.Sprintf("%s (%dd)", status, *days)
}
