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
	var stuck bool

	c := &cobra.Command{
		Use:   "diagnose",
		Short: "Compare GTID_EXECUTED between a source and replica and report errant/missing transactions",
		Long: `diagnose compares GTID_EXECUTED on a source and a replica (via gtid_subtract)
and reports:

  - errant transactions: GTIDs the replica executed that the source never saw
    (e.g. a write that happened directly on the replica)
  - missing transactions: GTIDs the source has that the replica doesn't

For each, it prints the commands that would resolve it (injecting empty
transactions to mark the GTIDs as already applied). myrepl never runs these
itself -- review them and run them yourself once you're sure they're right.

With --stuck, if the replica's SQL thread is stopped, it also pinpoints the
exact GTID it choked on (via performance_schema) and prints ready-to-run
commands (mysqlbinlog) to inspect what that transaction actually does.

This delegates to github.com/ChaosHour/go-gtids.`,
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

			// Fix/FixMissingReplica request go-gtids' detection of errant/missing
			// transactions; DryRun keeps it to printing the statements that would
			// resolve them, never executing anything.
			unresolved, err := gtids.CheckGtidSetSubset(ctx, sourceDB, replicaDB, sourceHost, replicaHost, gtids.Options{
				Fix:               true,
				FixMissingReplica: true,
				DryRun:            true,
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
	c.Flags().BoolVar(&stuck, "stuck", false, "if the replica's SQL thread is stopped, pinpoint the exact stuck GTID and print commands to inspect it")

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
