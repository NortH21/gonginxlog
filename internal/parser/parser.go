// Package parser compiles a format.Spec into something that can turn raw
// log lines into record.Record values: a JSON decoder for escape=json
// formats, or a generated regexp for classic ones.
package parser

import (
	"fmt"

	"github.com/north21/gonginxlog/internal/format"
	"github.com/north21/gonginxlog/internal/record"
)

// Parser turns one raw log line into a Record.
type Parser interface {
	Parse(line string) (*record.Record, error)
}

// New compiles spec into a Parser.
func New(spec *format.Spec) (Parser, error) {
	if spec.IsJSON {
		return newJSONParser(spec)
	}
	return newRegexParser(spec)
}

var errEmptyLine = fmt.Errorf("empty line")
