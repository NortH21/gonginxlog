package tui

import (
	"fmt"
	"strings"

	"github.com/north21/gonginxlog/internal/filter"
)

// ParseFilterExpr parses the "/" filter bar's input into a filter.And.
// Space-separated tokens are ANDed together:
//
//	status:<spec>    -> filter.ParseStatus   (e.g. status:5xx,404)
//	ip:<spec>        -> filter.ParseIP       (e.g. ip:203.0.113.5,10.0.0.0/8)
//	country:<spec>   -> filter.ParseCountry  (e.g. country:RU,US)
//	grep:<regexp>    -> whole raw line
//	<field>=<regexp> -> one named field, mirrors the CLI's --field
//
// An empty (or all-whitespace) expr returns an empty, always-matching
// filter.And - that's what clearing the filter bar produces.
func ParseFilterExpr(expr string) (filter.And, error) {
	var and filter.And
	for _, tok := range strings.Fields(expr) {
		f, err := parseFilterToken(tok)
		if err != nil {
			return nil, err
		}
		and = append(and, f)
	}
	return and, nil
}

func parseFilterToken(tok string) (filter.Filter, error) {
	switch {
	case strings.HasPrefix(tok, "status:"):
		return filter.ParseStatus(strings.TrimPrefix(tok, "status:"))
	case strings.HasPrefix(tok, "ip:"):
		return filter.ParseIP(strings.TrimPrefix(tok, "ip:"))
	case strings.HasPrefix(tok, "country:"):
		return filter.ParseCountry(strings.TrimPrefix(tok, "country:"))
	case strings.HasPrefix(tok, "grep:"):
		return filter.NewGrep("", strings.TrimPrefix(tok, "grep:"))
	default:
		key, pattern, ok := strings.Cut(tok, "=")
		if !ok || key == "" {
			return nil, fmt.Errorf("unrecognized filter token %q (expected status:/ip:/country:/grep: or field=regexp)", tok)
		}
		return filter.NewGrep(key, pattern)
	}
}
