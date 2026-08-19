package cmd

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ChaosHour/myrepl/internal/db"
	"github.com/ChaosHour/myrepl/internal/repl"
)

func newConfigureCmd() *cobra.Command {
	var replicaConn, sourceConn connFlags
	var channel, logFile string
	var logPos uint64
	var useGTID, dryRun, force, yes bool

	c := &cobra.Command{
		Use:   "configure",
		Short: "Safely repoint a replica at new log coordinates and restart replication",
		Long: `configure stops replication, points it at either GTID auto-position or an
explicit binlog file+position, and starts it back up.

Before touching anything it prints the current state and the exact
statements it plans to run. If --source-dsn (or --source-defaults-*) is
given, it also validates a requested file/position actually exist on the
source via SHOW BINARY LOGS. Nothing is executed until you confirm, or pass
--yes; validation failures require --force to override.`,
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
				fmt.Fprintf(out, "warning: %s\n", w)
			}
			fmt.Fprintln(out, "plan:")
			for _, stmt := range plan.Statements {
				fmt.Fprintf(out, "  %s;\n", stmt)
			}

			if dryRun {
				fmt.Fprintln(out, "\n--dry-run: nothing executed")
				return nil
			}

			if !yes {
				fmt.Fprint(out, "\nrun this plan? [y/N] ")
				reader := bufio.NewReader(cmd.InOrStdin())
				line, _ := reader.ReadString('\n')
				if strings.ToLower(strings.TrimSpace(line)) != "y" {
					fmt.Fprintln(out, "aborted")
					return nil
				}
			}

			if err := repl.Apply(ctx, replicaDB, plan); err != nil {
				return fmt.Errorf("apply plan (replication may now be stopped -- check `myrepl status` and retry): %w", err)
			}

			fmt.Fprintln(out, "\nresulting state:")
			after, err := repl.Fetch(ctx, replicaDB, syn, channel)
			if err != nil {
				return err
			}
			printStatus(cmd, after, channel)
			return nil
		},
	}

	replicaConn.register(c.Flags(), "", "replica")
	sourceConn.register(c.Flags(), "source-", "source")
	c.Flags().StringVar(&channel, "channel", "", "replication channel name (for multi-source replicas)")
	c.Flags().BoolVar(&useGTID, "gtid", false, "use GTID auto-position instead of an explicit log file/position")
	c.Flags().StringVar(&logFile, "log-file", "", "source binlog file to resume from (file-position mode)")
	c.Flags().Uint64Var(&logPos, "log-pos", 0, "source binlog position to resume from (file-position mode)")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "print the plan without executing it")
	c.Flags().BoolVar(&force, "force", false, "proceed despite validation warnings (e.g. moving the position backward, an unreachable/unvalidated source, or a currently-healthy replica)")
	c.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt")

	return c
}
