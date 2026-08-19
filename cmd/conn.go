package cmd

import (
	"fmt"

	"github.com/spf13/pflag"

	"github.com/ChaosHour/myrepl/internal/db"
)

// connFlags is the set of flags used to resolve one MySQL connection,
// mirroring how the mysql command-line client is invoked.
type connFlags struct {
	dsn          string
	defaultsFile string
	groupSuffix  string
}

func (f *connFlags) register(fs *pflag.FlagSet, prefix, label string) {
	fs.StringVar(&f.dsn, prefix+"dsn", "",
		fmt.Sprintf("full MySQL DSN for the %s (user:pass@tcp(host:port)/)", label))
	fs.StringVar(&f.defaultsFile, prefix+"defaults-file", "",
		fmt.Sprintf("MySQL option file to read %s connection settings from (default ~/.my.cnf)", label))
	fs.StringVar(&f.groupSuffix, prefix+"defaults-group-suffix", "",
		fmt.Sprintf("option file group suffix for the %s, e.g. _replica1 reads [client_replica1]", label))
}

func (f connFlags) toConnFlags(envVar string) db.ConnFlags {
	return db.ConnFlags{DSN: f.dsn, DefaultsFile: f.defaultsFile, GroupSuffix: f.groupSuffix, EnvVar: envVar}
}

func (f connFlags) given(envVar string) bool {
	return f.toConnFlags(envVar).Given()
}

func (f connFlags) resolve(envVar string) (string, error) {
	return db.ResolveDSN(f.toConnFlags(envVar))
}
