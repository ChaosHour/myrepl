package repl

import (
	"context"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func TestCoordinatesValidate(t *testing.T) {
	cases := []struct {
		name    string
		coords  Coordinates
		wantErr bool
	}{
		{"gtid alone is valid", Coordinates{GTID: true}, false},
		{"gtid with log file is an error", Coordinates{GTID: true, LogFile: "binlog.000001"}, true},
		{"gtid with log pos is an error", Coordinates{GTID: true, LogPos: 4}, true},
		{"file mode with both is valid", Coordinates{LogFile: "binlog.000001", LogPos: 4}, false},
		{"file mode missing file is an error", Coordinates{LogPos: 4}, true},
		{"file mode missing pos is an error", Coordinates{LogFile: "binlog.000001"}, true},
		{"neither gtid nor file is an error", Coordinates{}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.coords.validate()
			if (err != nil) != c.wantErr {
				t.Fatalf("validate() error = %v, wantErr %v", err, c.wantErr)
			}
		})
	}
}

// expectStatusQuery sets up the SHOW REPLICA STATUS call BuildPlan always
// issues first, returning a healthy-or-not single row.
func expectStatusQuery(mock sqlmock.Sqlmock, healthy bool, logFile string, execPos uint64) {
	ioRunning, sqlRunning := "Yes", "Yes"
	if !healthy {
		ioRunning, sqlRunning = "No", "No"
	}
	cols := []string{"Replica_IO_Running", "Replica_SQL_Running", "Source_Log_File", "Exec_Source_Log_Pos", "Auto_Position"}
	mock.ExpectQuery("SHOW REPLICA STATUS").
		WillReturnRows(sqlmock.NewRows(cols).AddRow(ioRunning, sqlRunning, logFile, execPos, "0"))
}

func TestBuildPlanRefusesHealthyReplicaWithoutForce(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	expectStatusQuery(mock, true, "binlog.000001", 100)

	_, err = BuildPlan(context.Background(), db, Syntax{}, "", Coordinates{GTID: true}, nil, false)
	if err == nil {
		t.Fatal("expected an error for a healthy replica without --force")
	}
}

func TestBuildPlanAllowsHealthyReplicaWithForce(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	expectStatusQuery(mock, true, "binlog.000001", 100)
	mock.ExpectQuery("SELECT @@GLOBAL.gtid_mode").
		WillReturnRows(sqlmock.NewRows([]string{"@@GLOBAL.gtid_mode"}).AddRow("ON"))

	plan, err := BuildPlan(context.Background(), db, Syntax{}, "", Coordinates{GTID: true}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Warnings) == 0 {
		t.Fatal("expected a warning noting the override")
	}
	if len(plan.Statements) != 3 {
		t.Fatalf("got %d statements, want STOP/CHANGE/START", len(plan.Statements))
	}
}

func TestBuildPlanGTIDRequiresGtidModeOn(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	expectStatusQuery(mock, false, "binlog.000001", 100)
	mock.ExpectQuery("SELECT @@GLOBAL.gtid_mode").
		WillReturnRows(sqlmock.NewRows([]string{"@@GLOBAL.gtid_mode"}).AddRow("OFF"))

	_, err = BuildPlan(context.Background(), db, Syntax{}, "", Coordinates{GTID: true}, nil, false)
	if err == nil {
		t.Fatal("expected an error when gtid_mode is not ON")
	}
}

func TestBuildPlanRefusesBackwardPositionWithoutForce(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	expectStatusQuery(mock, false, "binlog.000061", 277)

	coords := Coordinates{LogFile: "binlog.000061", LogPos: 4}
	_, err = BuildPlan(context.Background(), db, Syntax{}, "", coords, nil, false)
	if err == nil {
		t.Fatal("expected an error for a backward position without --force")
	}
}

func TestBuildPlanAllowsForwardPositionSameFile(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	expectStatusQuery(mock, false, "binlog.000061", 277)

	coords := Coordinates{LogFile: "binlog.000061", LogPos: 500}
	plan, err := BuildPlan(context.Background(), db, Syntax{}, "", coords, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Warnings) != 1 {
		t.Fatalf("expected exactly one warning (no source given), got %v", plan.Warnings)
	}
}

func TestBuildPlanValidatesAgainstSourceBinaryLogs(t *testing.T) {
	replicaDB, replicaMock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer replicaDB.Close()
	expectStatusQuery(replicaMock, false, "binlog.000061", 100)

	sourceDB, sourceMock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sourceDB.Close()
	sourceMock.ExpectQuery("SHOW BINARY LOGS").
		WillReturnRows(sqlmock.NewRows([]string{"Log_name", "File_size"}).
			AddRow("binlog.000060", 5000).
			AddRow("binlog.000061", 277))

	coords := Coordinates{LogFile: "binlog.000061", LogPos: 99999}
	_, err = BuildPlan(context.Background(), replicaDB, Syntax{}, "", coords, sourceDB, false)
	if err == nil {
		t.Fatal("expected an error for a position past the end of the source's binlog")
	}
}

func TestBuildPlanSourceFileNotFound(t *testing.T) {
	replicaDB, replicaMock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer replicaDB.Close()
	expectStatusQuery(replicaMock, false, "binlog.000061", 100)

	sourceDB, sourceMock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sourceDB.Close()
	sourceMock.ExpectQuery("SHOW BINARY LOGS").
		WillReturnRows(sqlmock.NewRows([]string{"Log_name", "File_size"}).AddRow("binlog.000060", 5000))

	coords := Coordinates{LogFile: "binlog.999999", LogPos: 100}
	_, err = BuildPlan(context.Background(), replicaDB, Syntax{}, "", coords, sourceDB, false)
	if err == nil {
		t.Fatal("expected an error for a log file the source doesn't have")
	}
}
