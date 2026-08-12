// Package routes turns user-supplied regex rules into a bounded set of
// "route" labels for request paths - the alternative to grouping stats
// by raw path, which is unbounded cardinality on real traffic (paths
// carrying embedded IDs, tracking beacons, etc).
package routes

import (
	"fmt"
	"os"
	"regexp"

	"go.yaml.in/yaml/v3"
)

// Rule maps one compiled pattern to a label.
type Rule struct {
	Pattern *regexp.Regexp
	Label   string
}

// Rules is an ordered list of Rule; the first match wins.
type Rules []Rule

// fileFormat mirrors the on-disk YAML shape:
//
//	routes:
//	  - pattern: '^/gamecenter/game/'
//	    label: game_page
type fileFormat struct {
	Routes []struct {
		Pattern string `yaml:"pattern"`
		Label   string `yaml:"label"`
	} `yaml:"routes"`
}

// Load reads and parses a routes config file. Every pattern is compiled
// at load time, so a bad regex fails fast at startup rather than being
// silently skipped per-line later.
func Load(path string) (Rules, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading routes file %s: %w", path, err)
	}

	var ff fileFormat
	if err := yaml.Unmarshal(data, &ff); err != nil {
		return nil, fmt.Errorf("parsing routes file %s: %w", path, err)
	}
	if len(ff.Routes) == 0 {
		return nil, fmt.Errorf("routes file %s has no routes", path)
	}

	rules := make(Rules, 0, len(ff.Routes))
	for i, r := range ff.Routes {
		if r.Label == "" {
			return nil, fmt.Errorf("routes file %s: entry %d has no label", path, i)
		}
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			return nil, fmt.Errorf("routes file %s: entry %d (%q): invalid pattern: %w", path, i, r.Label, err)
		}
		rules = append(rules, Rule{Pattern: re, Label: r.Label})
	}
	return rules, nil
}

// Label returns the label of the first rule whose pattern matches path,
// or ("", false) if none do - the caller decides the fallback (e.g. an
// "other" bucket).
func (rs Rules) Label(path string) (string, bool) {
	for _, r := range rs {
		if r.Pattern.MatchString(path) {
			return r.Label, true
		}
	}
	return "", false
}
