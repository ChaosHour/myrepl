package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/ChaosHour/myrepl/internal/db"
	"github.com/ChaosHour/myrepl/internal/repl"
)

var (
	colorGood = color.New(color.FgGreen).SprintFunc()
	colorWarn = color.New(color.FgYellow).SprintFunc()
	colorBad  = color.New(color.FgRed, color.Bold).SprintFunc()
)

// threadColor highlights a replica thread's Yes/No/Connecting state: green
// when running, red when it isn't -- a stopped thread is always the issue.
func threadColor(state string) string {
	if state == "Yes" {
		return colorGood(state)
	}
	return colorBad(state)
}

func newStatusCmd() *cobra.Command {
	var conn connFlags
	var channel string

	c := &cobra.Command{
		Use:   "status",
		Short: "Show and interpret SHOW REPLICA/SLAVE STATUS for a replica",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()

			dsn, err := conn.resolve("MYREPL_DSN")
			if err != nil {
				return err
			}
			replicaDB, err := db.Open(ctx, dsn)
			if err != nil {
				return err
			}
			defer replicaDB.Close()

			syn, err := repl.Detect(ctx, replicaDB)
			if err != nil {
				return err
			}

			st, err := repl.Fetch(ctx, replicaDB, syn, channel)
			if err != nil {
				return err
			}
			printStatus(cmd, st, channel)
			return nil
		},
	}

	conn.register(c.Flags(), "", "replica")
	c.Flags().StringVar(&channel, "channel", "", "replication channel name (for multi-source replicas)")
	return c
}

func printStatus(cmd *cobra.Command, st *repl.Status, channel string) {
	out := cmd.OutOrStdout()
	if st == nil {
		if channel != "" {
			fmt.Fprintf(out, "no replication configured for channel %q\n", channel)
		} else {
			fmt.Fprintln(out, "no replication configured on this server")
		}
		return
	}

	fmt.Fprintf(out, "channel:           %s\n", orDefault(st.Channel, "(default)"))
	fmt.Fprintf(out, "mode:              %s\n", st.Mode())
	fmt.Fprintf(out, "source:            %s:%d\n", st.SourceHost, st.SourcePort)
	fmt.Fprintf(out, "io thread:         %s\n", threadColor(st.IORunning))
	fmt.Fprintf(out, "sql thread:        %s\n", threadColor(st.SQLRunning))
	if st.SecondsBehind.Valid {
		behind := st.SecondsBehind.Int64
		switch {
		case behind == 0:
			fmt.Fprintf(out, "seconds behind:    %s\n", colorGood(behind))
		case behind < 60:
			fmt.Fprintf(out, "seconds behind:    %s\n", colorWarn(behind))
		default:
			fmt.Fprintf(out, "seconds behind:    %s\n", colorBad(behind))
		}
	} else {
		fmt.Fprintln(out, colorBad("seconds behind:    NULL (io thread not connected)"))
	}
	fmt.Fprintf(out, "source log file:   %s\n", st.SourceLogFile)
	fmt.Fprintf(out, "read pos:          %d\n", st.ReadSourceLogPos)
	fmt.Fprintf(out, "executed pos:      %d\n", st.ExecSourceLogPos)
	if st.LastIOErrno != 0 {
		fmt.Fprintln(out, colorBad(fmt.Sprintf("last io error:     [%d] %s", st.LastIOErrno, st.LastIOError)))
	}
	if st.LastSQLErrno != 0 {
		fmt.Fprintln(out, colorBad(fmt.Sprintf("last sql error:    [%d] %s", st.LastSQLErrno, st.LastSQLError)))
	}
	if st.RetrievedGtidSet != "" || st.ExecutedGtidSet != "" {
		fmt.Fprintf(out, "retrieved gtids:   %s\n", orDefault(st.RetrievedGtidSet, "(none)"))
		fmt.Fprintf(out, "executed gtids:    %s\n", orDefault(st.ExecutedGtidSet, "(none)"))
	}
	if st.Healthy() {
		fmt.Fprintf(out, "healthy:           %s\n", colorGood("true"))
	} else {
		fmt.Fprintf(out, "healthy:           %s\n", colorBad("false"))
	}
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
