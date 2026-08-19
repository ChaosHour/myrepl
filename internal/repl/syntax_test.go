package repl

import (
	"context"
	"database/sql"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func TestSyntaxStatementsNewVocabulary(t *testing.T) {
	syn := Syntax{Legacy: false}

	cases := []struct {
		name string
		got  string
		want string
	}{
		{"status no channel", syn.StatusStmt(""), "SHOW REPLICA STATUS"},
		{"status with channel", syn.StatusStmt("chan1"), "SHOW REPLICA STATUS FOR CHANNEL 'chan1'"},
		{"stop", syn.StopStmt("chan1"), "STOP REPLICA FOR CHANNEL 'chan1'"},
		{"start", syn.StartStmt("chan1"), "START REPLICA FOR CHANNEL 'chan1'"},
		{"change gtid", syn.ChangeToGTIDStmt("chan1"), "CHANGE REPLICATION SOURCE TO SOURCE_AUTO_POSITION = 1 FOR CHANNEL 'chan1'"},
		{"change file", syn.ChangeToFileStmt("chan1", "binlog.000061", 277),
			"CHANGE REPLICATION SOURCE TO SOURCE_AUTO_POSITION = 0, SOURCE_LOG_FILE = 'binlog.000061', SOURCE_LOG_POS = 277 FOR CHANNEL 'chan1'"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.got != c.want {
				t.Fatalf("got  %q\nwant %q", c.got, c.want)
			}
		})
	}
}

func TestSyntaxStatementsLegacyVocabulary(t *testing.T) {
	syn := Syntax{Legacy: true}

	cases := []struct {
		name string
		got  string
		want string
	}{
		{"status", syn.StatusStmt(""), "SHOW SLAVE STATUS"},
		{"stop", syn.StopStmt(""), "STOP SLAVE"},
		{"start", syn.StartStmt(""), "START SLAVE"},
		{"change gtid", syn.ChangeToGTIDStmt(""), "CHANGE MASTER TO MASTER_AUTO_POSITION = 1"},
		{"change file", syn.ChangeToFileStmt("", "binlog.000061", 277),
			"CHANGE MASTER TO MASTER_AUTO_POSITION = 0, MASTER_LOG_FILE = 'binlog.000061', MASTER_LOG_POS = 277"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.got != c.want {
				t.Fatalf("got  %q\nwant %q", c.got, c.want)
			}
		})
	}
}

func TestQuoteStringEscapesQuotesAndBackslashes(t *testing.T) {
	got := quoteString(`o'brien\log`)
	want := `'o\'brien\\log'`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDetectVersionFamilies(t *testing.T) {
	cases := []struct {
		version string
		legacy  bool
	}{
		{"5.7.44", true},
		{"8.0.22", true},  // REPLICA verbs exist but CHANGE REPLICATION SOURCE TO doesn't yet
		{"8.0.23", false}, // first version with the full new vocabulary
		{"8.0.43", false},
		{"8.4.0", false},
		{"9.1.0", false},
		{"10.11.6-MariaDB", true},
		{"not-a-version", true}, // unrecognized format falls back to the safe default
	}

	for _, c := range cases {
		t.Run(c.version, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()

			mock.ExpectQuery("SELECT VERSION\\(\\)").
				WillReturnRows(sqlmock.NewRows([]string{"VERSION()"}).AddRow(c.version))

			syn, err := Detect(context.Background(), db)
			if err != nil {
				t.Fatal(err)
			}
			if syn.Legacy != c.legacy {
				t.Fatalf("version %s: got Legacy=%v, want %v", c.version, syn.Legacy, c.legacy)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDetectQueryError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT VERSION\\(\\)").WillReturnError(sql.ErrConnDone)

	if _, err := Detect(context.Background(), db); err == nil {
		t.Fatal("expected an error")
	}
}
