// Package nginxconf locates a named log_format directive inside an nginx
// configuration tree, following include directives the way nginx itself
// would when it builds its effective config.
package nginxconf

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

var includeRe = regexp.MustCompile(`(?m)include\s+([^;\s][^;]*);`)

// LoadRawFormat reads confPath and everything it includes (recursively,
// globs and all) and returns the raw template string and escape=json flag
// for the log_format directive named formatName.
func LoadRawFormat(confPath, formatName string) (rawTemplate string, escapeJSON bool, err error) {
	corpus, err := buildCorpus(confPath)
	if err != nil {
		return "", false, err
	}
	template, escape, found, err := extractLogFormat(corpus, formatName)
	if err != nil {
		return "", false, err
	}
	if !found {
		return "", false, fmt.Errorf("log_format %q not found in %s (including its includes)", formatName, confPath)
	}
	return template, escape, nil
}

// buildCorpus concatenates the comment-stripped content of confPath and
// every file it (transitively) includes, in encounter order.
func buildCorpus(confPath string) (string, error) {
	visited := make(map[string]bool)
	var corpus []byte

	var load func(path string) error
	load = func(path string) error {
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if visited[abs] {
			return nil
		}
		visited[abs] = true

		raw, err := os.ReadFile(abs)
		if err != nil {
			return fmt.Errorf("reading %s: %w", abs, err)
		}
		stripped := stripComments(string(raw))
		corpus = append(corpus, stripped...)
		corpus = append(corpus, '\n')

		baseDir := filepath.Dir(abs)
		for _, m := range includeRe.FindAllStringSubmatch(stripped, -1) {
			pattern := trimQuotes(m[1])
			if !filepath.IsAbs(pattern) {
				pattern = filepath.Join(baseDir, pattern)
			}
			matches, err := filepath.Glob(pattern)
			if err != nil {
				return fmt.Errorf("expanding include %q: %w", pattern, err)
			}
			for _, inc := range matches {
				if err := load(inc); err != nil {
					return err
				}
			}
		}
		return nil
	}

	if err := load(confPath); err != nil {
		return "", err
	}
	return string(corpus), nil
}

func trimQuotes(s string) string {
	s = trimSpaceBoth(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func trimSpaceBoth(s string) string {
	i, j := 0, len(s)
	for i < j && isSpace(s[i]) {
		i++
	}
	for j > i && isSpace(s[j-1]) {
		j--
	}
	return s[i:j]
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n'
}

// stripComments removes nginx '#' comments (from an unquoted '#' to end of
// line) without touching '#' characters that appear inside quoted strings.
func stripComments(content string) string {
	out := make([]byte, 0, len(content))
	var quote byte
	for i := 0; i < len(content); i++ {
		c := content[i]
		if quote != 0 {
			out = append(out, c)
			if c == '\\' && i+1 < len(content) {
				out = append(out, content[i+1])
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
			out = append(out, c)
		case '#':
			for i < len(content) && content[i] != '\n' {
				i++
			}
			if i < len(content) {
				out = append(out, '\n')
			}
		default:
			out = append(out, c)
		}
	}
	return string(out)
}
