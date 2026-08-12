// Package filter implements the composable record.Record predicates used
// for --since/--until/--last, --status, --ip, --grep and --field.
package filter

import "github.com/north21/gonginxlog/internal/record"

// Filter reports whether a Record should be kept.
type Filter interface {
	Match(r *record.Record) bool
}

// And combines filters so all of them must match.
type And []Filter

func (a And) Match(r *record.Record) bool {
	for _, f := range a {
		if !f.Match(r) {
			return false
		}
	}
	return true
}
