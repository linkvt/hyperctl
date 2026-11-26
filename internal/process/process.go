// Package process runs external commands behind a seam that tests replace
// with a recorder, see processtest.
package process

import (
	"context"
	"io"
	"os"
	"os/exec"
)

// Command is an external command to run.
type Command struct {
	Path string
	Args []string
	// Env is added to the environment of the hyperctl process.
	Env    []string
	Stdout io.Writer
	Stderr io.Writer
}

// Runner runs commands.
type Runner interface {
	Run(ctx context.Context, cmd Command) error
}

// Exec runs commands as processes.
type Exec struct{}

// Run runs the command and waits for it to finish.
func (Exec) Run(ctx context.Context, c Command) error {
	cmd := exec.CommandContext(ctx, c.Path, c.Args...)
	if len(c.Env) > 0 {
		cmd.Env = append(os.Environ(), c.Env...)
	}
	cmd.Stdout = c.Stdout
	cmd.Stderr = c.Stderr
	return cmd.Run()
}
