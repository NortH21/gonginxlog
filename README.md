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

`gonginxlog --version` prints the build's version/commit/date. `error:`
and `warning:` messages are colored (red/yellow) when stderr is a
terminal; set `NO_COLOR=1` to turn that off.

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
- If `--nginx-conf` is given but the named `log_format` isn't found in it
  (or anything it includes), gonginxlog fails with an error instead of
  silently falling back to the built-in default — pass the right
  `--format-name` for your config.

## Filters

| Flag | Meaning | Examples |
|---|---|---|
| `--since` | keep entries at/after this time | `--since 2026-08-12T10:00:00Z`, `--since "12/Aug/2026:10:00:00 +0000"` |
| `--until` | keep entries strictly before this time | `--until 2026-08-12T12:00:00Z` |
| `--last` | keep entries from the last duration (overrides `--since`) | `--last 1h30m` |
| `--status` | status filter: exact / class / range, comma-combinable | `--status 404,500-599,3xx` |
| `--ip` | client IP filter: exact / CIDR, comma-combinable | `--ip 203.0.113.5,10.0.0.0/8` |
| `--country` | country code filter, comma-combinable (see below) | `--country RU,US` |
| `--grep` | regexp against the whole raw line | `--grep 'wp-admin'` |
| `--field` | regexp against one named field, repeatable (ANDed) | `--field http_user_agent=bot` `--field host=example\.com` |

Field names for `--field` are nginx variable names (`remote_addr`,
`status`, `request`, `http_user_agent`, `http_referer`, `host`, ...), the
same names the report and filters use internally.

`--since`/`--until` accept RFC3339 or nginx's own `time_local` layout
(`02/Jan/2006:15:04:05 -0700`).

`--country` matches `$geoip_country_code` (legacy `ngx_http_geoip_module`)
or `$geoip2_data_country_code` (`ngx_http_geoip2_module`), whichever your
log_format has. It only works — and the report's "Top countries" table
only appears — if the log_format carries one of those fields.

## Output

By default gonginxlog prints an aggregated report:

- status code distribution
- top client IPs, countries, requested paths, user agents, referers
  (`--top N` rows each, default 10; the countries table only shows up
  when the log_format has `$geoip_country_code` or
  `$geoip2_data_country_code` — requests with no resolved country are
  counted under `-`)
- request time / upstream response time percentiles (avg, p50, p90, p99, max)
- total bytes sent
- a requests-over-time histogram (`--bucket`; default `auto` picks a
  bucket size — 1m/5m/15m/30m/1h/3h/6h/12h/24h — from the data's actual
  time span so it stays readable; pass e.g. `--bucket 30m` to force one,
  or `--bucket 0` to disable it)

Flags controlling what's printed:

```sh
gonginxlog access.log                       # report only (default)
gonginxlog --lines access.log               # report + matching raw lines
gonginxlog --lines --report=false access.log  # matching raw lines only, like grep
gonginxlog --json access.log                # report as JSON instead of text tables
```

## Docker

A small (~4MB) static image is published on every push to `main` and on
every version tag, to both registries:

```sh
docker pull ghcr.io/north21/gonginxlog:latest
docker pull <dockerhub-user>/gonginxlog:latest
```

Run it against a log on your host by mounting it in:

```sh
docker run --rm -v /var/log/nginx:/logs:ro \
  ghcr.io/north21/gonginxlog:latest --status 5xx /logs/access.log
```

Tags: `latest` (tracks `main`), `vX.Y.Z` / `X.Y` (from release tags), and
`sha-<short-sha>`.

## Releases

Every `vX.Y.Z` tag also builds standalone binaries (linux/darwin/windows,
amd64/arm64) and publishes them as a GitHub Release, with a
`checksums.txt`. Grab one from the repo's Releases page and run it — no
Go toolchain or Docker needed.

## Roadmap

Not implemented yet, planned once there's an interactive (TUI) mode:

- Anomaly detection: a single IP generating an outsized share of
  requests, many distinct IPs hammering one endpoint, or one IP
  enumerating many distinct URLs (scanning).
- Live-updating stats while following a file with `-f`.

See `DESIGN.md` for the fuller design rationale and package layout.
