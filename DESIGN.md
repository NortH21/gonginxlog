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

## Maintenance rule

Whenever a change touches user-facing behavior (flags, output, supported
formats, roadmap) or the architecture, update **both** `README.md` (usage)
and this file (rationale/decisions/layout) in the same change. This was an
explicit user instruction, not a suggestion.

## Implementation notes worth remembering

- Go's stdlib `flag` package stops parsing at the first non-flag token,
  so `gonginxlog access.log --top 5` would otherwise silently swallow
  `--top 5` as extra file arguments. `main.go`'s `reorderArgs` splits
  `os.Args` into flag tokens and positional tokens *before* calling
  `flag.Parse`, so flags can be given before or after the file paths. A
  lone `-` is always treated as positional (the stdin marker), never as
  a flag.
- Validated end-to-end against a real production log (430MB / 208k
  lines, dropped in locally under `tests/`, real `main_json` format):
  all lines parsed with zero `log_format` mismatches,
  filters/report/`--json`/gzip/stdin/multi-file/`-f` all exercised. Real
  logs like this are large and not meant for git — see `.gitignore`
  (`/tests/*.log*`, `/tests/*.gz`).
- `--nginx-conf` is all-or-nothing by design: if the requested
  `--format-name` isn't found in that conf (or its includes), gonginxlog
  errors out rather than silently falling back to the built-in default —
  a silent fallback could report on the wrong fields without any
  indication something's off. Not passing `--nginx-conf` at all is the
  only way to get the built-in default; the real conf's contents are
  irrelevant in that case since it isn't read.
- `error:`/`warning:` output is colorized (red/yellow) via raw ANSI
  codes in `logging.go` — no dependency. Only active when stderr is a
  terminal (`isTerminal`, checked via `os.ModeCharDevice`) and `NO_COLOR`
  isn't set.
- The requests-over-time histogram bucket size defaults to `auto`
  (`stats.AutoBucket`): `Aggregator` now keeps a `(time, bytes)` sample
  per record (cheap: ~16 bytes/record) instead of bucketing incrementally,
  so the actual bucket size can be picked once the full time span is
  known — smallest of
  `1m/5m/15m/30m/1h/3h/6h/12h/24h` that keeps the bucket count ≤20. This
  replaced a fixed 5m default after real usage showed a multi-hour log
  needed coarser buckets to stay readable. `--bucket <duration>` still
  forces a fixed size; `--bucket 0` still disables it.
- `--version` prints version/commit/date/builtBy, sourced from
  `main.version` etc. via `-ldflags -X`. This mirrors goreleaser's
  *default* ldflags exactly (see below) so no custom `ldflags:` override
  is needed in `.goreleaser.yaml` — just those four `var`s existing in
  `main.go` is enough.

## Docker / CI / releases

- `Dockerfile`: multi-stage, `FROM --platform=$BUILDPLATFORM golang:1.26-alpine`
  builder (cross-compiles via `GOOS`/`GOARCH` `TARGETOS`/`TARGETARCH`
  build args — avoids QEMU-emulating the Go compiler itself for
  multi-arch builds) → `FROM scratch` final stage with just the static
  binary. No CA certs/tzdata needed: gonginxlog makes no network calls
  and doesn't consult the IANA tz database (offsets come straight from
  the log's own timestamps). ~4MB final image.
- `.github/workflows/docker.yml`: builds `linux/amd64,linux/arm64` and
  pushes to **both** `ghcr.io/<owner>/<repo>` (auth via the automatic
  `GITHUB_TOKEN`, needs `packages: write` permission) and Docker Hub
  (`<DOCKERHUB_USERNAME>/gonginxlog`). Triggers: push to `main` (tags
  `latest`/`sha-*`) and version tags `v*.*.*` (tags `X.Y.Z`/`X.Y`), plus
  manual dispatch.
  **Requires repo setup**: an Actions **variable** `DOCKERHUB_USERNAME`
  and an Actions **secret** `DOCKERHUB_TOKEN` (a Docker Hub access
  token). Without them the Docker Hub login step fails; GHCR push still
  needs nothing extra.
- `.github/workflows/release.yml` + `.goreleaser.yaml`: on `v*.*.*` tags,
  GoReleaser cross-builds linux/darwin/windows × amd64/arm64, archives
  (`tar.gz`, `zip` on Windows), writes `checksums.txt`, and publishes a
  GitHub Release — using the automatic `GITHUB_TOKEN` (`contents: write`
  permission), no extra secrets needed. Verified locally with
  `goreleaser check` and `goreleaser release --snapshot --clean` (builds
  all 6 targets, ~1.2MB binaries).
- Action versions were looked up live (via context7) rather than assumed
  from training data, since these get bumped often:
  `docker/build-push-action@v7`, `docker/metadata-action@v6`,
  `docker/login-action@v4`, `docker/setup-{qemu,buildx}-action@v4`,
  `goreleaser/goreleaser-action@v6`, goreleaser config `version: 2` with
  `formats:` (plural, current) not the deprecated singular `format:`.

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
logging.go                  colorized warnf/fatalf (red/yellow, NO_COLOR-aware)
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
