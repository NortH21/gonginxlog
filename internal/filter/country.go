package filter

import (
	"fmt"
	"strings"

	"github.com/north21/gonginxlog/internal/record"
)

// Country matches $geoip_country_code/$geoip2_data_country_code against a
// comma-separated list of exact codes (case-insensitive).
type Country struct {
	codes map[string]bool
}

// ParseCountry parses a --country flag value.
func ParseCountry(spec string) (*Country, error) {
	codes := map[string]bool{}
	for _, tok := range strings.Split(spec, ",") {
		tok = strings.ToUpper(strings.TrimSpace(tok))
		if tok == "" {
			continue
		}
		codes[tok] = true
	}
	if len(codes) == 0 {
		return nil, fmt.Errorf("--country requires at least one country code")
	}
	return &Country{codes: codes}, nil
}

func (c *Country) Match(r *record.Record) bool {
	return c.codes[strings.ToUpper(r.Country())]
}
