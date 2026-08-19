// Package myconf reads MySQL option files (my.cnf), mirroring how the mysql
// command-line client resolves connection settings via --defaults-file and
// --defaults-group-suffix.
package myconf

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Options holds the connection settings read from an option file.
type Options struct {
	User     string
	Password string
	Host     string
	Port     int
	Socket   string
}

// Load reads file (default ~/.my.cnf) and merges the [client] section with
// [client<groupSuffix>], the same precedence the mysql client applies for
// --defaults-group-suffix: values in the suffixed group win.
func Load(file, groupSuffix string) (*Options, error) {
	path, err := resolvePath(file)
	if err != nil {
		return nil, err
	}

	sections, err := parse(path)
	if err != nil {
		return nil, err
	}

	opts := &Options{}
	apply(opts, sections["client"])
	if groupSuffix != "" {
		apply(opts, sections["client"+groupSuffix])
	}
	return opts, nil
}

func resolvePath(file string) (string, error) {
	if file == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("determine home directory: %w", err)
		}
		return filepath.Join(home, ".my.cnf"), nil
	}
	if strings.HasPrefix(file, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(file, "~")), nil
		}
	}
	return file, nil
}

func parse(path string) (map[string]map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open MySQL option file %s: %w", path, err)
	}
	defer f.Close()

	sections := map[string]map[string]string{}
	section := ""
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			if _, ok := sections[section]; !ok {
				sections[section] = map[string]string{}
			}
			continue
		}
		if section == "" {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue // bare flags like "loose-skip-grant-tables" carry no value
		}
		sections[section][strings.TrimSpace(key)] = unquote(strings.TrimSpace(val))
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read MySQL option file %s: %w", path, err)
	}
	return sections, nil
}

func unquote(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

func apply(opts *Options, section map[string]string) {
	if section == nil {
		return
	}
	if v, ok := section["user"]; ok {
		opts.User = v
	}
	if v, ok := section["password"]; ok {
		opts.Password = v
	}
	if v, ok := section["host"]; ok {
		opts.Host = v
	}
	if v, ok := section["port"]; ok {
		if p, err := strconv.Atoi(v); err == nil {
			opts.Port = p
		}
	}
	if v, ok := section["socket"]; ok {
		opts.Socket = v
	}
}
