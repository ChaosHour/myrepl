package repl

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
)

// Status is a parsed, vocabulary-independent view of one
// SHOW [REPLICA|SLAVE] STATUS row. Field names follow the 8.0.23+ wording
// regardless of which vocabulary the server used to answer.
type Status struct {
	Channel          string
	SourceHost       string
	SourcePort       int
	IORunning        string // Yes / No / Connecting
	SQLRunning       string // Yes / No
	LastIOErrno      int
	LastIOError      string
	LastSQLErrno     int
	LastSQLError     string
	SourceLogFile    string
	ReadSourceLogPos uint64
	ExecSourceLogPos uint64
	SecondsBehind    sql.NullInt64
	RetrievedGtidSet string
	ExecutedGtidSet  string
	AutoPosition     bool

	// Raw holds every column the server returned, keyed by its own name, for
	// anything not surfaced above.
	Raw map[string]string
}

// Mode is the replication coordinate system currently in effect.
type Mode int

const (
	ModeUnknown Mode = iota
	ModeGTID
	ModeFile
)

func (m Mode) String() string {
	switch m {
	case ModeGTID:
		return "GTID"
	case ModeFile:
		return "file position"
	default:
		return "unknown"
	}
}

// Mode reports whether the channel is currently running GTID auto-position
// or plain file-position replication.
func (s *Status) Mode() Mode {
	if s.AutoPosition {
		return ModeGTID
	}
	if s.SourceLogFile != "" {
		return ModeFile
	}
	return ModeUnknown
}

// Healthy reports whether both threads are running with no recorded error.
func (s *Status) Healthy() bool {
	return s.IORunning == "Yes" && s.SQLRunning == "Yes" && s.LastIOErrno == 0 && s.LastSQLErrno == 0
}

// Fetch runs SHOW [REPLICA|SLAVE] STATUS [FOR CHANNEL '...'] and parses the
// single row it returns. A nil Status with a nil error means the connection
// has no replication configured for that channel (empty result set).
func Fetch(ctx context.Context, db *sql.DB, syn Syntax, channel string) (*Status, error) {
	stmt := syn.StatusStmt(channel)
	rows, err := db.QueryContext(ctx, stmt)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", stmt, err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	if !rows.Next() {
		return nil, rows.Err()
	}

	vals := make([]sql.NullString, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	raw := make(map[string]string, len(cols))
	for i, c := range cols {
		raw[c] = vals[i].String
	}
	return parseRow(raw), nil
}

// get returns the first present value among the given column names, trying
// the new (Source_/Replica_) wording before the legacy (Master_/Slave_) one.
func get(raw map[string]string, names ...string) string {
	for _, n := range names {
		if v, ok := raw[n]; ok {
			return v
		}
	}
	return ""
}

func parseRow(raw map[string]string) *Status {
	s := &Status{Raw: raw}
	s.Channel = get(raw, "Channel_Name")
	s.SourceHost = get(raw, "Source_Host", "Master_Host")
	s.SourcePort, _ = strconv.Atoi(get(raw, "Source_Port", "Master_Port"))
	s.IORunning = get(raw, "Replica_IO_Running", "Slave_IO_Running")
	s.SQLRunning = get(raw, "Replica_SQL_Running", "Slave_SQL_Running")
	s.LastIOErrno, _ = strconv.Atoi(get(raw, "Last_IO_Errno"))
	s.LastIOError = get(raw, "Last_IO_Error")
	s.LastSQLErrno, _ = strconv.Atoi(get(raw, "Last_SQL_Errno"))
	s.LastSQLError = get(raw, "Last_SQL_Error")
	s.SourceLogFile = get(raw, "Source_Log_File", "Master_Log_File")
	s.ReadSourceLogPos, _ = strconv.ParseUint(get(raw, "Read_Source_Log_Pos", "Read_Master_Log_Pos"), 10, 64)
	s.ExecSourceLogPos, _ = strconv.ParseUint(get(raw, "Exec_Source_Log_Pos", "Exec_Master_Log_Pos"), 10, 64)
	if v := get(raw, "Seconds_Behind_Source", "Seconds_Behind_Master"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			s.SecondsBehind = sql.NullInt64{Int64: n, Valid: true}
		}
	}
	s.RetrievedGtidSet = get(raw, "Retrieved_Gtid_Set")
	s.ExecutedGtidSet = get(raw, "Executed_Gtid_Set")
	s.AutoPosition = get(raw, "Auto_Position") == "1"
	return s
}
