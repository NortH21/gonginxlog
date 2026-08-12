package tui

import "github.com/north21/gonginxlog/internal/record"

// Entry is one item kept in the ring buffer.
type Entry struct {
	Record *record.Record
	Raw    string
}

// RingBuffer is a fixed-capacity circular buffer of Entry, oldest
// overwritten first once full. Backs the raw-log view and drill-down;
// it is not used for the running totals shown in the stat views (those
// come from the live stats.Aggregator, which never forgets).
type RingBuffer struct {
	entries []Entry
	next    int
	size    int
}

// NewRingBuffer creates a buffer holding at most capacity entries.
func NewRingBuffer(capacity int) *RingBuffer {
	if capacity <= 0 {
		capacity = 1
	}
	return &RingBuffer{entries: make([]Entry, capacity)}
}

// Add appends e, overwriting the oldest entry once the buffer is full.
func (b *RingBuffer) Add(e Entry) {
	b.entries[b.next] = e
	b.next = (b.next + 1) % len(b.entries)
	if b.size < len(b.entries) {
		b.size++
	}
}

// Reset empties the buffer without reallocating it.
func (b *RingBuffer) Reset() {
	b.next = 0
	b.size = 0
}

// Cap returns the buffer's fixed capacity.
func (b *RingBuffer) Cap() int {
	return len(b.entries)
}

// Len returns the number of entries currently held (<= Cap).
func (b *RingBuffer) Len() int {
	return b.size
}

// Entries returns all currently held entries, oldest first.
func (b *RingBuffer) Entries() []Entry {
	out := make([]Entry, 0, b.size)
	if b.size < len(b.entries) {
		return append(out, b.entries[:b.size]...)
	}
	out = append(out, b.entries[b.next:]...)
	out = append(out, b.entries[:b.next]...)
	return out
}
