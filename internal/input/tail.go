package input

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// Follow tails path like `tail -f`: it starts at the current end of the
// file and calls onLine for each newly appended line until ctx is
// cancelled. If the file shrinks (log rotation, e.g. logrotate's
// copytruncate or a rename+recreate) it is reopened from the start.
func Follow(ctx context.Context, path string, pollInterval time.Duration, onLine func(string) error) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}
	offset := info.Size()
	var pending []byte
	buf := make([]byte, 64*1024)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}

		info, err := f.Stat()
		if err != nil {
			return err
		}
		size := info.Size()
		if size < offset {
			f.Close()
			f, err = os.Open(path)
			if err != nil {
				return fmt.Errorf("reopening %s after rotation: %w", path, err)
			}
			offset = 0
			pending = nil
			info, err = f.Stat()
			if err != nil {
				return err
			}
			size = info.Size()
		}

		for offset < size {
			n, rerr := f.ReadAt(buf, offset)
			if n > 0 {
				pending = append(pending, buf[:n]...)
				offset += int64(n)
				for {
					idx := bytes.IndexByte(pending, '\n')
					if idx < 0 {
						break
					}
					line := strings.TrimSuffix(string(pending[:idx]), "\r")
					pending = pending[idx+1:]
					if err := onLine(line); err != nil {
						return err
					}
				}
			}
			if rerr != nil && rerr != io.EOF {
				return rerr
			}
			if n == 0 {
				break
			}
		}
	}
}
