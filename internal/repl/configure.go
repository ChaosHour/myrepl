package repl

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
)

// Coordinates describes where a replica should resume reading from its
// source: either GTID auto-position, or an explicit binlog file+position.
type Coordinates struct {
	GTID    bool
	LogFile string
	LogPos  uint64
}

func (c Coordinates) validate() error {
	if c.GTID {
		if c.LogFile != "" || c.LogPos != 0 {
			return fmt.Errorf("--gtid and an explicit log file/position are mutually exclusive")
		}
		return nil
	}
	if c.LogFile == "" || c.LogPos == 0 {
		return fmt.Errorf("file-position mode requires both a log file and a log position greater than 0")
	}
	return nil
}

// Plan is a reviewable set of statements BuildPlan wants to run, plus any
// warnings surfaced while building it. Nothing is executed until Apply runs.
type Plan struct {
	Channel    string
	Statements []string
	Warnings   []string
	Before     *Status
}

// BuildPlan validates the requested coordinates against the replica's
// current state (and, if sourceDB is non-nil, against the source's actual
// binary logs) and produces the STOP/CHANGE/START statements needed to
// apply them. It performs no writes. force downgrades would-be-fatal
// validation failures to warnings.
func BuildPlan(ctx context.Context, replicaDB *sql.DB, syn Syntax, channel string, coords Coordinates, sourceDB *sql.DB, force bool) (*Plan, error) {
	if err := coords.validate(); err != nil {
		return nil, err
	}

	before, err := Fetch(ctx, replicaDB, syn, channel)
	if err != nil {
		return nil, fmt.Errorf("read current replica status: %w", err)
	}

	plan := &Plan{Channel: channel, Before: before}

	if coords.GTID {
		var gtidMode string
		if err := replicaDB.QueryRowContext(ctx, "SELECT @@GLOBAL.gtid_mode").Scan(&gtidMode); err != nil {
			return nil, fmt.Errorf("check gtid_mode on replica: %w", err)
		}
		if gtidMode != "ON" {
			return nil, fmt.Errorf("replica has gtid_mode=%s; GTID auto-position requires gtid_mode=ON (rerun with --log-file/--log-pos instead)", gtidMode)
		}
	} else if sourceDB != nil {
		if err := validateLogFileOnSource(ctx, sourceDB, coords); err != nil {
			if !force {
				return nil, err
			}
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("%v (continuing: --force)", err))
		}
	} else {
		plan.Warnings = append(plan.Warnings, "no source connection given: the requested log file/position was not verified against the source's actual binary logs")
	}

	if before != nil && !coords.GTID && before.SourceLogFile == coords.LogFile && coords.LogPos < before.ExecSourceLogPos {
		msg := fmt.Sprintf("requested position %d is behind the replica's already-executed position %d in %s; this would replay already-applied events",
			coords.LogPos, before.ExecSourceLogPos, coords.LogFile)
		if !force {
			return nil, fmt.Errorf("%s (rerun with --force to proceed anyway)", msg)
		}
		plan.Warnings = append(plan.Warnings, msg+" (continuing: --force)")
	}

	if before != nil && before.Healthy() {
		if !force {
			return nil, fmt.Errorf("replica currently looks healthy (both threads running, no errors) -- refusing to repoint it without --force")
		}
		plan.Warnings = append(plan.Warnings, "replica currently looks healthy (both threads running, no errors) -- continuing: --force")
	}

	plan.Statements = append(plan.Statements, syn.StopStmt(channel))
	if coords.GTID {
		plan.Statements = append(plan.Statements, syn.ChangeToGTIDStmt(channel))
	} else {
		plan.Statements = append(plan.Statements, syn.ChangeToFileStmt(channel, coords.LogFile, coords.LogPos))
	}
	plan.Statements = append(plan.Statements, syn.StartStmt(channel))

	return plan, nil
}

// validateLogFileOnSource confirms coords.LogFile exists on the source and
// coords.LogPos falls within it, using SHOW BINARY LOGS.
func validateLogFileOnSource(ctx context.Context, sourceDB *sql.DB, coords Coordinates) error {
	rows, err := sourceDB.QueryContext(ctx, "SHOW BINARY LOGS")
	if err != nil {
		return fmt.Errorf("list binary logs on source: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return err
	}

	found := false
	var size uint64
	for rows.Next() {
		vals := make([]sql.RawBytes, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return fmt.Errorf("scan SHOW BINARY LOGS row: %w", err)
		}
		if string(vals[0]) == coords.LogFile {
			found = true
			size, _ = strconv.ParseUint(string(vals[1]), 10, 64)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if !found {
		return fmt.Errorf("source has no binary log named %s (check SHOW BINARY LOGS on the source)", coords.LogFile)
	}
	if coords.LogPos > size {
		return fmt.Errorf("requested position %d is past the end of %s on the source (size %d bytes)", coords.LogPos, coords.LogFile, size)
	}
	return nil
}

// Apply executes a Plan's statements in order against replicaDB, stopping at
// the first error. Callers should re-Fetch status afterward regardless of
// error, since STOP (or STOP+CHANGE) may already have run.
func Apply(ctx context.Context, replicaDB *sql.DB, plan *Plan) error {
	for _, stmt := range plan.Statements {
		if _, err := replicaDB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("%s: %w", stmt, err)
		}
	}
	return nil
}
