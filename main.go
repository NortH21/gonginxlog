// Command gonginxlog parses nginx access logs according to a log_format
// directive (read from an nginx.conf, or a built-in default matching the
// project's main_json escape=json format), applies time/status/ip/grep
// filters, and prints an aggregated report (or the matching raw lines).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/north21/gonginxlog/internal/anomaly"
	"github.com/north21/gonginxlog/internal/filter"
	"github.com/north21/gonginxlog/internal/format"
	"github.com/north21/gonginxlog/internal/input"
	"github.com/north21/gonginxlog/internal/nginxconf"
	"github.com/north21/gonginxlog/internal/output"
	"github.com/north21/gonginxlog/internal/parser"
	"github.com/north21/gonginxlog/internal/routes"
	"github.com/north21/gonginxlog/internal/stats"
	"github.com/north21/gonginxlog/internal/term"
	"github.com/north21/gonginxlog/internal/tui"
)

// version, commit, date and builtBy are set at build time via -ldflags
// "-X main.xxx=...", matching goreleaser's default ldflags so a release
// build's --version output is meaningful without extra config.
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
	builtBy = "unknown"
)

// valueFlagNames lists every flag (without leading dashes) that consumes a
// following token as its value, so reorderArgs can tell a flag's value
// apart from a positional file argument.
var valueFlagNames = map[string]bool{
	"nginx-conf":         true,
	"format-name":        true,
	"since":              true,
	"until":              true,
	"last":               true,
	"status":             true,
	"ip":                 true,
	"country":            true,
	"grep":               true,
	"top":                true,
	"bucket":             true,
	"field":              true,
	"ui-buffer":          true,
	"anomaly-ip-share":   true,
	"anomaly-ip-min":     true,
	"anomaly-scan-paths": true,
	"anomaly-hammer-ips": true,
	"routes-file":        true,
}

// reorderArgs splits args into flag tokens (kept in their original order,
// each paired with its value if it takes one) and positional tokens (log
// file paths), so that flags typed after positional arguments still work.
// A lone "-" is always treated as positional (the stdin marker), never as
// a flag.
func reorderArgs(args []string, valueFlags map[string]bool) (flagArgs, positional []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			positional = append(positional, args[i+1:]...)
			return
		case a == "-":
			positional = append(positional, a)
		case strings.HasPrefix(a, "-"):
			flagArgs = append(flagArgs, a)
			name := strings.TrimLeft(a, "-")
			if strings.Contains(name, "=") {
				continue // value is already attached, e.g. --top=5
			}
			if valueFlags[name] && i+1 < len(args) {
				i++
				flagArgs = append(flagArgs, args[i])
			}
		default:
			positional = append(positional, a)
		}
	}
	return
}

type fieldFlags []string

func (f *fieldFlags) String() string { return strings.Join(*f, ",") }
func (f *fieldFlags) Set(v string) error {
	*f = append(*f, v)
	return nil
}

