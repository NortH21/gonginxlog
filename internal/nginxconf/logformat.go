package nginxconf

import (
	"fmt"
	"regexp"
	"strings"
)

var logFormatKeywordRe = regexp.MustCompile(`\blog_format\b`)

// extractLogFormat scans corpus for a `log_format <name> [escape=json] "..." "..." ;`
// directive matching name and returns its concatenated, unquoted template.
// It keeps scanning past non-matching directives so later ones (e.g. from a
// later included file) are still found.
func extractLogFormat(corpus, name string) (template string, escapeJSON bool, found bool, err error) {
	pos := 0
	for {
		loc := logFormatKeywordRe.FindStringIndex(corpus[pos:])
		if loc == nil {
			return "", false, false, nil
		}
		start := pos + loc[1]

		i := skipSpace(corpus, start)
		nameStart := i
		for i < len(corpus) && isWordByte(corpus[i]) {
			i++
		}
		directiveName := corpus[nameStart:i]
		if directiveName == "" {
			return "", false, false, fmt.Errorf("malformed log_format directive near byte %d", start)
		}

		tmpl, escape, end, perr := parseDirectiveArgs(corpus, i)
		if perr != nil {
			return "", false, false, fmt.Errorf("parsing log_format %q: %w", directiveName, perr)
		}

		if directiveName == name {
			return tmpl, escape, true, nil
		}
		pos = end
	}
}

// parseDirectiveArgs parses everything after the format name up to the
// terminating ';', concatenating quoted string segments (with escapes
// resolved) into one template and detecting escape=json/escape=none.
func parseDirectiveArgs(s string, start int) (template string, escapeJSON bool, end int, err error) {
	var sb strings.Builder
	i := start
	for {
		i = skipSpace(s, i)
		if i >= len(s) {
			return "", false, 0, fmt.Errorf("unterminated directive (missing ';')")
		}
		switch s[i] {
		case ';':
			return sb.String(), escapeJSON, i + 1, nil
		case '"', '\'':
			quote := s[i]
			i++
			for i < len(s) && s[i] != quote {
				if s[i] == '\\' && i+1 < len(s) {
					sb.WriteByte(s[i+1])
					i += 2
					continue
				}
				sb.WriteByte(s[i])
				i++
			}
			if i >= len(s) {
				return "", false, 0, fmt.Errorf("unterminated quoted string")
			}
			i++ // skip closing quote
		default:
			tokStart := i
			for i < len(s) && !isSpaceByte(s[i]) && s[i] != ';' {
				i++
			}
			tok := s[tokStart:i]
			switch tok {
			case "escape=json":
				escapeJSON = true
			case "escape=none", "escape=default":
				escapeJSON = false
			}
		}
	}
}

func skipSpace(s string, i int) int {
	for i < len(s) && isSpaceByte(s[i]) {
		i++
	}
	return i
}

func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n'
}

func isWordByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}
