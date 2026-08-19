package db

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveDSNPrecedence(t *testing.T) {
	t.Run("explicit DSN wins over everything", func(t *testing.T) {
		t.Setenv("MYREPL_TEST_DSN", "envuser:envpass@tcp(1.2.3.4:3306)/")
		got, err := ResolveDSN(ConnFlags{DSN: "explicit:dsn@tcp(9.9.9.9:3306)/", EnvVar: "MYREPL_TEST_DSN"})
		if err != nil {
			t.Fatal(err)
		}
		if got != "explicit:dsn@tcp(9.9.9.9:3306)/" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("defaults file wins over env", func(t *testing.T) {
		t.Setenv("MYREPL_TEST_DSN", "envuser:envpass@tcp(1.2.3.4:3306)/")
		path := filepath.Join(t.TempDir(), ".my.cnf")
		if err := os.WriteFile(path, []byte("[client]\nuser=fileuser\nhost=5.6.7.8\nport=3307\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := ResolveDSN(ConnFlags{DefaultsFile: path, EnvVar: "MYREPL_TEST_DSN"})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(got, "fileuser@tcp(5.6.7.8:3307)/") {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("env var used when nothing else given", func(t *testing.T) {
		t.Setenv("MYREPL_TEST_DSN", "envuser:envpass@tcp(1.2.3.4:3306)/")
		got, err := ResolveDSN(ConnFlags{EnvVar: "MYREPL_TEST_DSN"})
		if err != nil {
			t.Fatal(err)
		}
		if got != "envuser:envpass@tcp(1.2.3.4:3306)/" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("nothing given is an error", func(t *testing.T) {
		os.Unsetenv("MYREPL_TEST_DSN_UNSET")
		if _, err := ResolveDSN(ConnFlags{EnvVar: "MYREPL_TEST_DSN_UNSET"}); err == nil {
			t.Fatal("expected an error")
		}
	})
}

func TestConnFlagsGiven(t *testing.T) {
	os.Unsetenv("MYREPL_TEST_GIVEN")
	if (ConnFlags{EnvVar: "MYREPL_TEST_GIVEN"}).Given() {
		t.Fatal("expected Given()==false with nothing set")
	}
	if !(ConnFlags{DSN: "x", EnvVar: "MYREPL_TEST_GIVEN"}).Given() {
		t.Fatal("expected Given()==true with DSN set")
	}
	t.Setenv("MYREPL_TEST_GIVEN", "x")
	if !(ConnFlags{EnvVar: "MYREPL_TEST_GIVEN"}).Given() {
		t.Fatal("expected Given()==true with env var set")
	}
}

func TestBuildDSNDefaults(t *testing.T) {
	// No user/host/port in the option file: buildDSN falls back to root@127.0.0.1:3306.
	path := filepath.Join(t.TempDir(), ".my.cnf")
	if err := os.WriteFile(path, []byte("[client]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveDSN(ConnFlags{DefaultsFile: path})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "root@tcp(127.0.0.1:3306)/") {
		t.Fatalf("got %q", got)
	}
}

func TestBuildDSNUsesSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".my.cnf")
	if err := os.WriteFile(path, []byte("[client]\nuser=root\nsocket=/tmp/mysql.sock\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveDSN(ConnFlags{DefaultsFile: path})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "@unix(/tmp/mysql.sock)/") {
		t.Fatalf("got %q, want a unix() DSN", got)
	}
}
