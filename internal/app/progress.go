package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"time"
)

// spinnerFrames are drawn while a quiet action runs in a terminal.
var spinnerFrames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

// progress reports the steps of a command. Its own lines are indented below
// "==>" step headers; output of wrapped tools is prefixed with the tool name.
type progress struct {
	w           io.Writer
	interactive bool
}

// step starts a new step.
func (p progress) step(format string, args ...any) {
	_, _ = fmt.Fprintf(p.w, "==> "+format+"\n", args...)
}

// info reports an intermediate message of the current step.
func (p progress) info(format string, args ...any) {
	_, _ = fmt.Fprintf(p.w, "  "+format+"\n", args...)
}

// done reports a completed part of the current step.
func (p progress) done(format string, args ...any) {
	_, _ = fmt.Fprintf(p.w, "  ✓ "+format+"\n", args...)
}

// warn reports something the user should act on.
func (p progress) warn(format string, args ...any) {
	_, _ = fmt.Fprintf(p.w, "  ! "+format+"\n", args...)
}

// wait runs a quiet action, showing a spinner with the title in a terminal.
// The action must not write to the output.
func (p progress) wait(ctx context.Context, title string, action func(context.Context) error) error {
	if !p.interactive {
		p.info("%s ...", title)
		return action(ctx)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for i := 0; ; i++ {
			_, _ = fmt.Fprintf(p.w, "\r\033[2K  %c %s", spinnerFrames[i%len(spinnerFrames)], title)
			select {
			case <-stop:
				_, _ = fmt.Fprint(p.w, "\r\033[2K")
				return
			case <-ticker.C:
			}
		}
	})
	err := action(ctx)
	close(stop)
	wg.Wait()
	return err
}

// tool returns a writer that prefixes every line with the tool name. Use the
// same writer for stdout and stderr of a process, so that lines do not
// interleave.
func (p progress) tool(name string) io.Writer {
	return &prefixWriter{w: p.w, prefix: []byte("  [" + name + "] "), lineStart: true}
}

// prefixWriter streams output and writes a prefix at the start of each line.
type prefixWriter struct {
	w         io.Writer
	prefix    []byte
	lineStart bool
}

func (pw *prefixWriter) Write(b []byte) (int, error) {
	for rest := b; len(rest) > 0; {
		if pw.lineStart {
			if _, err := pw.w.Write(pw.prefix); err != nil {
				return 0, err
			}
		}
		end := len(rest)
		if i := bytes.IndexByte(rest, '\n'); i >= 0 {
			end = i + 1
		}
		if _, err := pw.w.Write(rest[:end]); err != nil {
			return 0, err
		}
		pw.lineStart = rest[end-1] == '\n'
		rest = rest[end:]
	}
	return len(b), nil
}

// since formats the time elapsed since start for progress messages.
func since(start time.Time) string {
	return time.Since(start).Round(time.Second).String()
}
