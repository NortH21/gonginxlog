package parser

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/north21/gonginxlog/internal/format"
	"github.com/north21/gonginxlog/internal/record"
)

var variableRe = regexp.MustCompile(`\$([A-Za-z_][A-Za-z0-9_]*)`)

type regexParser struct {
	re *regexp.Regexp
}

// newRegexParser turns a plain (non-JSON) log_format template into an
// anchored regexp with one named capture group per $variable, and literal
// separators escaped and matched verbatim in between.
func newRegexParser(spec *format.Spec) (*regexParser, error) {
	template := spec.Literal
	matches := variableRe.FindAllStringSubmatchIndex(template, -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("log_format %q has no $variables", spec.Name)
	}

	var sb strings.Builder
	sb.WriteString(`^`)
	last := 0
	seen := map[string]int{}
	for _, m := range matches {
		sb.WriteString(regexp.QuoteMeta(template[last:m[0]]))
		varName := template[m[2]:m[3]]
		seen[varName]++
		groupName := varName
		if seen[varName] > 1 {
			groupName = fmt.Sprintf("%s_%d", varName, seen[varName])
		}
		sb.WriteString(fmt.Sprintf(`(?P<%s>.*?)`, groupName))
		last = m[1]
	}
	sb.WriteString(regexp.QuoteMeta(template[last:]))
	sb.WriteString(`$`)

	re, err := regexp.Compile(sb.String())
	if err != nil {
		return nil, fmt.Errorf("building regexp for log_format %q: %w", spec.Name, err)
	}
	return &regexParser{re: re}, nil
}

func (p *regexParser) Parse(line string) (*record.Record, error) {
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return nil, errEmptyLine
	}
	m := p.re.FindStringSubmatch(line)
	if m == nil {
		return nil, fmt.Errorf("line does not match the configured log_format")
	}
	fields := make(map[string]string, len(m))
	for i, name := range p.re.SubexpNames() {
		if i == 0 || name == "" {
			continue
		}
		canonical := stripDupSuffix(name)
		if _, exists := fields[canonical]; !exists {
			fields[canonical] = m[i]
		}
	}
	return &record.Record{Fields: fields, Raw: line}, nil
}

// stripDupSuffix undoes the "_2", "_3", ... suffix added to repeated
// variable names so their values map back to the canonical variable.
func stripDupSuffix(name string) string {
	idx := strings.LastIndex(name, "_")
	if idx <= 0 {
		return name
	}
	suffix := name[idx+1:]
	if suffix == "" {
		return name
	}
	for _, c := range suffix {
		if c < '0' || c > '9' {
			return name
		}
	}
	return name[:idx]
}
