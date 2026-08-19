// Package repl reads and repairs MySQL replication: parsing SHOW
// [REPLICA|SLAVE] STATUS and building/applying safe CHANGE
// [REPLICATION SOURCE|MASTER] TO plans, across both the pre- and
// post-8.0.23 command vocabularies and both GTID and file-position
// coordinate modes.
package repl

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Syntax selects which command vocabulary a server understands. MySQL
// renamed SHOW SLAVE STATUS -> SHOW REPLICA STATUS and STOP/START SLAVE ->
// STOP/START REPLICA in 8.0.22, and CHANGE MASTER TO -> CHANGE REPLICATION
// SOURCE TO in 8.0.23. MariaDB never adopted the rename. Legacy verbs still
// work as deprecated aliases through 8.0.x but that isn't guaranteed to
// hold forever, so myrepl detects rather than assumes.
type Syntax struct {
	Legacy bool
}

var versionRe = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)`)

// Detect inspects @@version to choose a command vocabulary.
func Detect(ctx context.Context, db *sql.DB) (Syntax, error) {
	var version string
	if err := db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&version); err != nil {
		return Syntax{}, fmt.Errorf("read server version: %w", err)
	}

	if strings.Contains(strings.ToLower(version), "mariadb") {
		return Syntax{Legacy: true}, nil
	}

	m := versionRe.FindStringSubmatch(version)
	if m == nil {
		return Syntax{Legacy: true}, nil // unrecognized format: fall back to the widely-supported verbs
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	patch, _ := strconv.Atoi(m[3])
	if major > 8 || (major == 8 && (minor > 0 || patch >= 23)) {
		return Syntax{Legacy: false}, nil
	}
	return Syntax{Legacy: true}, nil
}

func (s Syntax) forClause(channel string) string {
	if channel == "" {
		return ""
	}
	return " FOR CHANNEL " + quoteString(channel)
}

// StatusStmt returns SHOW REPLICA STATUS or SHOW SLAVE STATUS.
func (s Syntax) StatusStmt(channel string) string {
	verb := "SHOW REPLICA STATUS"
	if s.Legacy {
		verb = "SHOW SLAVE STATUS"
	}
	return verb + s.forClause(channel)
}

// StopStmt returns STOP REPLICA or STOP SLAVE.
func (s Syntax) StopStmt(channel string) string {
	verb := "STOP REPLICA"
	if s.Legacy {
		verb = "STOP SLAVE"
	}
	return verb + s.forClause(channel)
}

// StartStmt returns START REPLICA or START SLAVE.
func (s Syntax) StartStmt(channel string) string {
	verb := "START REPLICA"
	if s.Legacy {
		verb = "START SLAVE"
	}
	return verb + s.forClause(channel)
}

// ChangeToGTIDStmt returns a CHANGE .. TO statement that switches the
// channel to GTID auto-position.
func (s Syntax) ChangeToGTIDStmt(channel string) string {
	if s.Legacy {
		return fmt.Sprintf("CHANGE MASTER TO MASTER_AUTO_POSITION = 1%s", s.forClause(channel))
	}
	return fmt.Sprintf("CHANGE REPLICATION SOURCE TO SOURCE_AUTO_POSITION = 1%s", s.forClause(channel))
}

// ChangeToFileStmt returns a CHANGE .. TO statement that points the channel
// at an explicit binlog file and position. It always sets AUTO_POSITION = 0
// alongside the coordinates: MySQL rejects a bare LOG_FILE/LOG_POS change
// (error 1776) while auto-position is still active on the channel, so the
// two must be cleared together even when the channel wasn't using GTID.
func (s Syntax) ChangeToFileStmt(channel, logFile string, logPos uint64) string {
	if s.Legacy {
		return fmt.Sprintf("CHANGE MASTER TO MASTER_AUTO_POSITION = 0, MASTER_LOG_FILE = %s, MASTER_LOG_POS = %d%s",
			quoteString(logFile), logPos, s.forClause(channel))
	}
	return fmt.Sprintf("CHANGE REPLICATION SOURCE TO SOURCE_AUTO_POSITION = 0, SOURCE_LOG_FILE = %s, SOURCE_LOG_POS = %d%s",
		quoteString(logFile), logPos, s.forClause(channel))
}

func quoteString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return "'" + s + "'"
}
