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
- `.github/workflows/test.yml`: `go build && go vet && gofmt -l . && go
  test ./... -race` (no `-count=10` in CI - that's a local
  concurrency-change habit, not worth the extra CI minutes on every
  push). Declared with `on: workflow_call` so `docker.yml` and
  `release.yml` both gate their `build-and-push`/`goreleaser` job on it
  via `needs: test` (a `test:` job with just `uses:
  ./.github/workflows/test.yml`, no `runs-on` - that's how a reusable
  workflow is invoked from another workflow in the same repo) instead of
  duplicating the same steps in three places. It also has its own
  `push`/`pull_request` triggers so tests run on every change, not only
  on the main/tag pushes those two workflows react to. Added 2026-08-13
  after a batch of security-hardening changes (see the sanitization
  section below) made "tests must pass before a Docker image or a
  release binary ships" worth enforcing instead of just running locally.

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

### A real crash: concurrent map read/write (2026-08-12, later same day)

On the *previous* version the user hit a hard process crash - a Go
runtime fatal error (`internal/runtime/maps.fatal`, not a normal
`panic`, so `recover()` can't catch it) while `--ui` was running against
real traffic. This is Go detecting a **concurrent map read and map
write** and killing the process outright. Root-caused to two related
bugs, both now fixed and verified with `go test -race` (which reproduces
the exact same class of bug deterministically instead of waiting for
unlucky timing in production):

1. **`stats.Aggregator.Report()`'s `StatusDist` field was a live alias,
   not a snapshot**: `StatusDist: a.statusCounts` assigned the *same map
   object* `Add()` keeps mutating, instead of a copy. `Report()`'s own
   doc comment says it "snapshots the current aggregates" - every other
   field honored that (the `Top*` fields go through `topN()`, which
   builds a fresh slice; `Histogram` builds a fresh slice from a fresh
   local map), but `StatusDist` didn't. Once the live TUI holds a
   `*Report` across goroutines (which it always does - that's the whole
   point of `Report()`), any concurrent `Add()` racing a later read of
   `rep.StatusDist` (e.g. `renderStatusTable` iterating it from tview's
   event goroutine while `ingestLive` is still calling `Add()` from the
   tail goroutine) is a live, ever-present race - not an edge case, the
   normal steady state. Fixed by copying `a.statusCounts` into a new map
   before constructing the `Report`.
2. **`App.reseed`'s goroutine called `agg.Report()` *after* releasing
   `a.mu`**: by that point `agg` had already been assigned to `a.agg`,
   so `Report()`'s internal iteration over `a.ipCounts`/`a.pathCounts`/
   etc. could run at the same time as `ingestLive`'s `a.agg.Add()` (which
   *does* take `a.mu`, but that doesn't help if the reader doesn't).
   Fixed by moving the `Report()` call back inside the locked section,
   matching the pattern `tickLoop` already used correctly.

Bug (1) alone would have caused a race pretty much continuously under
any real traffic, independent of (2) - it's the more likely explanation
for what the user actually hit. Both needed fixing since either one is
sufficient on its own.

**Verification note**: getting `go test ./internal/tui/... -race` clean
also required fixing the *test* itself (`app_smoke_test.go`) - it was
reading `app.lastReport`/`app.activeView`/etc. directly from the test
goroutine while `--ui`'s internal goroutines were mutating them, which
`-race` correctly flags even though nothing outside the test ever
touches those fields except through tview's own serialized event loop.
Added a small `syncRead[T]` helper that runs the read via
`app.QueueUpdateDraw` and receives the result over a channel, so
assertions are synchronized through the same mechanism the app itself
uses for every other cross-goroutine handoff. `go test ./... -race
-count=10` is now clean; run this (not just a plain `go test`) before
trusting any future change to `internal/tui`'s concurrency - a plain run
without `-race` will not catch this class of bug.

### Version in the header, and a pause hotkey (2026-08-12, later still)

