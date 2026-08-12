package output

import (
	"encoding/json"
	"io"

	"github.com/north21/gonginxlog/internal/stats"
)

// WriteJSON renders rep as indented JSON to w.
func WriteJSON(w io.Writer, rep *stats.Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rep)
}
