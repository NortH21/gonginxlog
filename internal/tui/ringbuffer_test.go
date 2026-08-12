package tui

import "testing"

func TestRingBufferWraparoundOrder(t *testing.T) {
	b := NewRingBuffer(3)
	for i := 0; i < 5; i++ {
		b.Add(Entry{Raw: string(rune('a' + i))}) // a, b, c, d, e
	}
	// Capacity 3, 5 added: only the last 3 (c, d, e) should remain, oldest first.
	got := b.Entries()
	if len(got) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(got))
	}
	want := []string{"c", "d", "e"}
	for i, w := range want {
		if got[i].Raw != w {
			t.Fatalf("entry %d: got %q, want %q (full: %+v)", i, got[i].Raw, w, got)
		}
	}
}

func TestRingBufferNotYetFull(t *testing.T) {
	b := NewRingBuffer(5)
	b.Add(Entry{Raw: "a"})
	b.Add(Entry{Raw: "b"})
	got := b.Entries()
	if len(got) != 2 || got[0].Raw != "a" || got[1].Raw != "b" {
		t.Fatalf("unexpected entries: %+v", got)
	}
	if b.Len() != 2 || b.Cap() != 5 {
		t.Fatalf("unexpected Len/Cap: %d/%d", b.Len(), b.Cap())
	}
}

func TestRingBufferReset(t *testing.T) {
	b := NewRingBuffer(3)
	b.Add(Entry{Raw: "a"})
	b.Add(Entry{Raw: "b"})
	b.Reset()
	if b.Len() != 0 || len(b.Entries()) != 0 {
		t.Fatalf("expected empty buffer after Reset, got Len=%d Entries=%v", b.Len(), b.Entries())
	}
	b.Add(Entry{Raw: "z"})
	got := b.Entries()
	if len(got) != 1 || got[0].Raw != "z" {
		t.Fatalf("unexpected entries after reset+add: %+v", got)
	}
}
