package parser

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/north21/gonginxlog/internal/format"
	"github.com/north21/gonginxlog/internal/record"
)

type jsonParser struct {
	keyToVar map[string]string
}

func newJSONParser(spec *format.Spec) (*jsonParser, error) {
	m := make(map[string]string, len(spec.Fields))
	for _, f := range spec.Fields {
		m[f.JSONKey] = f.Variable
	}
	return &jsonParser{keyToVar: m}, nil
}

func (p *jsonParser) Parse(line string) (*record.Record, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, errEmptyLine
	}

	dec := json.NewDecoder(strings.NewReader(line))
	dec.UseNumber()
	var raw map[string]interface{}
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("invalid json line: %w", err)
	}

	fields := make(map[string]string, len(raw))
	for key, value := range raw {
		variable, ok := p.keyToVar[key]
		if !ok {
			continue
		}
		fields[variable] = stringifyJSONValue(value)
	}
	return &record.Record{Fields: fields, Raw: line}, nil
}

func stringifyJSONValue(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case json.Number:
		return t.String()
	default:
		return fmt.Sprintf("%v", t)
	}
}