func main() {
	var (
		nginxConfFlag      = flag.String("nginx-conf", "", "path to nginx.conf to read the log_format from (includes are followed)")
		formatNameFlag     = flag.String("format-name", format.DefaultName, "name of the log_format directive to use")
		sinceFlag          = flag.String("since", "", "keep entries at or after this time (RFC3339 or nginx time_local layout)")
		untilFlag          = flag.String("until", "", "keep entries strictly before this time")
		lastFlag           = flag.String("last", "", "keep entries from the last duration, e.g. 1h30m (overrides --since)")
		statusFlag         = flag.String("status", "", "comma-separated status filter: exact codes, classes (5xx), ranges (500-599)")
		ipFlag             = flag.String("ip", "", "comma-separated client IP filter: exact addresses and/or CIDR subnets")
		countryFlag        = flag.String("country", "", "comma-separated country code filter (e.g. RU,US), matched against $geoip_country_code/$geoip2_data_country_code")
		grepFlag           = flag.String("grep", "", "regexp that must match the raw log line")
		jsonFlag           = flag.Bool("json", false, "print the report as JSON instead of text tables")
		linesFlag          = flag.Bool("lines", false, "print matching raw lines (like grep)")
		reportFlag         = flag.Bool("report", true, "print the aggregated report")
		topFlag            = flag.Int("top", 10, "number of rows to show in each top-N table")
		bucketFlag         = flag.String("bucket", "auto", "time bucket size for the requests-over-time histogram: a duration (e.g. 30m), \"auto\" (pick from the data's time span), or 0 to disable")
		followFlag         = flag.Bool("f", false, "follow a single log file for new lines (tail -f); prints matching lines only")
		uiFlag             = flag.Bool("ui", false, "open a k9s-style TUI dashboard (live-tails one plain file by default; see --ui-static)")
		uiStaticFlag       = flag.Bool("ui-static", false, "make --ui browse a static snapshot instead of live-tailing (also implied by multiple files, a .gz file, or stdin)")
		uiBufferFlag       = flag.Int("ui-buffer", 10000, "ring buffer size backing the --ui raw view and drill-down")
		anomalyIPShareFlag = flag.Float64("anomaly-ip-share", anomaly.DefaultThresholds.FloodShare, "--ui alerts: min share (0-1) of traffic from one IP in the last 60s to flag as a flood")
		anomalyIPMinFlag   = flag.Int("anomaly-ip-min", anomaly.DefaultThresholds.FloodMinTotal, "--ui alerts: min total requests in that 60s window before the flood share is even checked")
		anomalyScanFlag    = flag.Int("anomaly-scan-paths", anomaly.DefaultThresholds.ScanPaths, "--ui alerts: min distinct paths one IP must hit in 10s to flag as scanning")
		anomalyHammerFlag  = flag.Int("anomaly-hammer-ips", anomaly.DefaultThresholds.HammerIPs, "--ui alerts: min distinct IPs hitting one path in 10s to flag as a distributed hammer")
		routesFileFlag     = flag.String("routes-file", "", "YAML file of regex path->route rules; groups paths into bounded route labels for --top paths and enables the \"slowest routes\" report/view")
		showAgentsFlag     = flag.Bool("show-agents", false, "include the Top user agents section/view (off by default)")
		showReferersFlag   = flag.Bool("show-referers", false, "include the Top referers section/view (off by default)")
		versionFlag        = flag.Bool("version", false, "print the version and exit")
	)
	var fields fieldFlags
	flag.Var(&fields, "field", "field=regexp filter on one named field, repeatable (e.g. --field http_user_agent=bot)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `gonginxlog - filter and summarize nginx access logs using a real log_format

Usage:
  gonginxlog [flags] <logfile> [more logfiles...]
  gonginxlog [flags] -              (read from stdin)
  gonginxlog -f [flags] <logfile>   (follow / tail -f)

Flags:
`)
		flag.PrintDefaults()
	}

	// flag.Parse stops at the first non-flag token, so `gonginxlog
	// access.log --top 5` would silently treat "--top" and "5" as extra
	// file paths. Reorder os.Args so every flag (wherever it was typed)
	// comes before the positional file arguments.
	flagArgs, positional := reorderArgs(os.Args[1:], valueFlagNames)
	if err := flag.CommandLine.Parse(append(flagArgs, positional...)); err != nil {
		os.Exit(2)
	}

	if *versionFlag {
		fmt.Printf("gonginxlog %s (commit %s, built %s by %s)\n", version, commit, date, builtBy)
		return
	}

	files := flag.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, colorize(term.Red, "error:")+" no input files given (use '-' to read from stdin)")
		flag.Usage()
		os.Exit(2)
	}

	spec, err := loadFormatSpec(*nginxConfFlag, *formatNameFlag)
	if err != nil {
		fatalf("%v", err)
	}
	p, err := parser.New(spec)
	if err != nil {
		fatalf("%v", err)
	}

	filters, err := buildFilters(*sinceFlag, *untilFlag, *lastFlag, *statusFlag, *ipFlag, *countryFlag, *grepFlag, fields)
	if err != nil {
		fatalf("%v", err)
	}

	bucket, err := parseBucket(*bucketFlag)
	if err != nil {
		fatalf("invalid --bucket %q: %v", *bucketFlag, err)
	}

	trackCountry := specHasVariable(spec, "geoip_country_code") || specHasVariable(spec, "geoip2_data_country_code")

	var routeRules routes.Rules
	if *routesFileFlag != "" {
		routeRules, err = routes.Load(*routesFileFlag)
		if err != nil {
			fatalf("%v", err)
		}
	}

	if *uiFlag {
		for _, f := range files {
			if f == "-" {
				fatalf("--ui can't read from stdin: it needs stdin free for keyboard input")
			}
		}
		live := len(files) == 1 && !*uiStaticFlag && !strings.HasSuffix(files[0], ".gz")
		anomalyThresholds := anomaly.Thresholds{
			FloodShare:    *anomalyIPShareFlag,
			FloodMinTotal: *anomalyIPMinFlag,
			ScanPaths:     *anomalyScanFlag,
			HammerIPs:     *anomalyHammerFlag,
		}
		runUI(files, p, filters, uiOptions{
			trackCountry: trackCountry,
			bufferSize:   *uiBufferFlag,
			anomaly:      anomalyThresholds,
			live:         live,
			pathLabel:    routeLabeler(routeRules),
			showAgents:   *showAgentsFlag,
			showReferers: *showReferersFlag,
		})
		return
	}

	if *followFlag {
		if len(files) != 1 || files[0] == "-" {
			fatalf("-f/--follow requires exactly one real file path")
		}
		runFollow(files[0], p, filters)
		return
	}

	agg := stats.NewAggregator(*topFlag, bucket, trackCountry)
	if labeler := routeLabeler(routeRules); labeler != nil {
		agg.SetPathLabeler(labeler)
	}
	runBatch(files, p, filters, agg, *linesFlag, *reportFlag, *jsonFlag, output.Options{
		ShowAgents:   *showAgentsFlag,
		ShowReferers: *showReferersFlag,
	})
}

