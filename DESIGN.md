# gonginxlog — design notes

## Purpose

CLI tool (Go) that parses nginx access logs the way they are actually
configured, not with a hardcoded regex. It reads the `log_format`
directive from an nginx config (following `include`), builds a parser
from it, then lets you filter and aggregate — something in spirit of
`goaccess`, but scriptable and log_format-aware.

Default format if no config is given (`main_json`, `escape=json`):

```
log_format main_json escape=json '{'
        '"remote_addr":"$remote_addr",'
        '"country":"$geoip_country_code",'
        '"remote_user":"$remote_user",'
        '"time_local":"$time_local",'
        '"ssl_protocol":"$ssl_protocol",'
        '"host":"$host",'
        '"request":"$request",'
        '"status":"$status",'
        '"bytes_sent":"$bytes_sent",'
        '"http_referer":"$http_referer",'
        '"http_user_agent":"$http_user_agent",'
        '"http_cookie":"$http_cookie",'
        '"http_x_forwarded_for":"$http_x_forwarded_for",'
        '"request_time":"$request_time",'
        '"request_length":"$request_length",'
        '"upstream_addr":"$upstream_addr",'
        '"upstream_response_time":"$upstream_response_time",'
        '"upstream_status":"$upstream_status",'
        '"upstream_cache_status":"$upstream_cache_status",'
        '"x-request-id":"$request_id"'
'}';
```

## Decisions made (via Q&A on 2026-08-12)

- **MVP scope**: CLI filters + aggregated text report. No TUI yet —
  planned as a later phase (see "Deferred").
- **log_format source**: `--nginx-conf <path> --format-name <name>`.
  If not given, falls back to the built-in `main_json` default above.
  `include` directives in the conf are followed recursively (globs
  resolved, like `nginx -T` would see them), so `log_format` defined in
  `conf.d/*.conf` etc. is found too.
- **Format support**: both `escape=json` formats (parsed as real JSON
  per line — key order doesn't matter) *and* classic non-JSON formats
  (`combined`-style) via generating a regex with named capture groups
  from the `$variable` tokens in the format string.
- **Semantic field mapping**: internally everything is keyed by nginx
  variable name (`remote_addr`, `status`, `request_time`, ...), not by
  whatever JSON key the user chose (e.g. `"x-request-id":"$request_id"`
  still resolves to variable `request_id`). This is what lets filters
  and stats work regardless of custom JSON key names.
- **Time filter**: both absolute (`--since`/`--until`, RFC3339 or nginx
  `time_local` layout) and relative (`--last 1h`).
- **Status filter**: exact codes, classes (`5xx`), and ranges
  (`500-599`), comma-combinable.
- **IP filter**: exact/list and CIDR subnets, comma-combinable.
- **Grep**: whole raw line (`--grep`) and per-field (`--field
  key=regex`, repeatable, ANDed).
- **Aggregates always shown**: top client IPs, top requested
  paths, status code distribution.
- **Aggregates additionally requested**: request/upstream timing
  percentiles (p50/p90/p99/avg/max), top User-Agent/Referer, and a
  requests-over-time histogram (ASCII bar chart, bucket size
  configurable via `--bucket`).
- **Output**: text tables by default; `--json` for machine-readable
  export.
- **Input sources**: multiple files as positional args, `.gz` rotated
  logs read transparently, `-` for stdin, and `-f`/`--follow` for a
  live-tail mode (streams matching lines; aggregated stats are *not*
  recomputed live in this phase — that's TUI territory).
- **Repo**: git-initialized here; module path
  `github.com/north21/gonginxlog`.

## Deferred (explicitly, not forgotten)

Anomaly detection, to be added once there's a TUI/GUI:
- One IP generating an unusually large share of requests (flood/abuse
  from a single address).
- Many distinct addresses hitting a single endpoint in a short window
  (distributed hammering / credential stuffing on one route).
- One address enumerating many distinct URLs in a short window
  (path/endpoint scanning, e.g. looking for admin panels).

The stats aggregation code should stay easy to extend with this later
(e.g. per-IP path-diversity counters, per-path IP-diversity counters,
sliding time windows) without a redesign — but no anomaly-detection
code is stubbed in yet, since the shape of the future TUI will drive
how it's surfaced.

## Package layout

```
main.go                    CLI entry point, flag parsing, wiring
internal/format             log_format template -> Spec (JSON pairs or
                             literal/variable tokens), default spec
internal/nginxconf           reads nginx.conf, follows include globs,
                             extracts a named log_format's raw template
internal/parser              Spec -> Parser (JSON decode or generated
                             regex), Parser.Parse(line) -> *record.Record
internal/record               canonical accessors over the parsed field
                             map (Time, StatusCode, Method, Path, ...)
internal/filter               Filter interface + time/status/ip/grep/
                             field filters, ANDed together
internal/stats                Aggregator (top-N counters, status dist,
                             timing percentiles, histogram) -> Report
internal/input                 multi-file/gzip/stdin sources, follow
                             (tail -f) mode
internal/output                text-table and JSON rendering of Report
```
