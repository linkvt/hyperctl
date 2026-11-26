package process_test

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"testing"

	"github.com/linkvt/hyperctl/internal/process"
)

func TestExecRun(t *testing.T) {
	t.Setenv("PROCESS_TEST_INHERITED", "inherited")
	var stdout, stderr bytes.Buffer

	err := process.Exec{}.Run(context.Background(), process.Command{
		Path:   "/bin/sh",
		Args:   []string{"-c", `echo "$PROCESS_TEST_INHERITED $PROCESS_TEST_ADDED $1"; echo err >&2; exit 3`, "sh", "arg"},
		Env:    []string{"PROCESS_TEST_ADDED=added"},
		Stdout: &stdout,
		Stderr: &stderr,
	})

	if exitErr, ok := errors.AsType[*exec.ExitError](err); !ok || exitErr.ExitCode() != 3 {
		t.Errorf("expected exit code 3, got %v", err)
	}
	if got, want := stdout.String(), "inherited added arg\n"; got != want {
		t.Errorf("stdout: got %q, want %q", got, want)
	}
	if got, want := stderr.String(), "err\n"; got != want {
		t.Errorf("stderr: got %q, want %q", got, want)
	}
}
