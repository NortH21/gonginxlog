package filter

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/north21/gonginxlog/internal/record"
)

// Status matches an HTTP status code against a comma-separated spec mixing
// exact codes ("404"), classes ("5xx") and ranges ("500-599").
type Status struct {
	exact   map[int]bool
	classes map[int]bool // keyed by code/100, e.g. 5 for 5xx
	ranges  [][2]int
}

// ParseStatus parses a --status flag value.
func ParseStatus(spec string) (*Status, error) {
	s := &Status{exact: map[int]bool{}, classes: map[int]bool{}}
	for _, tok := range strings.Split(spec, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		switch {
		case len(tok) == 3 && (tok[1] == 'x' || tok[1] == 'X') && (tok[2] == 'x' || tok[2] == 'X'):
			class, err := strconv.Atoi(tok[:1])
			if err != nil {
				return nil, fmt.Errorf("invalid status class %q", tok)
			}
			s.classes[class] = true
		case strings.Contains(tok, "-"):
			parts := strings.SplitN(tok, "-", 2)
			lo, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
			hi, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err1 != nil || err2 != nil || lo > hi {
				return nil, fmt.Errorf("invalid status range %q", tok)
			}
			s.ranges = append(s.ranges, [2]int{lo, hi})
		default:
			code, err := strconv.Atoi(tok)
			if err != nil {
				return nil, fmt.Errorf("invalid status %q", tok)
			}
			s.exact[code] = true
		}
	}
	return s, nil
}

func (s *Status) Match(r *record.Record) bool {
	code, ok := r.StatusCode()
	if !ok {
		return false
	}
	if s.exact[code] {
		return true
	}
	if s.classes[code/100] {
		return true
	}
	for _, rg := range s.ranges {
		if code >= rg[0] && code <= rg[1] {
			return true
		}
	}
	return false
}
