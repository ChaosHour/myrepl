package repl

import (
	"context"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func TestParseRowNewVocabularyGTIDMode(t *testing.T) {
	raw := map[string]string{
		"Channel_Name":          "source_3",
		"Source_Host":           "172.20.0.3",
		"Source_Port":           "3306",
		"Replica_IO_Running":    "Yes",
		"Replica_SQL_Running":   "Yes",
		"Last_IO_Errno":         "0",
		"Last_SQL_Errno":        "0",
		"Source_Log_File":       "binlog.000061",
		"Read_Source_Log_Pos":   "277",
		"Exec_Source_Log_Pos":   "277",
		"Seconds_Behind_Source": "0",
		"Retrieved_Gtid_Set":    "uuid1:1-5",
		"Executed_Gtid_Set":     "uuid1:1-5",
		"Auto_Position":         "1",
	}
	s := parseRow(raw)

	if s.Channel != "source_3" || s.SourceHost != "172.20.0.3" || s.SourcePort != 3306 {
		t.Fatalf("identity fields wrong: %+v", s)
	}
	if s.Mode() != ModeGTID {
		t.Fatalf("got mode %v, want GTID", s.Mode())
	}
	if !s.Healthy() {
		t.Fatal("expected healthy")
	}
	if !s.SecondsBehind.Valid || s.SecondsBehind.Int64 != 0 {
		t.Fatalf("got SecondsBehind %+v", s.SecondsBehind)
	}
}

func TestParseRowLegacyVocabularyFileMode(t *testing.T) {
	raw := map[string]string{
		"Master_Host":           "10.0.0.1",
		"Master_Port":           "3306",
		"Slave_IO_Running":      "No",
		"Slave_SQL_Running":     "No",
		"Last_IO_Errno":         "2003",
		"Last_IO_Error":         "error connecting to master",
		"Last_SQL_Errno":        "0",
		"Master_Log_File":       "binlog.000005",
		"Read_Master_Log_Pos":   "1024",
		"Exec_Master_Log_Pos":   "512",
		"Seconds_Behind_Master": "", // a real SQL NULL scans to sql.NullString{Valid:false} -> ""
	}

	s := parseRow(raw)

	if s.SourceHost != "10.0.0.1" || s.SourcePort != 3306 {
		t.Fatalf("legacy Master_* fields not picked up: %+v", s)
	}
	if s.Mode() != ModeFile {
		t.Fatalf("got mode %v, want file position", s.Mode())
	}
	if s.Healthy() {
		t.Fatal("expected unhealthy (threads stopped, IO error set)")
	}
	if s.SecondsBehind.Valid {
		t.Fatalf("expected SecondsBehind to be NULL/invalid, got %+v", s.SecondsBehind)
	}
	if s.LastIOErrno != 2003 || s.LastIOError != "error connecting to master" {
		t.Fatalf("IO error fields wrong: %+v", s)
	}
}

func TestParseRowUnknownMode(t *testing.T) {
	s := parseRow(map[string]string{})
	if s.Mode() != ModeUnknown {
		t.Fatalf("got mode %v, want unknown for an empty row", s.Mode())
	}
}

func TestModeString(t *testing.T) {
	cases := map[Mode]string{ModeGTID: "GTID", ModeFile: "file position", ModeUnknown: "unknown"}
	for mode, want := range cases {
		if got := mode.String(); got != want {
			t.Fatalf("mode %d: got %q, want %q", mode, got, want)
		}
	}
}

func TestFetchNewVocabulary(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	cols := []string{"Replica_IO_Running", "Replica_SQL_Running", "Source_Log_File", "Auto_Position"}
	mock.ExpectQuery("SHOW REPLICA STATUS FOR CHANNEL 'chan1'").
		WillReturnRows(sqlmock.NewRows(cols).AddRow("Yes", "Yes", "binlog.000001", "1"))

	st, err := Fetch(context.Background(), db, Syntax{Legacy: false}, "chan1")
	if err != nil {
		t.Fatal(err)
	}
	if st == nil || st.Mode() != ModeGTID || !st.Healthy() {
		t.Fatalf("got %+v", st)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFetchNoRowsMeansNoReplication(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery("SHOW REPLICA STATUS").
		WillReturnRows(sqlmock.NewRows([]string{"Replica_IO_Running"}))

	st, err := Fetch(context.Background(), db, Syntax{Legacy: false}, "")
	if err != nil {
		t.Fatal(err)
	}
	if st != nil {
		t.Fatalf("expected nil Status for an empty result set, got %+v", st)
	}
}
