package cmd

import (
	"os"
	"testing"
)

func TestConnFlagsGivenAndResolve(t *testing.T) {
	const envVar = "MYREPL_CMD_TEST_DSN"
	os.Unsetenv(envVar)

	var f connFlags
	if f.given(envVar) {
		t.Fatal("expected given()==false with nothing set")
	}
	if _, err := f.resolve(envVar); err == nil {
		t.Fatal("expected resolve() to error with nothing set")
	}

	f.dsn = "root@tcp(127.0.0.1:3306)/"
	if !f.given(envVar) {
		t.Fatal("expected given()==true once --dsn is set")
	}
	got, err := f.resolve(envVar)
	if err != nil {
		t.Fatal(err)
	}
	if got != f.dsn {
		t.Fatalf("got %q, want %q", got, f.dsn)
	}
}