Directly motivated by the "am I even running the fixed version?"
back-and-forth over the crash above: `Config.Version` (set from
`main.go`'s existing `version` var, the same one `-ldflags -X
main.version=...` populates) is now threaded through to
`renderHeader`, shown as `gonginxlog vX.Y.Z` at the front of the header
line. No new flag - it's just always shown, same as `--version`'s
output but visible without leaving the dashboard.

`p` toggles pause, by analogy with k9s/htop-style freeze (the user's
own framing). Design choice: pausing freezes **the display only**,
not data collection - `tickLoop` skips its recompute-and-redraw step
entirely while `a.paused` (checked under `a.mu`, since it's written
from the key-handler goroutine and read from `tickLoop`'s), but
`ingestLive` keeps running unconditionally in the background, so
nothing arriving during a pause is lost. On resume, `togglePause` does
one immediate `Report()`/`Tick()` + render rather than waiting for the
next tick, so there's no lag before the screen catches up to
whatever accumulated while paused. The header's `● LIVE` becomes
`⏸ PAUSED` so the frozen state is visually obvious, matching the
existing `⚠ N alert(s)` badge pattern. `TestAppSmoke` gained a
pause/resume case: append a line while paused and assert
`lastReport.TotalRequests` doesn't move, then resume and assert it
catches up - passes under `-race`.

### `DefaultThresholds` recalibrated from two real false positives (2026-08-12, same day)

Both real sites tested against `--ui` false-positived on `ip_flood`
with the original defaults (`FloodShare: 0.30, FloodMinTotal: 20`),
for opposite reasons:

- **A very quiet real site** (77 total requests over ~7 minutes):
  a single real visitor's normal session (multi-page navigation +
  tracking beacons, 20 distinct paths - clearly not a scan or an
  attack) is trivially 100% of a quiet window's traffic, and
  `FloodMinTotal: 20` is far below what one real session generates in a
  minute (37 requests in the busiest observed minute alone).
- **A busy real site** (~800 req/s): a single legitimate high-volume
  client (almost certainly a backend's own heartbeat/health-check
  traffic, all HTTP 200) sat around a 31-35% share, comfortably clearing
  the 30% bar without being an attack.

There's no way to raise *either* value enough on its own to cover both:
raising `FloodMinTotal` far enough to exclude a quiet site's single
visitor doesn't help the busy site (which clears any reasonable count
floor trivially), and raising `FloodShare` alone doesn't help the quiet
site (still 100%). Both needed to move together. Picked `FloodShare:
0.60, FloodMinTotal: 100` specifically because they're the smallest
round numbers that clear *both* observed false positives given the
actual numbers above (60% > 35%, and 100 > 77) while presumably still
being low enough to catch an actual single-IP flood, which by
definition pushes far past either number. `ScanPaths`/`HammerIPs` were
left unchanged - no false positives were reported against either of
those two detectors on either site. Existing unit tests
(`TestIPFloodTriggers`, `TestAlertResolvesWhenNoLongerTriggering`)
updated to use sample counts that still clear the new, higher bar.

This is a "best guess from two data points," not a claim that 0.60/100
is universally right - it's still just the default; `--anomaly-ip-share`
/`--anomaly-ip-min` exist precisely so a third site with a different
profile isn't stuck with it.

### Git history was rewritten to remove real domains and the author's work email (2026-08-12, later still)

Despite the earlier security review before the first push, two real
domain names (a partner site's referer domain, and the hostname of one
of the `--ui` test sessions) ended up committed in README/DESIGN
examples anyway - caught by the user, not by me, after they'd already
been pushed and were part of tagged releases (v0.1.0 through v0.3.1).
Separately, every commit's author/committer email was the user's real
work email (their git global config, not something set for this repo
specifically), which also identifies their employer.

Fixed both, all the way back through history, not just going forward:

- `git filter-repo --replace-text <file>` to replace the two domain
  strings everywhere they appeared across all commits (two old commits
  had them, per `git rev-list --all | git grep`).
- `git filter-repo --mailmap <file>` to remap every commit's
  author/committer email to `north21@users.noreply.github.com` (GitHub's
  own noreply-email convention), keeping the display name.
- This rewrites every commit's SHA from that point on. All 5 existing
  tags (v0.1.0-v0.3.1) got force-pushed to point at their rewritten
  equivalents (`git push --tags --force`) rather than deleted/recreated,
  and the GitHub Releases attached to those tag *names* kept working
  (their uploaded binary assets aren't tied to git SHAs at all, and the
  tag object just got repointed).
- Took a local `.git` backup before running anything, and verified
  clean (`git grep` over `git rev-list --all` finds nothing) both
  locally and via the GitHub API/raw content *after* pushing, before
  considering this done.

**Standing rule, not just a one-time cleanup**: never put a real
hostname/domain, real IP from a real log, or personal/work email into
an example, comment, or commit in this repo - use `example.com`-style
placeholders even when a real one would make an example more concrete.
This bit us once already after an explicit security review; don't
assume "I'll catch it in review" is enough.

## Report/UI improvement batch: routes, slow-route timing, colored batch output, static `--ui`, truncation, opt-in agents/referers (2026-08-13)

Follow-up to real `--ui` usage, prioritized via Q&A, with one stated
overriding constraint: **no meaningful extra load, no crashes**. Every
design choice below picks the safer/simpler option over the more
"complete" one for that reason.

- **`internal/routes`**: user-supplied YAML regex-to-label rules,
  first-match-wins, invalid regex/missing label/empty file fail at
  `Load()` time (startup), not per-line at runtime. Dependency choice
  mattered here: `gopkg.in/yaml.v3` (the classic, near-universal import
  path) hasn't been released since 2022 - the project moved to
  `go.yaml.in/yaml/v3` (same v3 API, actively maintained, last release
  2026-07-26), confirmed by checking `proxy.golang.org/.../@latest` for
  both paths rather than assuming from training data (the org also
  publishes a `v4`, but it's still an RC - `v3` under the new path was
  the right pick: current *and* stable).
- **Route grouping is deliberately not the default, and never applies
  to raw paths automatically**: `stats.Aggregator.SetPathLabeler(fn
  func(path string) string)` is additive/opt-in - `Add()` only relabels
  paths when a labeler was set, so every existing call site is
  unaffected unless it opts in. `Report().RouteTiming` (new field,
  `[]RouteTimingEntry{Route, Count, AvgSeconds}`, sorted slowest-first)
  only gets populated when a labeler is set, mirroring the existing
  `TopCountries == nil` pattern for "not applicable to this format/
  config." Memory for `routeTimings` is bounded by the number of
  distinct labels the function returns (i.e. by the rule count + 1 for
  "other"), never by raw-path cardinality or request volume - verified
  directly with `TestPathLabelerBoundsRouteTimingMemory` (10,000
  distinct raw paths, 3 labels, asserts the map never exceeds 3
  entries).
- **Average, not percentiles, for route timing**: `routeTimingAccum{sum
  float64; n int}` is O(1) memory per route. Exact percentiles would
  need per-route sample storage; since route cardinality is
  config-bounded (not traffic-bounded) that's *less* risky than the
  per-raw-path case would have been, but there was no concrete need for
  percentile precision here to justify the extra memory against an
  explicit "don't add load" ask - average already answers "which routes
  are slow."
- **`internal/term`** extracted from `logging.go`'s pre-existing
  `isTerminal`/`useColor`/`colorize` (used for `warn:`/`error:` on
  stderr) so `internal/output`'s new status-table coloring (same
  2xx/3xx/4xx/5xx mapping `internal/tui/colors.go` already uses, as ANSI
  codes instead of `tcell.Color`) doesn't duplicate that logic. `output.
  WriteText` type-asserts its `io.Writer` to `*os.File` to decide
  colorability (a `bytes.Buffer` or anything else never gets ANSI
  codes, regardless of terminal state) - `TestWriteTextNoColorForNonFileWriter`
  covers this.
- **Top User-Agent/Referer are opt-in** (`--show-agents`/
  `--show-referers`, both default `false`) in *both* the batch report
  and `--ui`'s view list (`buildViews()` wraps those two `addTable`
  calls the same way it already wrapped `countries` behind
  `TrackCountry`) - applying it to both surfaces, not just the batch
  report, keeps "this is opt-in data" consistent rather than
  inconsistent-by-surface.
- **Static `--ui`** (`Config.Live bool`, was implicitly always-true
  before): live only when exactly one plain, non-`.gz` file was given
  and `--ui-static` wasn't passed; anything else (multiple files, a
  `.gz`, or the explicit flag) is static. `Run()` doesn't start
  `followLoop` *or* `tickLoop` at all when static - there's nothing that
  changes after the initial scan, so there's no periodic work to skip
  doing, not just periodic work that no-ops. Filter changes still work
  through the existing `reseed` path regardless (it re-scans
  `cfg.Paths`, live or not). `--ui -` (stdin) is rejected outright at
  the CLI layer, live or static: tcell needs the process's own stdin
  for keyboard input, and a piped `-` almost certainly means the caller
  intended stdin for log data instead - letting that combination reach
  tcell would misbehave in a confusing way rather than fail clearly.
  `p` (pause) is a no-op when static (nothing is ticking to pause).
  Header shows `■ STATIC` in place of `● LIVE`/`⏸ PAUSED` and omits
  `req/s`/`uptime` (not meaningful without tailing).
  `TestAppSmokeStatic` proves the "nothing tails" guarantee concretely,
  not just by code inspection: it appends a line to one of the seed
  files *after* `Run()` starts and asserts `lastReport` doesn't move.
- **Truncation**: `views.go`'s `truncate(s string, max int) string`
  (plain Go, 80-char default) is applied to the `Key` column in
  `renderCountTable`/`renderRouteTimingTable`, not via
  `tview.TableCell.SetMaxWidth` (checked the source - it constrains
  column screen-width but its godoc doesn't promise an ellipsis, so
  relying on it for *this specific* UX goal felt like guessing).
  Drill-down is unaffected by construction: `onEnter` already reads
  `v.entries[idx].Key` (the untruncated stored value), never rendered
  cell text.
- **Route-aware drill-down**: `matchesDimension` and
  `complementaryBreakdowns` both gained a `pathLabel func(string)
  string` parameter (nil everywhere except when `--routes-file` is
  set) so drilling into anything (an IP, a status code, ...) shows a
  "ROUTE" breakdown column instead of raw "PATH" when routes are
  configured, for the same consistency reason as everywhere else route
  grouping applies.
- **Footer hotkeys are generated from the actually-registered
  `a.views`**, not a hardcoded string, specifically so they never
  advertise a key for a view that isn't there (`5`/`6` when agents/
  referers are off, `8` only when routes are configured) - this
  replaced the previous static `hotkeyBar()` function.

## Terminal / tview injection hardening (2026-08-13)

A security review of the codebase surfaced a real, exploitable class of
bug: every log field this tool ever displays (`$request` → path,
`$http_user_agent`, `$http_referer`, and the raw line itself) is data
the HTTP client fully controls - nginx logs it verbatim, and nothing in
gonginxlog's own pipeline sanitized it before printing. Two distinct
consequences, both fixed the same way:

1. **Terminal escape injection.** A crafted User-Agent/path containing
   raw ESC (`\x1b`) or other C0/C1 control bytes, printed via
   `fmt.Println`/`fmt.Fprintf` in `--lines`, `-f`, or the batch report's
   tables, reaches the operator's real terminal unmodified and can
   forge or hide output, move the cursor, etc.
2. **tview markup injection.** Separately from terminal escapes, tview
   itself interprets `[color]`/`[region]` style tags in any text it
   draws. For `TextView` that's gated by `SetDynamicColors` (which
   `--ui` sets `true` on every view that needs it), but for
   `Table`/`TableCell` there is **no such gate at all** - confirmed by
   reading `rivo/tview@v0.42.0`'s `table.go`/`util.go`: cell text always
   goes through `printWithStyle` with tag-parsing on, unconditionally.
   So a User-Agent like `[red]fake[-:-:-]` shown in a top-agents table
   or an alert's `KEY`/`DETAIL` column would have its markup
   interpreted, not displayed literally.

Where this reaches attacker data can also be subtler than it looks: for
`escape=json` log formats, nginx itself escapes control bytes as
literal `\uXXXX` text in the *file* - but `internal/parser`'s JSON
parser calls `encoding/json`'s decoder, which un-escapes those back
into real control-byte runes in memory before anything downstream
(aggregation, rendering) ever sees the value. So the fix can't rely on
"nginx already escaped it" even for that format, let alone
`escape=none`/older nginx/the plain-format regex parser, where
`Fields`/`Raw` are just substrings of the file bytes with no escaping
guaranteed at all.

**Fix, not a config knob** - this is always on, no flag disables it,
since there's no legitimate reason a path/UA/referer needs a raw
control byte or a literal `[tag]`-shaped substring to display correctly:

- `internal/term.Sanitize(s string) string` strips C0 (`0x00-0x1F`),
  DEL (`0x7F`), and C1 (`0x80-0x9F`) control runes. Applied at every
  print site that touches log-derived text: `main.go`'s `--lines`/`-f`
  output, `internal/output/text.go`'s top-N tables and route-timing
  section, `internal/tui/app.go`'s raw view (`renderRaw`), and
  `internal/tui/detail.go`'s drill-down header/title.
- `internal/tui/views.go`'s new `safeCellText(s string) string` wraps
  `tview.Escape(term.Sanitize(s))` and is applied to every table cell
  sourced from log data: `renderCountTable`'s/`renderRouteTimingTable`'s
  key column, and `renderAlertsTable`'s `KEY`/`DETAIL` columns.
  `tview.Escape` turns `[tag]` into `[tag[]` (tview's own convention for
  "render this literally"), so it's applied *in addition to*
  `Sanitize`, not instead of it - one defends the terminal underneath
  tview, the other defends tview's own markup layer.
- Critically, sanitization/escaping only ever touches the *rendered
  display string* passed to `SetCell`/`Fprintf`, never the underlying
  `stats.CountEntry`/`Entry`/`anomaly.Alert` values that drill-down
  matching (`matchesDimension`, `v.entries[idx].Key`) compares against -
  otherwise an attacker's control bytes in a path would make drill-down
  silently stop matching that path's own records. `renderCountTable`
  already stored `v.entries` before building display cells, so this
  fell out of the existing structure rather than requiring a new split.
- Tests: `internal/term/term_test.go` covers `Sanitize` directly (C0,
  DEL, C1 via a U+009B CSI rune, embedded CR/LF, and a no-op case for
  clean input). `internal/output/text_test.go`'s
  `TestWriteTextSanitizesControlBytesInFields` and
  `internal/tui/security_test.go` (new file) cover the print sites:
  `renderRaw` and `safeCellText` assert no raw ESC byte and no
  live `[tag]` substring survive; `buildDetailPage`'s test drills with a
  malicious User-Agent as the key and inspects the actual
  `tview.Flex.GetTitle()`/header `TextView.GetText(false)` output, not
  just the sanitizer in isolation, so a future refactor that
  accidentally drops the `safeKey` substitution at one of the two call
  sites would fail a test, not just look fine in code review.

## Default query-string trimming for the "paths" table (2026-08-13)

Real-world feedback: a "paths" top-N table on a busy site is close to
useless without `--routes-file` configured, because query parameters
(numeric IDs, tokens, session-ish values) make almost every raw path
unique - e.g. `/api/profile.php?stand=0&id=3002559145` never repeats
with that exact query string, so it shows up as its own 1-count row
instead of contributing to a meaningful `/api/profile.php` total.
`--routes-file` already solves this generally (regex → bounded label),
but configuring it is real effort or the wrong effort for a case that's
just "the query string is noise" - most of the time.

**Decision**: trim everything from the first `?` onward before using a
path as a `pathCounts`/"paths" key, by default, with a new
`--show-path-args` flag to opt back into the full path+query (the
previous, and until now the only, behavior). Precedence: a
`--routes-file` labeler, when configured, always wins and always sees
the *raw, untrimmed* path - its regexes may deliberately match against
query parameters, so this default must never mutate what reaches it.

- `record.Record.PathWithoutQuery()` - the new primitive, `Path()` cut
  at the first `?` (`strings.IndexByte`, no allocation when there's no
  query string to trim).
- `stats.Aggregator.keepPathQuery bool` (zero value `false`, i.e.
  trimming is the default - deliberately chosen so no existing
  construction of `*Aggregator` needs to change to get the new
  behavior) + `SetKeepPathQuery(bool)`. `Add()`: `if pathLabeler != nil
  { use labeler(raw path) } else if !keepPathQuery { use
  PathWithoutQuery() } else { use raw path }`.
- The "path" dimension's drill-down matching (`matchesDimension` in
  `internal/tui/detail.go`) and the PATH breakdown column
  (`complementaryBreakdowns`) both needed the same `keepPathQuery bool`
  threaded through and the identical trim-or-not logic - otherwise
  drilling into a "paths" row (now keyed by the trimmed path) would
  compare against each record's full untrimmed `Path()` and find zero
  matches, the exact class of bug `matchesDimension`'s country-dash
  normalization already exists to avoid (see above). `buildDetailPage`
  also calls `agg.SetKeepPathQuery` on its own throwaway Aggregator for
  the same reason.
- `main.go`: `--show-path-args` (bool, default `false`) wired into both
  the batch `Aggregator` and `tui.Config.KeepPathQuery`, same one flag
  covers both surfaces like `--show-agents`/`--show-referers` do.
- Tests added at both layers this touches: `internal/record` (the
  trimming primitive itself), `internal/stats` (aggregation collapses
  same-path-different-query into one entry by default, keeps them
  distinct when configured, and the labeler still sees the untrimmed
  path regardless), `internal/tui` (drill-down matching and the PATH
  breakdown column, both trimmed-by-default and kept-when-configured).

## Deferred (explicitly, not forgotten)

- Live tailing of **multiple** files in `--ui` (static multi-file
  browsing now works; live is still exactly one file).

## Idea: log-based Prometheus exporter (discussed 2026-08-12, not started)

Not implemented, no code written - captured here so the discussion
isn't lost before the user comes back to it. Context: the user already
runs `nginx-module-vts` for basic per-zone metrics (requests, status
codes, bytes, latency), so a new exporter here should **not** duplicate
that. Its value is specifically the things log-derived data can do that
VTS can't: GeoIP breakdown and the existing `internal/anomaly`
detectors. Everything below is the shape that discussion converged on,
not a commitment to build it this way.

**Scope, deliberately narrow to avoid duplicating VTS**:
- `country` counter from `$geoip_country_code`/`$geoip2_data_country_code`
  (already have this via `record.Record.Country()`).
- Anomaly gauges reusing `internal/anomaly.Detector` as-is, e.g.
  `gonginxlog_active_alerts{type="ip_flood"}` = **count** of currently
  active alerts of that type, not one series per offending IP/path -
  keeps cardinality bounded regardless of how many distinct IPs/paths
  have ever triggered an alert over the exporter's lifetime.
- Path/route labels only via **user-supplied regex rules** (config file
  mapping `pattern -> label`, unmatched falls into `other`) - never raw
  paths as a label. Real paths in this user's logs embed things like
  numeric game/session IDs (`/gamecenter/game/8611353272737455290`,
  `/counter?id=...`), so raw-path-as-label would be unbounded
  cardinality almost immediately.
- Client IPs only as a **bounded top-N gauge** (e.g. top 20, refreshed
  each cycle), never a per-IP counter with unbounded history. Flagged
  as a minor Prometheus anti-pattern (series that appear/disappear
  between scrapes aren't quite what Prometheus is designed for) but
  workable - Prometheus marks series stale automatically when a label
  combination stops being scraped.

**File discovery - the part that addresses "will this hammer the
disk/CPU"**: the user's real `/var/log/nginx` has 608 files / 60GB, but
that's dominated by logrotate archives, not live data. The exporter
should never need to read historical/rotated content at all:
- Periodic (e.g. every 30s) `filepath.Glob` rescan of a configurable
  pattern - default should match the user's actual rotation scheme,
  confirmed live: `<vhost>_access.log` (active) rotates to
  `<vhost>_access.log.N.gz`. A glob of `*_access.log` matches only the
  active file for each vhost automatically - `.N.gz` siblings and
  `<vhost>_error.log` don't match the suffix, no extra filtering logic
  needed. This should still be a `--exporter-glob` flag, not hardcoded,
  since other setups may name things differently.
- Each matched file gets tailed via the same `input.Follow`-style
  polling used by `-f`/`--ui`, but **no seed-read of existing content**
  - unlike `--ui` (where a human wants an immediately-populated
    dashboard), a metrics exporter's counters are meant to be cumulative
    from process start; Prometheus already handles counter resets on
    restart. Starting at EOF means zero large reads, ever, in normal
    operation - only `stat()` polls and incremental reads of newly
    appended bytes.
  - Tailing a since-abandoned file (the user has one vhost whose active
    log hasn't been written to in over a month) costs essentially
    nothing either - it's just a `stat()` that keeps finding no size
    change.
- Needs `internal/input.Follow`-equivalent logic generalized from one
  file to N (it's currently `-f`/`--ui`-only, single file); the
  per-file polling logic itself doesn't need to change, just needs N
  independent instances instead of one.
- Deployment note, not architecture: the log files are
  `nginx:root`-owned, `0640` - the exporter process needs to run as
  `nginx` (or be in the right group, or get an ACL) to read them at
  all. Worth remembering when this gets built, easy to lose an hour to
  otherwise.
- Per-vhost `log_format` could differ; v1 probably assumes one shared
  format across all matched files rather than per-file overrides,
  unless that turns out to be wrong in practice.

**Still open** when this comes back: is it a new `--exporter` mode of
the same `gonginxlog` binary (reusing `internal/parser`/`record`/
`anomaly` directly, my working assumption) or a separate tool/binary;
exact number of live vhosts (order of magnitude for how many concurrent
tailers/goroutines); config file format for the regex route rules.

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
internal/output                text-table (colorized) and JSON rendering
                             of Report
internal/term                   shared terminal/ANSI-color helpers used
                             by both logging.go and internal/output
internal/routes                 YAML regex-to-label rules for bounded
                             path grouping (--routes-file)
internal/anomaly                fixed-threshold sliding-window Detector
                             (ip flood / url scan / distributed hammer)
                             for --ui's alerts view
internal/tui                    the --ui dashboard: App (tview wiring,
                             ticker, seed+follow ingest, live/static),
                             views.go (per-view table/text rendering,
                             truncation), detail.go (drill-down),
                             filter_dsl.go (the "/" mini-language),
                             ringbuffer.go, colors.go
```
