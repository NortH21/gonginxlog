// Command gonginxlog parses nginx access logs according to a log_format
// directive (read from an nginx.conf, or a built-in default matching the
// project's main_json escape=json format), applies time/status/ip/grep
// filters, and prints an aggregated report (or the matching raw lines).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/north21/gonginxlog/internal/filter"
	"github.com/north21/gonginxlog/internal/format"
	"github.com/north21/gonginxlog/internal/input"
	"github.com/north21/gonginxlog/internal/nginxconf"
	"github.com/north21/gonginxlog/internal/output"
	"github.com/north21/gonginxlog/internal/parser"
	"github.com/north21/gonginxlog/internal/stats"
)

// valueFlagNames lists every flag (without leading dashes) that consumes a
// following token as its value, so reorderArgs can tell a flag's value
// apart from a positional file argument.
var valueFlagNames = map[string]bool{
	"nginx-conf":  true,
	"format-name": true,
	"since":       true,
	"until":       true,
	"last":        true,
	"status":      true,
	"ip":          true,
	"grep":        true,
	"top":         true,
	"bucket":      true,
	"field":       true,
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
		nginxConfFlag  = flag.String("nginx-conf", "", "path to nginx.conf to read the log_format from (includes are followed)")
		formatNameFlag = flag.String("format-name", format.DefaultName, "name of the log_format directive to use")
		sinceFlag      = flag.String("since", "", "keep entries at or after this time (RFC3339 or nginx time_local layout)")
		untilFlag      = flag.String("until", "", "keep entries strictly before this time")
		lastFlag       = flag.String("last", "", "keep entries from the last duration, e.g. 1h30m (overrides --since)")
		statusFlag     = flag.String("status", "", "comma-separated status filter: exact codes, classes (5xx), ranges (500-599)")
		ipFlag         = flag.String("ip", "", "comma-separated client IP filter: exact addresses and/or CIDR subnets")
		grepFlag       = flag.String("grep", "", "regexp that must match the raw log line")
		jsonFlag       = flag.Bool("json", false, "print the report as JSON instead of text tables")
		linesFlag      = flag.Bool("lines", false, "print matching raw lines (like grep)")
		reportFlag     = flag.Bool("report", true, "print the aggregated report")
		topFlag        = flag.Int("top", 10, "number of rows to show in each top-N table")
		bucketFlag     = flag.String("bucket", "5m", "time bucket size for the requests-over-time histogram (0 disables it)")
		followFlag     = flag.Bool("f", false, "follow a single log file for new lines (tail -f); prints matching lines only")
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

	files := flag.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "error: no input files given (use '-' to read from stdin)")
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

	filters, err := buildFilters(*sinceFlag, *untilFlag, *lastFlag, *statusFlag, *ipFlag, *grepFlag, fields)
	if err != nil {
		fatalf("%v", err)
	}

	bucketDur, err := time.ParseDuration(*bucketFlag)
	if err != nil {
		fatalf("invalid --bucket %q: %v", *bucketFlag, err)
	}

	if *followFlag {
		if len(files) != 1 || files[0] == "-" {
			fatalf("-f/--follow requires exactly one real file path")
		}
		runFollow(files[0], p, filters)
		return
	}

	runBatch(files, p, filters, stats.NewAggregator(*topFlag, bucketDur), *linesFlag, *reportFlag, *jsonFlag)
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

func buildFilters(since, until, last, status, ip, grep string, fields fieldFlags) (filter.And, error) {
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

func runBatch(files []string, p parser.Parser, filters filter.And, agg *stats.Aggregator, showLines, showReport, jsonOut bool) {
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
			fmt.Println(rec.Raw)
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
			output.WriteText(os.Stdout, rep)
		}
	}

	if parseErrors > 0 {
		fmt.Fprintf(os.Stderr, "warning: %d line(s) did not match the configured log_format and were skipped\n", parseErrors)
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
		fmt.Println(rec.Raw)
		return nil
	})
	if err != nil {
		fatalf("%v", err)
	}
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
