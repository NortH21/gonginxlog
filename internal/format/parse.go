package format

import (
	"fmt"
	"regexp"
	"strings"
)

// jsonFieldRe matches `"key":"$variable"` or `"key":$variable` pairs inside
// a JSON-shaped log_format template (escape=json).
var jsonFieldRe = regexp.MustCompile(`"([A-Za-z0-9_.\-]+)"\s*:\s*"?\$([A-Za-z_][A-Za-z0-9_]*)"?`)

// variableRe matches any $variable reference, used to enumerate the fields
// of a non-JSON (plain) log_format template.
var variableRe = regexp.MustCompile(`\$([A-Za-z_][A-Za-z0-9_]*)`)

// ParseTemplate builds a Spec from the raw, already-concatenated
// log_format template string (the quoted segments joined together, as
// nginx would see them). escapeJSON should be true when the directive was
// declared with `escape=json`.
func ParseTemplate(name, rawTemplate string, escapeJSON bool) (*Spec, error) {
	template := strings.TrimSpace(rawTemplate)
	if template == "" {
		return nil, fmt.Errorf("log_format %q has an empty template", name)
	}

	isJSON := escapeJSON || looksLikeJSON(template)

	spec := &Spec{Name: name, IsJSON: isJSON, Literal: template}

	if isJSON {
		matches := jsonFieldRe.FindAllStringSubmatch(template, -1)
		if len(matches) == 0 {
			return nil, fmt.Errorf("log_format %q looks like JSON but no \"key\":\"$var\" pairs were found", name)
		}
		for _, m := range matches {
			spec.Fields = append(spec.Fields, FieldDef{JSONKey: m[1], Variable: m[2]})
		}
		return spec, nil
	}

	matches := variableRe.FindAllStringSubmatch(template, -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("log_format %q has no $variables in it", name)
	}
	for _, m := range matches {
		spec.Fields = append(spec.Fields, FieldDef{Variable: m[1]})
	}
	return spec, nil
}

func looksLikeJSON(template string) bool {
	return strings.HasPrefix(template, "{") && strings.HasSuffix(template, "}")
}
