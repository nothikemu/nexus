package localdb

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"time"
)

// tailAndFollow writes the last n lines of a file and, if follow is set,
// streams appended data until ctx is cancelled.
func tailAndFollow(ctx context.Context, path string, w io.Writer, n int, follow bool) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		if !follow {
			return nil
		}
		// Wait for the server to create its log.
		for errors.Is(err, os.ErrNotExist) {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(500 * time.Millisecond):
			}
			f, err = os.Open(path)
		}
	}
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := io.WriteString(w, tailFile(path, n)); err != nil {
		return err
	}
	if !follow {
		return nil
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	buf := make([]byte, 32*1024)
	for {
		nr, err := f.Read(buf)
		if nr > 0 {
			if _, werr := w.Write(buf[:nr]); werr != nil {
				return werr
			}
			continue
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// tailFile returns the last n lines of a file ("" if unreadable).
func tailFile(path string, n int) string {
	data, err := os.ReadFile(path)
	if err != nil || n <= 0 {
		return ""
	}
	data = bytes.TrimRight(data, "\n")
	if len(data) == 0 {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n") + "\n"
}
