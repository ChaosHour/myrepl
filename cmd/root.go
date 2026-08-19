// Package cmd implements the myrepl command-line interface.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "myrepl",
	Short: "Inspect and recover MySQL replication",
	Long: `myrepl automates two things you'd otherwise do by hand during a MySQL
replication incident: reading SHOW REPLICA/SLAVE STATUS into a clear summary,
and safely repointing a replica at new log coordinates (GTID auto-position or
an explicit binlog file+position) after validating them.

It auto-detects both the replica's SQL vocabulary (MySQL renamed SHOW SLAVE
STATUS / CHANGE MASTER TO across 8.0.22-23; myrepl works with either) and its
coordinate mode (GTID vs. file position).`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// SetVersion wires build metadata into the root command's --version output.
func SetVersion(version, commit, date string) {
	rootCmd.Version = fmt.Sprintf("%s (commit %s, built %s)", version, commit, date)
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(newStatusCmd(), newConfigureCmd(), newDiagnoseCmd())
}
