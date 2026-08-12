// Package format turns an nginx log_format template string into a Spec
// that the parser package can compile into an actual line parser.
package format

// FieldDef is one field of a log_format template. Variable is always the
// bare nginx variable name (no leading '$'). JSONKey is set only for JSON
// templates and holds the JSON object key the value was published under,
// which may differ from Variable (e.g. "x-request-id" for $request_id).
type FieldDef struct {
	JSONKey  string
	Variable string
}

// Spec describes a compiled-enough view of a log_format directive: whether
// it produces JSON per line, and which fields it carries.
type Spec struct {
	Name    string
	IsJSON  bool
	Fields  []FieldDef
	Literal string // the raw, concatenated template string (for non-JSON regex building)
}