// routeLabeler builds the func(path string) string that
// stats.Aggregator.SetPathLabeler expects from a loaded routes.Rules,
// falling back unmatched paths to "other" so route-timing/route-count
// memory stays bounded by rule count + 1. Returns nil (meaning "use raw
// paths, don't track route timing") when no rules were configured.
func routeLabeler(rules routes.Rules) func(path string) string {
	if rules == nil {
		return nil
	}
	return func(path string) string {
		if label, ok := rules.Label(path); ok {
			return label
		}
		return "other"
	}
}

// specHasVariable reports whether the log_format carries the given nginx
// variable, so optional report sections (e.g. country distribution) only
// show up when the data actually supports them.
func specHasVariable(spec *format.Spec, variable string) bool {
	for _, f := range spec.Fields {
		if f.Variable == variable {
			return true
		}
	}
	return false
}

func parseBucket(spec string) (stats.Bucket, error) {
	if spec == "auto" {
		return stats.AutoBucket, nil
	}
	d, err := time.ParseDuration(spec)
	if err != nil {
		return stats.Bucket{}, err
	}
	if d <= 0 {
		return stats.DisabledBucket, nil
	}
	return stats.FixedBucket(d), nil
}

func loadFormatSpec(nginxConfPath, formatName string) (*format.Spec, error) {
	if nginxConfPath == "" {
		return format.Default(), nil
	}
	raw, escapeJSON, err := nginxconf.LoadRawFormat(nginxConfPath, formatName)
	if err != nil {
		return nil, err
	}
	return format.ParseTemplate(formatName, raw, escapeJSON)
}

