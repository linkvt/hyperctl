// Package processtest provides a process.Runner for tests that records
// commands instead of running them.
package processtest

import (
	"context"
	"io"

	"github.com/linkvt/hyperctl/internal/process"
)

// Call is a recorded command without its output writers.
type Call struct {
	Path string
	Args []string
	Env  []string
}

// Result is the canned outcome of a command.
type Result struct {
	Stdout string
	Stderr string
	Err    error
}

// Recorder records commands and answers them with Results in order. Commands
// beyond Results succeed without output.
type Recorder struct {
	Results []Result
	Calls   []Call
}

// Run records the command and writes its canned output.
func (r *Recorder) Run(_ context.Context, c process.Command) error {
	r.Calls = append(r.Calls, Call{Path: c.Path, Args: c.Args, Env: c.Env})
	var result Result
	if i := len(r.Calls) - 1; i < len(r.Results) {
		result = r.Results[i]
	}
	if c.Stdout != nil {
		_, _ = io.WriteString(c.Stdout, result.Stdout)
	}
	if c.Stderr != nil {
		_, _ = io.WriteString(c.Stderr, result.Stderr)
	}
	return result.Err
}
