package filter

import (
	"fmt"
	"regexp"

	"github.com/north21/gonginxlog/internal/record"
)

// Grep matches a regexp against either the whole raw line (Field == "") or
// a single field's value (Field == nginx variable name, e.g.
// "http_user_agent" for --field http_user_agent=...).
type Grep struct {
	Field string
	re    *regexp.Regexp
}

// NewGrep compiles pattern for --grep (field == "") or --field field=pattern.
func NewGrep(field, pattern string) (*Grep, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regexp %q: %w", pattern, err)
	}
	return &Grep{Field: field, re: re}, nil
}

func (g *Grep) Match(r *record.Record) bool {
	if g.Field == "" {
		return g.re.MatchString(r.Raw)
	}
	return g.re.MatchString(r.Get(g.Field))
}
