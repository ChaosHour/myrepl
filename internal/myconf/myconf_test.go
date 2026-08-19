package myconf

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCnf(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".my.cnf")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadBaseClientSection(t *testing.T) {
	path := writeCnf(t, `
[client]
user=root
password=secret
host=10.0.0.1
port=3306
`)
	opts, err := Load(path, "")
	if err != nil {
		t.Fatal(err)
	}
	want := &Options{User: "root", Password: "secret", Host: "10.0.0.1", Port: 3306}
	if *opts != *want {
		t.Fatalf("got %+v, want %+v", *opts, *want)
	}
}

func TestLoadGroupSuffixOverridesClient(t *testing.T) {
	path := writeCnf(t, `
[client]
user=root
host=127.0.0.1
port=3306

[client_replica1]
host=127.0.0.1
port=3307
`)
	opts, err := Load(path, "_replica1")
	if err != nil {
		t.Fatal(err)
	}
	// user comes from [client] (not overridden); host/port come from the suffixed group.
	if opts.User != "root" || opts.Host != "127.0.0.1" || opts.Port != 3307 {
		t.Fatalf("got %+v", *opts)
	}
}

func TestLoadIgnoresUnrelatedGroups(t *testing.T) {
	path := writeCnf(t, `
[client]
user=root

[client_primary1]
user=someone-else
port=9999

[mysqld]
port=1111
`)
	opts, err := Load(path, "_replica1")
	if err != nil {
		t.Fatal(err)
	}
	if opts.User != "root" || opts.Port != 0 {
		t.Fatalf("group isolation leaked: got %+v", *opts)
	}
}

func TestLoadQuotedAndCommentedValues(t *testing.T) {
	path := writeCnf(t, `
# a comment
; also a comment
[client]
user = "quoted-user"
password = 'quoted-pass'
loose-skip-grant-tables
`)
	opts, err := Load(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if opts.User != "quoted-user" || opts.Password != "quoted-pass" {
		t.Fatalf("got %+v", *opts)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.cnf"), "")
	if err == nil {
		t.Fatal("expected an error for a missing option file")
	}
}

func TestLoadSocketOverridesHost(t *testing.T) {
	path := writeCnf(t, `
[client]
socket=/tmp/mysql.sock
host=127.0.0.1
`)
	opts, err := Load(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if opts.Socket != "/tmp/mysql.sock" {
		t.Fatalf("got %+v", *opts)
	}
}
