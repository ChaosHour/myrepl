package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/ChaosHour/myrepl/internal/db"
	"github.com/ChaosHour/myrepl/internal/repl"
)

func newConfigureCmd() *cobra.Command {
	var replicaConn, sourceConn connFlags
	var channel, logFile string
	var logPos uint64
	var useGTID, force bool

	c := &cobra.Command{
		Use:   "configure",
		Short: "Compute and print the statements to safely repoint a replica at new log coordinates",
		Long: `configure validates a requested GTID auto-position or explicit binlog
file+position against the replica's current state (and, if --source-dsn or
--source-defaults-* is given, against the source's actual SHOW BINARY LOGS),
then prints the current state and the exact STOP/CHANGE/START statements
that would apply the change.

myrepl never runs these statements itself -- copy/paste them into a mysql
session yourself once you're satisfied they're correct. Validation failures
require --force to override (they print as warnings instead of aborting).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			if !useGTID && (logFile == "" || logPos == 0) {
				return fmt.Errorf("specify either --gtid, or both --log-file and --log-pos")
			}

			replicaDSN, err := replicaConn.resolve("MYREPL_DSN")
			if err != nil {
				return err
			}
			replicaDB, err := db.Open(ctx, replicaDSN)
			if err != nil {
				return err
			}
			defer replicaDB.Close()

			var sourceDB *sql.DB
			if sourceConn.given("MYREPL_SOURCE_DSN") {
				sourceDSN, err := sourceConn.resolve("MYREPL_SOURCE_DSN")
				if err != nil {
					return err
				}
				sourceDB, err = db.Open(ctx, sourceDSN)
				if err != nil {
					return err
				}
				defer sourceDB.Close()
			}

			syn, err := repl.Detect(ctx, replicaDB)
			if err != nil {
				return err
			}

			coords := repl.Coordinates{GTID: useGTID, LogFile: logFile, LogPos: logPos}
			plan, err := repl.BuildPlan(ctx, replicaDB, syn, channel, coords, sourceDB, force)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if plan.Before != nil {
				fmt.Fprintln(out, "current state:")
				printStatus(cmd, plan.Before, channel)
				fmt.Fprintln(out)
			}
			for _, w := range plan.Warnings {
				fmt.Fprintln(out, color.New(color.FgYellow, color.Bold).Sprintf("warning: %s", w))
			}
			fmt.Fprintln(out, "plan:")
			for _, stmt := range plan.Statements {
				fmt.Fprintf(out, "  %s;\n", stmt)
			}
			fmt.Fprintln(out, "\nmyrepl does not execute plans -- run the statements above yourself once you're satisfied they're correct.")
			return nil
		},
	}

	replicaConn.register(c.Flags(), "", "replica")
	sourceConn.register(c.Flags(), "source-", "source")
	c.Flags().StringVar(&channel, "channel", "", "replication channel name (for multi-source replicas)")
	c.Flags().BoolVar(&useGTID, "gtid", false, "use GTID auto-position instead of an explicit log file/position")
	c.Flags().StringVar(&logFile, "log-file", "", "source binlog file to resume from (file-position mode)")
	c.Flags().Uint64Var(&logPos, "log-pos", 0, "source binlog position to resume from (file-position mode)")
	c.Flags().BoolVar(&force, "force", false, "proceed despite validation warnings (e.g. moving the position backward, an unreachable/unvalidated source, or a currently-healthy replica)")

	return c
}
