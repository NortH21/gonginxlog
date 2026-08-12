package record

import "testing"

func TestPathWithoutQueryTrimsAtFirstQuestionMark(t *testing.T) {
	r := &Record{Fields: map[string]string{"request": "GET /api/profile.php?stand=0&id=123 HTTP/1.1"}}
	if got := r.PathWithoutQuery(); got != "/api/profile.php" {
		t.Fatalf("PathWithoutQuery() = %q, want %q", got, "/api/profile.php")
	}
}

func TestPathWithoutQueryLeavesPlainPathUnchanged(t *testing.T) {
	r := &Record{Fields: map[string]string{"request": "GET /foo/bar HTTP/1.1"}}
	if got := r.PathWithoutQuery(); got != "/foo/bar" {
		t.Fatalf("PathWithoutQuery() = %q, want %q", got, "/foo/bar")
	}
}

func TestPathWithoutQueryOnEmptyRequest(t *testing.T) {
	r := &Record{Fields: map[string]string{}}
	if got := r.PathWithoutQuery(); got != "" {
		t.Fatalf("PathWithoutQuery() = %q, want empty", got)
	}
}
