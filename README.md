# myrepl

A MySQL replication recovery CLI. Three commands:

- **`status`** — read `SHOW REPLICA/SLAVE STATUS` as a clear, parsed summary
- **`configure`** — safely repoint a broken replica at new log coordinates
  (GTID auto-position or an explicit binlog file+position)
- **`diagnose`** — compare GTID sets between a source and replica, find
  errant/missing transactions, and pinpoint exactly what a stuck SQL thread
  choked on

It auto-detects, so you don't have to remember which MySQL version wants
which words:

- **Command vocabulary** — MySQL renamed `SHOW SLAVE STATUS` →
  `SHOW REPLICA STATUS` and `CHANGE MASTER TO` → `CHANGE REPLICATION SOURCE
  TO` across 8.0.22–23. `myrepl` probes `@@version` and uses whichever
  wording the server understands (MariaDB and pre-8.0.22 MySQL get the
  legacy verbs).
- **Coordinate mode** — GTID auto-position vs. explicit binlog
  file+position, based on `Auto_Position` in the current status.

## Build

```console
$ make build          # -> ./bin/myrepl
$ make install         # go install into $GOBIN
```

## Connecting

All three commands accept the same trio of flags for the replica, mirroring
how the `mysql` CLI itself connects. `configure` accepts a `source-`-prefixed
set for an *optional* source connection (used to validate requested
coordinates); `diagnose` requires it, since it always compares two servers.

| Flag | Env var | Notes |
|---|---|---|
| `--dsn` | `$MYREPL_DSN` | a full DSN: `user:pass@tcp(host:port)/` |
| `--defaults-file` | | MySQL option file to read (default `~/.my.cnf`) |
| `--defaults-group-suffix` | | e.g. `_replica1` reads `[client]` overlaid with `[client_replica1]`, same as `mysql --defaults-group-suffix` |

Precedence: `--dsn` > `--defaults-file`/`--defaults-group-suffix` > the env
var. For the source side, swap in `--source-dsn`, `--source-defaults-file`,
`--source-defaults-group-suffix`, `$MYREPL_SOURCE_DSN`.

Given a `~/.my.cnf` like:

```ini
[client]
user=root

[client_primary1]
host=127.0.0.1
port=3306

[client_replica1]
host=127.0.0.1
port=3307
```

you can drive every command with just `--defaults-group-suffix`, exactly as
you would with the `mysql` client.

## `myrepl status`

```console
$ myrepl status --defaults-group-suffix=_replica1
channel:           source_3
mode:              GTID
source:            172.20.0.3:3306
io thread:         Yes
sql thread:        Yes
seconds behind:    0
source log file:   binlog.000061
read pos:          277
executed pos:      277
retrieved gtids:   1d1fff5a-c9bc-11ed-9c19-02a36d996b94:123,
2ac8ec13-9255-11f0-8705-6238a95b9967:1-42628
executed gtids:    1d1fff5a-c9bc-11ed-9c19-02a36d996b94:123,
2ac8ec13-9255-11f0-8705-6238a95b9967:1-42628
healthy:           true
```

Multi-source replica? Point it at a channel:

```console
$ myrepl status --defaults-group-suffix=_replica1 --channel source_3
```

## `myrepl configure`

```console
$ myrepl configure --defaults-group-suffix=_replica1 --channel source_3 \
    --source-defaults-group-suffix=_primary1 \
    --log-file binlog.000061 --log-pos 277 --dry-run
current state:
channel:           source_3
mode:              GTID
...
healthy:           true

plan:
  STOP REPLICA FOR CHANNEL 'source_3';
  CHANGE REPLICATION SOURCE TO SOURCE_AUTO_POSITION = 0, SOURCE_LOG_FILE = 'binlog.000061', SOURCE_LOG_POS = 277 FOR CHANNEL 'source_3';
  START REPLICA FOR CHANNEL 'source_3';

--dry-run: nothing executed
```

Drop `--dry-run` and it prompts before running the plan:

```console
$ myrepl configure --defaults-group-suffix=_replica1 --channel source_3 \
    --gtid --force
...
run this plan? [y/N] y

resulting state:
channel:           source_3
mode:              GTID
io thread:         Yes
sql thread:        Yes
healthy:           true
```

`--yes` skips the prompt (for scripting); `--force` overrides the validation
checks below (each one prints as a warning instead of aborting).

**What it checks, all overridable with `--force`:**

- GTID mode requires `gtid_mode=ON` on the replica.
- File-position mode, when a source connection is given, checks the
  requested file exists and the position is within it via
  `SHOW BINARY LOGS` on the source.
