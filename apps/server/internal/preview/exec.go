package preview

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	// renderTimeout bounds a single external render. pdftoppm and ffmpeg can
	// both be made to spin for a very long time on a malformed file; without a
	// deadline one bad upload parks a worker slot indefinitely.
	renderTimeout = 30 * time.Second
	// maxRenderOutput caps what we will read back from a child process. A
	// rasterizer pointed at a poster-sized PDF can emit hundreds of MB.
	maxRenderOutput = 16 << 20
	// maxRenderInput caps what we spool to disk before rendering. Seeking
	// renderers need a real file, but they only ever read the head.
	maxRenderInput = 512 << 20
)

// cappedBuffer collects output but refuses to grow past max.
//
// The cap has to bite *during* the write, not after: a renderer pointed at a
// hostile file can emit output without bound, and checking the size once the
// process has exited means the memory has already been allocated. Failing the
// write kills the pipe, which is how the child finds out to stop.
type cappedBuffer struct {
	buf bytes.Buffer
	max int
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if c.buf.Len()+len(p) > c.max {
		return 0, fmt.Errorf("output exceeded %d bytes", c.max)
	}
	return c.buf.Write(p)
}

// readCapped reads a file a renderer wrote, with the same bound as stdout.
// pdftoppm rasterizes to disk, so its output escapes the stdout cap entirely.
func readCapped(path string, max int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// One byte over the cap, so hitting it exactly is distinguishable from a
	// file that merely happens to be max bytes long.
	data, err := io.ReadAll(io.LimitReader(f, int64(max)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > max {
		return nil, fmt.Errorf("%s is larger than the %d byte cap", filepath.Base(path), max)
	}
	return data, nil
}

// hasBinary reports whether an executable is on PATH. Renderers use it at
// construction time so a process advertises only what it can actually run:
// an image built without ffmpeg registers no video renderer, claims no
// render:video job, and leaves that work pending for a worker that can.
func hasBinary(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// spool writes src to a temp file inside its own directory and returns the
// path plus a cleanup func. Both external renderers need a seekable file:
// pdftoppm reads the trailer to find the page tree, and ffmpeg's fast seek
// needs random access.
func spool(src io.Reader, pattern string) (path string, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "pp-render-")
	if err != nil {
		return "", nil, err
	}
	cleanup = func() { _ = os.RemoveAll(dir) }

	f, err := os.Create(filepath.Join(dir, pattern))
	if err != nil {
		cleanup()
		return "", nil, err
	}
	defer f.Close()

	n, err := io.Copy(f, io.LimitReader(src, maxRenderInput))
	if err != nil {
		cleanup()
		return "", nil, err
	}
	if n == 0 {
		cleanup()
		return "", nil, fmt.Errorf("empty source")
	}
	return f.Name(), cleanup, nil
}

// runCapture executes a renderer binary and returns its stdout.
//
// The context carries renderTimeout, and CommandContext kills the child when
// it expires. stderr is captured only to enrich the error: these binaries are
// chatty on stdout by design when asked to write an image there.
func runCapture(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, renderTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	stdout := &cappedBuffer{max: maxRenderOutput}
	var stderr bytes.Buffer
	cmd.Stdout = stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("%s timed out after %s", name, renderTimeout)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w: %s", name, err, firstLine(stderr.String()))
	}
	if stdout.buf.Len() == 0 {
		return nil, fmt.Errorf("%s produced no output: %s", name, firstLine(stderr.String()))
	}
	return stdout.buf.Bytes(), nil
}

// runQuiet executes a binary that writes to a file rather than stdout.
func runQuiet(ctx context.Context, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, renderTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() != nil {
		return fmt.Errorf("%s timed out after %s", name, renderTimeout)
	}
	if err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, firstLine(stderr.String()))
	}
	return nil
}

// firstLine trims a child's stderr to something loggable. These binaries can
// emit hundreds of lines of banner text on failure.
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "no stderr"
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
