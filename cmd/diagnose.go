package cmd

import (
	"context"
	"os"
	"strings"
	"time"

	gomysql "github.com/go-sql-driver/mysql"
	"github.com/spf13/cobra"

	"github.com/ChaosHour/go-gtids/pkg/gtids"
	"github.com/ChaosHour/myrepl/internal/db"
)

func newDiagnoseCmd() *cobra.Command {
	var replicaConn, sourceConn connFlags
	var fix, fixReplica, fixMissingReplica, dryRun, force, stuck bool

	c := &cobra.Command{
		Use:   "diagnose",
		Short: "Compare GTID_EXECUTED between a source and replica and report errant/missing transactions",
		Long: `diagnose compares GTID_EXECUTED on a source and a replica (via gtid_subtract)
and reports:

  - errant transactions: GTIDs the replica executed that the source never saw
    (e.g. a write that happened directly on the replica)
  - missing transactions: GTIDs the source has that the replica doesn't

With --stuck, if the replica's SQL thread is stopped, it also pinpoints the
exact GTID it choked on (via performance_schema) and prints ready-to-run
commands (mysqlbinlog) to inspect what that transaction actually does.

--fix / --fix-replica / --fix-missing-replica inject empty transactions to
resolve what diagnose found; each prints a confirmation prompt (skip with
--yes) and supports --dry-run to preview without executing. This delegates
to github.com/ChaosHour/go-gtids.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			sourceDSN, err := sourceConn.resolve("MYREPL_SOURCE_DSN")
			if err != nil {
				return err
			}
			sourceDB, err := db.Open(ctx, sourceDSN)
			if err != nil {
				return err
			}
			defer sourceDB.Close()

			replicaDSN, err := replicaConn.resolve("MYREPL_DSN")
			if err != nil {
				return err
			}
			replicaDB, err := db.Open(ctx, replicaDSN)
			if err != nil {
				return err
			}
			defer replicaDB.Close()

			sourceHost, sourcePort := hostPort(sourceDSN, "source")
			replicaHost, replicaPort := hostPort(replicaDSN, "replica")

			unresolved, err := gtids.CheckGtidSetSubset(ctx, sourceDB, replicaDB, sourceHost, replicaHost, gtids.Options{
				Fix:               fix,
				FixReplica:        fixReplica,
				FixMissingReplica: fixMissingReplica,
				DryRun:            dryRun,
				AssumeYes:         force,
			})
			if err != nil {
				return err
			}

			if stuck {
				if err := gtids.DiagnoseStuckReplication(ctx, sourceDB, replicaDB, sourceHost, sourcePort, replicaHost, replicaPort); err != nil {
					return err
				}
			}

			if unresolved {
				os.Exit(2) // matches go-gtids' own convention: 2 = errant/missing transactions remain
			}
			return nil
		},
	}

	replicaConn.register(c.Flags(), "", "replica")
	sourceConn.register(c.Flags(), "source-", "source")
	c.Flags().BoolVar(&fix, "fix", false, "apply errant GTIDs to the source (they replicate downstream, where they are auto-skipped)")
	c.Flags().BoolVar(&fixReplica, "fix-replica", false, "apply errant GTIDs to the replica (stops/restarts replication)")
	c.Flags().BoolVar(&fixMissingReplica, "fix-missing-replica", false, "mark missing GTIDs as executed on the replica WITHOUT applying their data (their data must be synced separately)")
	c.Flags().BoolVar(&stuck, "stuck", false, "if the replica's SQL thread is stopped, pinpoint the exact stuck GTID and print commands to inspect it")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "print the statements a fix would execute without running them")
	c.Flags().BoolVar(&force, "yes", false, "skip the confirmation prompt before applying fixes")

	return c
}

// hostPort extracts a "host", "port" pair from a DSN for display and for
// go-gtids' mysqlbinlog command suggestions, falling back to a generic label
// when the DSN can't be parsed (e.g. a unix socket).
func hostPort(dsn, label string) (host, port string) {
	cfg, err := gomysql.ParseDSN(dsn)
	if err != nil || cfg.Net != "tcp" {
		return label, "3306"
	}
	h, p, ok := strings.Cut(cfg.Addr, ":")
	if !ok {
		return cfg.Addr, "3306"
	}
	return h, p
}
