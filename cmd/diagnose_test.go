package cmd

import "testing"

func TestHostPort(t *testing.T) {
	cases := []struct {
		dsn      string
		wantHost string
		wantPort string
	}{
		{"root:pw@tcp(127.0.0.1:3307)/", "127.0.0.1", "3307"},
		{"root@tcp(10.0.0.5:3306)/mydb", "10.0.0.5", "3306"},
		{"root@unix(/tmp/mysql.sock)/", "source", "3306"}, // non-tcp DSNs fall back to the label
		{"not a dsn at all", "source", "3306"},
	}
	for _, c := range cases {
		t.Run(c.dsn, func(t *testing.T) {
			host, port := hostPort(c.dsn, "source")
			if host != c.wantHost || port != c.wantPort {
				t.Fatalf("hostPort(%q) = (%q, %q), want (%q, %q)", c.dsn, host, port, c.wantHost, c.wantPort)
			}
		})
	}
}