- Refuses to move the position backward within the same file — that would
  replay already-applied events.
- Refuses to repoint a replica that currently looks healthy (both threads
  running, no error). `myrepl configure` is for recovery, not routine
  reconfiguration.

Switching *to* file-position mode always clears `SOURCE_AUTO_POSITION`/
`MASTER_AUTO_POSITION` in the same statement, even if the channel wasn't
using GTID — MySQL rejects a bare `SOURCE_LOG_FILE`/`SOURCE_LOG_POS` change
while auto-position is still active (error 1776).

## `myrepl diagnose`

```console
$ myrepl diagnose --defaults-group-suffix=_replica1 \
    --source-defaults-group-suffix=_primary1
[+] Source -> 127.0.0.1 gtid_executed: 2ac8ec13-...:1-42628
[+] server_uuid: 2ac8ec13-9255-11f0-8705-6238a95b9967
[+] Target -> 127.0.0.1 gtid_executed: 2ac8ec13-...:1-42628,2af7e535-...:1-120061
[+] server_uuid: 2af7e535-9255-11f0-87f8-76ae10baffb1
[-] Errant Transactions: 2af7e535-9255-11f0-87f8-76ae10baffb1:1-120061
[-] Errant Transaction Found in Log Name: binlog.000006
```

Add `--stuck` to also check whether the replica's SQL thread is currently
stopped and, if so, pinpoint the exact GTID it choked on:

```console
$ myrepl diagnose --defaults-group-suffix=_replica1 \
    --source-defaults-group-suffix=_primary1 --stuck

Diagnosis:
[-] Replication is not running on 127.0.0.1 (IO=Yes SQL=No)
[i] Stuck at transaction: 2ac8ec13-9255-11f0-8705-6238a95b9967:42490 (errno 1062)
[i] Source has 2ac8ec13-9255-11f0-8705-6238a95b9967 through :42628 -> 139 outstanding transaction(s) blocking replication.

Suggested commands to see exactly what's blocking:
    mysql -h 127.0.0.1 -P 3307 -e "SELECT * FROM performance_schema.replication_applier_status_by_worker\G"
    mysqlbinlog -h 127.0.0.1 -P 3306 -r --include-gtids='2ac8ec13-9255-11f0-8705-6238a95b9967:42490-42628' binlog.000061 | grep -iE "^(CREATE|DROP|ALTER|USE)"
```

Once you know what's wrong, fix it in place — each mode is destructive
(injects empty transactions, and `--fix-replica` stops/starts replication),
so each prompts for confirmation unless you pass `--yes`, and every mode
supports `--dry-run`:

```console
# Preview applying the errant GTID to the replica (marks it as already-executed there):
$ myrepl diagnose --defaults-group-suffix=_replica1 \
    --source-defaults-group-suffix=_primary1 \
    --fix-replica --dry-run
[dry-run] Would execute on replica (single pinned session):
    STOP REPLICA;
    SET SESSION sql_log_bin = 0;
    SET GTID_NEXT='2af7e535-9255-11f0-87f8-76ae10baffb1:1'; BEGIN; COMMIT;
    ...
    SET SESSION sql_log_bin = 1;
    START REPLICA;
```

Exit codes: `0` = in sync (or a fix was applied and resolved everything),
`1` = a real error (can't connect, bad query, etc.), `2` = errant/missing
transactions remain unresolved — handy for cron/alerting.

`diagnose` delegates to
[github.com/ChaosHour/go-gtids](https://github.com/ChaosHour/go-gtids),
reusing myrepl's own connection resolution instead of go-gtids' standalone
host/port flags.

## Testing

```console
$ make test          # go test ./...
$ make test-race      # + race detector and coverage
$ make check          # fmt-check + vet + test, for CI
```

Tests use [`go-sqlmock`](https://github.com/DATA-DOG/go-sqlmock) to exercise
the SQL-touching code (version detection, status parsing, plan-building
validation) without a live server — no Docker required to run `make test`.
The safety checks in `configure` (GTID-mode requirement, backward-position
guard, healthy-replica guard, source binlog validation) are covered directly
against realistic `SHOW REPLICA/SLAVE STATUS` rows for both the old and new
MySQL column-name vocabularies.

## Scope

Targets MySQL 5.7/8.0/8.4. MariaDB gets the legacy `SLAVE`/`MASTER`
vocabulary (which is correct for MariaDB), but GTID auto-detection is based
on MySQL's `gtid_mode`/`Auto_Position`, not MariaDB's `gtid_current_pos`/
`Using_Gtid` — MariaDB GTID replicas will be treated as file-position only.
