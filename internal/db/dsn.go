// Package db resolves MySQL connections and opens them.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/ChaosHour/myrepl/internal/myconf"
	gomysql "github.com/go-sql-driver/mysql"
)

// ConnFlags is the resolved input for one MySQL target. Exactly one source
// wins, in this precedence: DSN, then an option file (DefaultsFile /
// GroupSuffix), then the environment variable.
type ConnFlags struct {
	DSN          string
	DefaultsFile string
	GroupSuffix  string
	EnvVar       string
}

// Given reports whether any of DSN, DefaultsFile, GroupSuffix, or the
// environment variable actually supplies a connection, without resolving it.
func (f ConnFlags) Given() bool {
	return f.DSN != "" || f.DefaultsFile != "" || f.GroupSuffix != "" || os.Getenv(f.EnvVar) != ""
}

// ResolveDSN builds a go-sql-driver/mysql DSN from a ConnFlags.
func ResolveDSN(f ConnFlags) (string, error) {
	if f.DSN != "" {
		return f.DSN, nil
	}
	if f.DefaultsFile != "" || f.GroupSuffix != "" {
		opts, err := myconf.Load(f.DefaultsFile, f.GroupSuffix)
		if err != nil {
			return "", err
		}
		return buildDSN(opts), nil
	}
	if v := os.Getenv(f.EnvVar); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("no MySQL connection given: pass --dsn, --defaults-group-suffix (with an optional --defaults-file), or set $%s", f.EnvVar)
}

func buildDSN(o *myconf.Options) string {
	cfg := gomysql.NewConfig()
	cfg.User = o.User
	if cfg.User == "" {
		cfg.User = "root"
	}
	cfg.Passwd = o.Password
	if o.Socket != "" {
		cfg.Net = "unix"
		cfg.Addr = o.Socket
	} else {
		host := o.Host
		if host == "" {
			host = "127.0.0.1"
		}
		port := o.Port
		if port == 0 {
			port = 3306
		}
		cfg.Net = "tcp"
		cfg.Addr = fmt.Sprintf("%s:%d", host, port)
	}
	cfg.ParseTime = true
	cfg.Loc = time.Local
	cfg.AllowNativePasswords = true
	return cfg.FormatDSN()
}

// Open connects to MySQL and verifies the connection.
func Open(ctx context.Context, dsn string) (*sql.DB, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(5)
	db.SetConnMaxLifetime(time.Hour)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect to MySQL: %w", err)
	}
	return db, nil
}
