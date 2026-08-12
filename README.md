# gonginxlog

A CLI tool that parses nginx access logs the way they're actually
configured — by reading the real `log_format` directive (from an
`nginx.conf`, or a built-in default) instead of assuming a fixed layout.
Filter by time, status, client IP, or arbitrary regex, and get a
goaccess-style aggregated report.

## Build

```sh
go build -o gonginxlog .
```

(or just `go run . <flags> <logfile>` while developing)

## Quick start

```sh
# default main_json (escape=json) format, full report
./gonginxlog access.log

# only 5xx from one IP in the last hour, as raw matching lines
./gonginxlog --ip 203.0.113.5 --status 5xx --last 1h --lines --report=false access.log

# read the log_format from a real nginx.conf (include directives are followed)
./gonginxlog --nginx-conf /etc/nginx/nginx.conf --format-name main_json access.log

# tail a live file
./gonginxlog -f /var/log/nginx/access.log
```

Flags can be given before or after the file paths — `gonginxlog
access.log --top 5` and `gonginxlog --top 5 access.log` both work.

## Input

Positional arguments are one or more log files, read and concatenated in
order:

```sh
gonginxlog access.log access.log.1 access.log.2.gz   # multiple files, gzip transparently decompressed
cat access.log | gonginxlog -                          # "-" reads stdin
gonginxlog -f access.log                                # follow mode (tail -f)
```

`-f`/`--follow` takes exactly one real file path (not `-`, not multiple
files) and streams newly appended matching lines. It reopens the file if
it shrinks (log rotation via `copytruncate` or rename+recreate). Aggregated
stats are not recomputed live in this mode — see [Roadmap](#roadmap).

## Choosing the log_format

By default gonginxlog uses a built-in format equivalent to:

```
log_format main_json escape=json '{'
        '"remote_addr":"$remote_addr", ... '"x-request-id":"$request_id"'
'}';
```

To use your actual nginx config instead:

```sh
gonginxlog --nginx-conf /etc/nginx/nginx.conf --format-name main_json access.log
```

- `--nginx-conf` is read along with everything it `include`s (globs and
  all, recursively) — so a `log_format` defined in `conf.d/*.conf` or
  `sites-enabled/*` is found too.
- `--format-name` picks which `log_format <name> ...;` directive to use
  when a conf defines more than one.
- Both `escape=json` templates (parsed as real JSON, key order doesn't
  matter) and classic non-JSON templates (`combined`-style, compiled into
  a generated regexp from the `$variable` tokens) are supported.
- Fields are matched internally by nginx **variable** name, not by
  whatever JSON key you chose — `"x-request-id":"$request_id"` is still
  understood as `request_id`. This is what lets filters and the report
  work regardless of custom JSON key naming.

## Filters

| Flag | Meaning | Examples |
|---|---|---|
| `--since` | keep entries at/after this time | `--since 2026-08-12T10:00:00Z`, `--since "12/Aug/2026:10:00:00 +0000"` |
| `--until` | keep entries strictly before this time | `--until 2026-08-12T12:00:00Z` |
| `--last` | keep entries from the last duration (overrides `--since`) | `--last 1h30m` |
| `--status` | status filter: exact / class / range, comma-combinable | `--status 404,500-599,3xx` |
| `--ip` | client IP filter: exact / CIDR, comma-combinable | `--ip 203.0.113.5,10.0.0.0/8` |
| `--grep` | regexp against the whole raw line | `--grep 'wp-admin'` |
| `--field` | regexp against one named field, repeatable (ANDed) | `--field http_user_agent=bot` `--field host=example\.com` |

Field names for `--field` are nginx variable names (`remote_addr`,
`status`, `request`, `http_user_agent`, `http_referer`, `host`, ...), the
same names the report and filters use internally.

`--since`/`--until` accept RFC3339 or nginx's own `time_local` layout
(`02/Jan/2006:15:04:05 -0700`).

## Output

By default gonginxlog prints an aggregated report:

- status code distribution
- top client IPs, requested paths, user agents, referers (`--top N` rows each, default 10)
- request time / upstream response time percentiles (avg, p50, p90, p99, max)
- total bytes sent
- a requests-over-time histogram (`--bucket`, default `5m`; `--bucket 0` disables it)

Flags controlling what's printed:

```sh
gonginxlog access.log                       # report only (default)
gonginxlog --lines access.log               # report + matching raw lines
gonginxlog --lines --report=false access.log  # matching raw lines only, like grep
gonginxlog --json access.log                # report as JSON instead of text tables
```

## Roadmap

Not implemented yet, planned once there's an interactive (TUI) mode:

- Anomaly detection: a single IP generating an outsized share of
  requests, many distinct IPs hammering one endpoint, or one IP
  enumerating many distinct URLs (scanning).
- Live-updating stats while following a file with `-f`.

See `DESIGN.md` for the fuller design rationale and package layout.
