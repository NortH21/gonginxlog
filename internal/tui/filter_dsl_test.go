package tui

import (
	"testing"

	"github.com/north21/gonginxlog/internal/record"
)

func TestParseFilterExprEmpty(t *testing.T) {
	and, err := ParseFilterExpr("   ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rec := &record.Record{Fields: map[string]string{"status": "500"}}
	if !and.Match(rec) {
		t.Fatalf("empty filter expression should match everything")
	}
}

func TestParseFilterExprStatusAndIP(t *testing.T) {
	and, err := ParseFilterExpr("status:5xx ip:203.0.113.5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	match := &record.Record{Fields: map[string]string{"status": "500", "remote_addr": "203.0.113.5"}}
	if !and.Match(match) {
		t.Fatalf("expected match for %+v", match.Fields)
	}
	noMatch := &record.Record{Fields: map[string]string{"status": "200", "remote_addr": "203.0.113.5"}}
	if and.Match(noMatch) {
		t.Fatalf("expected no match for %+v (wrong status)", noMatch.Fields)
	}
}

func TestParseFilterExprFieldEquals(t *testing.T) {
	and, err := ParseFilterExpr("http_user_agent=bot")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rec := &record.Record{Fields: map[string]string{"http_user_agent": "evilbot/1.0"}}
	if !and.Match(rec) {
		t.Fatalf("expected field=regexp match")
	}
}

func TestParseFilterExprUnrecognizedToken(t *testing.T) {
	if _, err := ParseFilterExpr("garbage"); err == nil {
		t.Fatalf("expected an error for a token with no prefix and no '='")
	}
}

func TestParseFilterExprInvalidStatus(t *testing.T) {
	if _, err := ParseFilterExpr("status:notanumber"); err == nil {
		t.Fatalf("expected an error for an invalid status spec")
	}
}
