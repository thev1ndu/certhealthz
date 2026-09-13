package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/thev1ndu/certhealthz/pkg/history"
)

const defaultHistoryDBPath = "certhealthz-history.db"

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
	historyCmd.PersistentFlags().StringVar(&historyDBPath, "db", defaultHistoryDBPath, "path to the SQLite history database")
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
