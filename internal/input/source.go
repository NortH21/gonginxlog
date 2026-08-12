// Package input turns CLI file arguments (plain files, .gz rotated logs,
// "-" for stdin) into a single byte stream, plus a tail -f style follower
// for live mode.
package input

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"strings"
)

// Open concatenates paths (in order) into one ReadCloser. "-" reads stdin.
// Paths ending in .gz are transparently decompressed.
func Open(paths []string) (io.ReadCloser, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("no input files given")
	}

	var readers []io.Reader
	var closers []io.Closer
	for _, p := range paths {
		if p == "-" {
			readers = append(readers, os.Stdin)
			continue
		}
		f, err := os.Open(p)
		if err != nil {
			closeAll(closers)
			return nil, fmt.Errorf("opening %s: %w", p, err)
		}
		closers = append(closers, f)
		if strings.HasSuffix(p, ".gz") {
			gz, err := gzip.NewReader(f)
			if err != nil {
				closeAll(closers)
				return nil, fmt.Errorf("opening gzip %s: %w", p, err)
			}
			closers = append(closers, gz)
			readers = append(readers, gz)
			continue
		}
		readers = append(readers, f)
	}

	return &multiCloser{Reader: io.MultiReader(readers...), closers: closers}, nil
}

type multiCloser struct {
	io.Reader
	closers []io.Closer
}

func (m *multiCloser) Close() error {
	return closeAll(m.closers)
}

func closeAll(closers []io.Closer) error {
	var firstErr error
	for i := len(closers) - 1; i >= 0; i-- {
		if err := closers[i].Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// NewLineScanner wraps r in a bufio.Scanner with a generous buffer, since
// nginx log lines (cookies, long user agents, referers...) can exceed the
// default 64KiB token limit.
func NewLineScanner(r io.Reader) *bufio.Scanner {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	return sc
}
