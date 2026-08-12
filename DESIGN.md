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

- **MVP scope** (original, 2026-08-12): CLI filters + aggregated text
  report. A live k9s-styled TUI (`--ui`) was added the same day, in a
  separate round of Q&A — see "Live TUI dashboard" below.
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
formats, roadmap) or the architecture, update **all three** docs in the
same change: `README.md` (usage/examples, **in Russian** since
2026-08-12 — the maintainer's working language), this file
(rationale/decisions/layout, in English, for contributors), and
`CLAUDE.md` (short status snapshot, in Russian, auto-loaded by Claude
Code at the start of every session in this repo) if the overall project
status changed. This was an explicit user instruction, not a
suggestion.

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
- **Country distribution** (`Record.Country()`, `filter.Country`,
  `Aggregator.trackCountry`): decided via Q&A on 2026-08-12.
  - Checks both `$geoip_country_code` (legacy `ngx_http_geoip_module`)
    and `$geoip2_data_country_code` (`ngx_http_geoip2_module`), whichever
    the log_format has.
  - Report table is **top-N** (`--top`, like IPs/paths), not a full
    distribution like `StatusDist` — consistent with the other
    high-cardinality-ish tables.
  - Only appears when the log_format actually carries one of those two
    variables (`specHasVariable` in `main.go`, checked once against
    `format.Spec.Fields` and passed into `stats.NewAggregator` as
    `trackCountry`) — otherwise every record would show as `-` and the
    table would be meaningless noise for formats without geoip at all.
  - Records with no resolved country (geoip module present in the
    format but didn't resolve, e.g. local/internal addresses) are
    grouped under the literal key `-`, shown as a normal row rather than
    excluded — the user wanted to see that volume, not hide it.
  - `--country RU,US` added alongside for symmetry with `--ip`/`--status`
    (exact match on the same two variables, case-insensitive).

## Docker / CI / releases

- `Dockerfile`: multi-stage, `FROM --platform=$BUILDPLATFORM golang:1.26-alpine`
  builder (cross-compiles via `GOOS`/`GOARCH` `TARGETOS`/`TARGETARCH`
  build args — avoids QEMU-emulating the Go compiler itself for
  multi-arch builds) → `FROM scratch` final stage with just the static
  binary. No CA certs/tzdata needed: gonginxlog makes no network calls
  and doesn't consult the IANA tz database (offsets come straight from
  the log's own timestamps). ~5.6MB final image (grew from ~4MB once
  `tview`/`tcell` became a real dependency for `--ui`; still tiny).
  `go.sum` now exists and is `COPY`'d alongside `go.mod` before `go mod
  download`, since there's finally something in it to verify.
- `.github/workflows/docker.yml`: builds `linux/amd64,linux/arm64` and
  pushes to **both** `ghcr.io/<owner>/<repo>` (auth via the automatic
  `GITHUB_TOKEN`, needs `packages: write` permission) and Docker Hub
  (`<DOCKERHUB_USERNAME>/gonginxlog`). Triggers: push to `main` (tags
  `latest`/`sha-*`) and version tags `v*.*.*` (tags `X.Y.Z`/`X.Y`), plus
  manual dispatch.
  Docker Hub publishing is **optional and conditional** on `vars.DOCKERHUB_USERNAME`
  being set (`if: vars.DOCKERHUB_USERNAME != ''` on its login/metadata
  steps) — without it those steps are skipped (not failed), and GHCR
  still publishes. This was a fix made during a pre-publish security
  review: the first version logged into Docker Hub unconditionally, so
  a missing token failed the whole job before it ever reached the GHCR
  push. **To enable Docker Hub too**: add repo Actions **variable**
  `DOCKERHUB_USERNAME` and Actions **secret** `DOCKERHUB_TOKEN` (a
  Docker Hub access token).
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

## Live TUI dashboard (`--ui`)

Decided via three rounds of Q&A on 2026-08-12 (the user explicitly
wanted to be "grilled" on the details before this got built): k9s-styled
— one full-screen table with a view switcher, not a multi-panel
dashboard — targeting live mode from day one, not a static-file browser.

- **Library**: `github.com/rivo/tview` (what k9s itself is built on).
  This is the project's **first external dependency** — everything
  before this was pure stdlib. Pulls in `github.com/gdamore/tcell/v2`.
  Final scratch image grew from ~4MB to ~5.6MB; still tiny.
- **Seeding**: `--ui` batch-reads the file's existing content first
  (reusing the same scan/filter/Add loop as the CLI's batch mode, via
  `App.scanFile`), *then* starts tailing new lines. Plain `input.Follow`
  starts at EOF (correct for `-f`), which would otherwise leave the
  dashboard empty until new traffic arrives. No double-counting: `Follow`
  re-stats the file fresh when it starts, so anything appended in the
  gap between the seed scan and the tail starting gets picked up by the
  tail, not skipped.
- **Refresh**: 1x/second (`Config.Refresh`), driven by a ticker goroutine
  that calls `app.QueueUpdateDraw` — tview's documented pattern for
  updating widgets from a background goroutine. Key-handler callbacks
  (`SetInputCapture`/`SetSelectedFunc`) run on tview's own main/event
  goroutine per its concurrency docs, so they mutate widgets directly
  without `QueueUpdateDraw` (using it there would deadlock).
- **Views** (`internal/tui/views.go`): status, ips, countries (only
  registered if `TrackCountry`), paths, agents, referers, timeline, raw,
  alerts — one hotkey each, fixed regardless of which views exist (so
  `paths` is always `4`, whether or not `countries` is present). Every
  count-table view is rendered from the *uncapped* `stats.Report`
  (`Aggregator` constructed with `topN=0`, which `topN()` already treats
  as "no cutoff") since the tables are scrollable — `--top` stays a
  batch-CLI-only concept.
- **In-view filter (`/`)**: reuses the `filter` package's own
  constructors instead of inventing a grammar (`internal/tui/filter_dsl.go`,
  `ParseFilterExpr`) — `status:`/`ip:`/`country:`/`grep:` prefixes or
  bare `field=regexp`. Applying or clearing (`x`) it **resets and
  re-seeds** (re-scans the file with the new filter ANDed onto the
  startup `--status`/`--ip`/etc. filters) rather than trying to
  retroactively refilter already-aggregated totals in place — simplest
  correct behavior, matches "restart tail -f with a new pattern". Runs
  on a goroutine so a slow re-scan doesn't freeze the UI.
- **Drill-down (`Enter`)**: generic across every table
  (`internal/tui/detail.go`, `matchesDimension`/`buildDetailPage`) —
  filters the ring buffer to the selected key, runs a throwaway
  `stats.Aggregator` over just that subset, shows its status breakdown
  plus the complementary dimension's top list (IP → its paths, path →
  its IPs, etc). Scoped to the ring buffer's contents, not all-time —
  labeled as such in the page title so it's not mistaken for full
  history on a long-running session.
- **Ring buffer** (`internal/tui/ringbuffer.go`): fixed-capacity circular
  buffer of `(*record.Record, raw string)`, `--ui-buffer` (default
  10000). Backs the `raw` view and drill-down only — the running totals
  in every other view come from the live `Aggregator`, which never
  forgets.
- **Anomaly detection** (`internal/anomaly`), all three original roadmap
  items shipped, fixed thresholds (no flags yet): IP flood (≥30% share
  over 60s, once ≥20 samples), URL scan (≥20 distinct paths/IP over
  10s), distributed hammer (≥15 distinct IPs/path over 10s). Implemented
  as tiny per-second sliding-window bucket maps, evicted each `Tick`.
  **The one subtle bit**: `Observe` buckets by each record's *own*
  timestamp, but `Tick` evicts by *wall-clock* `time.Now()`. If both used
  record time, the seed phase (batch-reading potentially hours of
  history in milliseconds) would look like it all happened in the same
  instant and trip every threshold instantly. Keying by record time but
  evicting by wall-clock time means old seeded data ages out of the
  windows before a real `Tick` ever runs — only genuinely live activity
  (whose record timestamp ≈ wall-clock now) can alert. Covered by
  `TestSeedHistoryDoesNotFalselyTrigger`.
- **A real concurrency bug found and fixed during manual testing**: the
  goroutine that calls `app.Stop()` on context cancellation raced with
  `Run()` when the *seed scan itself* (which can take several seconds on
  a large file, and doesn't take a ctx at all originally) was still
  running when Ctrl+C arrived — ctx was already cancelled before `Run()`
  was ever called, so `Stop()` fired on a screen that was never
  initialized, panicking inside tcell's `Fini()` (`close of nil
  channel`). Fixed two ways: `scanFile` now takes a `ctx` and checks it
  every 4096 lines (so Ctrl+C during a slow seed aborts promptly instead
  of being silently ignored until the scan finishes), and `Run()`
  returns early if `ctx.Err() != nil` before starting anything, plus a
  `recover()` around the `Stop()` call as a last-resort safety net. Also
  found because this sandbox has no controlling terminal at all
  (`tview.Application.Run()` fails with `open /dev/tty: device not
  configured`) — a real terminal wouldn't hit that particular error, but
  the Ctrl+C-during-seed race is real regardless of environment.
- **Testing without a real TTY**: `internal/anomaly` and the pure parts
  of `internal/tui` (ring buffer, filter DSL) have plain unit tests.
  `internal/tui/app_smoke_test.go` drives the actual `App` headlessly via
  `tcell.NewSimulationScreen` + `Application.SetScreen` (call it *before*
  `Run()`; tview only creates a real screen if none was already set) —
  injects key events (`2`, `/status:404`+Enter, `x`, `Enter` for
  drill-down, `Esc`, `q`) and asserts view/state transitions. This is how
  the interaction logic was verified in an environment where I
  (Claude) can't watch a terminal render colors myself — the user still
  needs to eyeball the actual look/feel in their own terminal.
- `--ui` requires exactly one real file (same restriction as `-f`) and
  is mutually exclusive in practice with `--lines`/`--json`/`--report`
  (those flags are simply ignored when `--ui` is set; `main.go` checks
  `--ui` first and returns before reaching that logic).

### Fixes and changes from real production use (2026-08-12)

The user ran `--ui` against two real production nginx logs (one
low-traffic, one at roughly 800 req/s) right after the first version
shipped. That surfaced issues no amount of synthetic testing had caught:

- **Hotkey bar had no space after the key**: `hotkeyBar()` in
  `header.go` had `[yellow]1[-:-:-]status` - the closing color tag was
  immediately followed by the label with nothing between them, so it
  rendered as `1status` `2ips` etc. Fixed by adding a space after every
  closing tag.
- **Drilling into `country=-` always showed 0 requests, even though the
  countries table showed a nonzero share for it**: a real bug, not a
  buffer-staleness artifact. `Aggregator.Add` labels an unresolved
  country as the literal string `"-"` (see the country feature notes
  above), but `Record.Country()` itself returns `""` for that case -
  `matchesDimension`'s `"country"` case compared `rec.Country() == key`
  directly, so it was comparing `"" == "-"`, which is never true. Fixed
  by normalizing `""` to `"-"` in `matchesDimension` the same way the
  aggregator does. Covered by `TestMatchesDimensionCountryDash`.
- **Drilling into a status code only showed a "top paths" breakdown,
  not IPs**: the user's reaction was "shouldn't this show IPs? isn't
  this supposed to be a top list?" - fair, since "which IPs are causing
  these 500s" is at least as useful as "which paths". Generalized
  `complementaryDimension` (singular) into `complementaryBreakdowns`
  (plural): drilling into `ip` or `path` still only shows the other one
  (asking "which paths did this IP hit" doesn't also need "which IPs
  hit this IP"), but `status`/`country`/`user_agent`/`referer` now show
  **both** an IP and a PATH breakdown table.
- **A single legitimate IP (2833 requests, all HTTP 200) tripped the
  fixed 30%-in-60s ip_flood threshold** on the high-traffic log - the
  user's own assessment was "this is legitimate, just a lot of it"
  (very plausibly a game backend's own heartbeat/matchmaking traffic or
  a health checker, common on this kind of service). This is exactly
  the failure mode anticipated when thresholds were first designed
  ("what counts as concentrated varies a lot by site") but the fixed
  values turned out to bite on the very first real target. Rather than
  guess at better universal numbers, the four threshold values became
  CLI flags (`--anomaly-ip-share`, `--anomaly-ip-min`,
  `--anomaly-scan-paths`, `--anomaly-hammer-ips`) backed by
  `anomaly.Thresholds`/`anomaly.DefaultThresholds`, passed through
  `tui.Config.Anomaly`. The three window durations (60s/10s/10s) stay
  fixed - only the trigger values needed tuning per the feedback, and
  keeping the windows fixed keeps the flag surface small. Defaults are
  unchanged from the original values; this is a knob, not a fix to the
  defaults themselves, since there's no universally "right" number.
- **A single "0 requests, 1 total all-time" drill-down (status 500,
  exactly one occurrence) looked like a bug** on the high-traffic log:
  143k+ total requests against a 10000-entry ring buffer means a rare
  event from early in the session is almost certainly already evicted.
  This one *is* the documented ring-buffer-recency limitation, not a
  new bug - but the page gave no indication of *why* it was empty.
  `buildDetailPage` now takes the row's all-time count (from the live,
  unbounded `Aggregator`, passed in by `onEnter`) and, when the buffered
  match count is lower, adds a `(N total all-time - the rest are older
  than the buffer)` note to the header instead of silently showing
  nothing.

## Deferred (explicitly, not forgotten)

- Multi-file tailing in `--ui` (currently exactly one file).
- Interactive TUI browsing of a static, non-live file.

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
internal/anomaly                fixed-threshold sliding-window Detector
                             (ip flood / url scan / distributed hammer)
                             for --ui's alerts view
internal/tui                    the --ui dashboard: App (tview wiring,
                             ticker, seed+follow ingest), views.go
                             (per-view table/text rendering), detail.go
                             (drill-down), filter_dsl.go (the "/" mini-
                             language), ringbuffer.go, colors.go
```