func buildFilters(since, until, last, status, ip, country, grep string, fields fieldFlags) (filter.And, error) {
	var filters filter.And

	var tr filter.TimeRange
	hasTimeFilter := false
	if last != "" {
		d, err := time.ParseDuration(last)
		if err != nil {
			return nil, fmt.Errorf("invalid --last %q: %w", last, err)
		}
		tr.From = time.Now().Add(-d)
		hasTimeFilter = true
	}
	if since != "" {
		t, err := filter.ParseTime(since)
		if err != nil {
			return nil, fmt.Errorf("invalid --since %q: %w", since, err)
		}
		tr.From = t
		hasTimeFilter = true
	}
	if until != "" {
		t, err := filter.ParseTime(until)
		if err != nil {
			return nil, fmt.Errorf("invalid --until %q: %w", until, err)
		}
		tr.To = t
		hasTimeFilter = true
	}
	if hasTimeFilter {
		filters = append(filters, tr)
	}

	if status != "" {
		f, err := filter.ParseStatus(status)
		if err != nil {
			return nil, err
		}
		filters = append(filters, f)
	}

	if ip != "" {
		f, err := filter.ParseIP(ip)
		if err != nil {
			return nil, err
		}
		filters = append(filters, f)
	}

	if country != "" {
		f, err := filter.ParseCountry(country)
		if err != nil {
			return nil, err
		}
		filters = append(filters, f)
	}

	if grep != "" {
		f, err := filter.NewGrep("", grep)
		if err != nil {
			return nil, err
		}
		filters = append(filters, f)
	}

	for _, spec := range fields {
		key, pattern, ok := strings.Cut(spec, "=")
		if !ok || key == "" {
			return nil, fmt.Errorf("invalid --field %q, expected field=regexp", spec)
		}
		f, err := filter.NewGrep(key, pattern)
		if err != nil {
			return nil, err
		}
		filters = append(filters, f)
	}

	return filters, nil
}

func runBatch(files []string, p parser.Parser, filters filter.And, agg *stats.Aggregator, showLines, showReport, jsonOut bool, textOpts output.Options) {
	rc, err := input.Open(files)
	if err != nil {
		fatalf("%v", err)
	}
	defer rc.Close()

	scanner := input.NewLineScanner(rc)
	parseErrors := 0
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		rec, err := p.Parse(line)
		if err != nil {
			parseErrors++
			continue
		}
		if !filters.Match(rec) {
			continue
		}
		if showLines {
			fmt.Println(term.Sanitize(rec.Raw))
		}
		if showReport {
			agg.Add(rec)
		}
	}
	if err := scanner.Err(); err != nil {
		fatalf("reading input: %v", err)
	}

	if showReport {
		rep := agg.Report()
		if jsonOut {
			if err := output.WriteJSON(os.Stdout, rep); err != nil {
				fatalf("%v", err)
			}
		} else {
			output.WriteText(os.Stdout, rep, textOpts)
		}
	}

	if parseErrors > 0 {
		warnf("%d line(s) did not match the configured log_format and were skipped", parseErrors)
	}
}

// uiOptions bundles --ui's less-central knobs so runUI's signature
// doesn't grow a parameter per flag.
type uiOptions struct {
	trackCountry bool
	bufferSize   int
	anomaly      anomaly.Thresholds
	live         bool
	pathLabel    func(path string) string
	showAgents   bool
	showReferers bool
}

func runUI(paths []string, p parser.Parser, filters filter.And, opts uiOptions) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app := tui.NewApp(ctx, tui.Config{
		Version:      version,
		Paths:        paths,
		Live:         opts.live,
		Parser:       p,
		BaseFilters:  filters,
		TrackCountry: opts.trackCountry,
		BufferSize:   opts.bufferSize,
		Refresh:      time.Second,
		PollInterval: 500 * time.Millisecond,
		Anomaly:      opts.anomaly,
		PathLabel:    opts.pathLabel,
		ShowAgents:   opts.showAgents,
		ShowReferers: opts.showReferers,
	})
	if err := app.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		fatalf("%v", err)
	}
}

func runFollow(path string, p parser.Parser, filters filter.And) {
	fmt.Fprintln(os.Stderr, "note: -f/--follow streams matching lines only; aggregated stats are reserved for a future TUI mode")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := input.Follow(ctx, path, 500*time.Millisecond, func(line string) error {
		rec, err := p.Parse(line)
		if err != nil {
			return nil
		}
		if !filters.Match(rec) {
			return nil
		}
		fmt.Println(term.Sanitize(rec.Raw))
		return nil
	})
	if err != nil {
		fatalf("%v", err)
	}
}
