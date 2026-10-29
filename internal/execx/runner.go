// Package execx wraps external command execution behind a mockable interface.
package execx

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Result holds the outcome of a command execution.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Combined returns stdout and stderr concatenated.
func (r Result) Combined() string {
	return strings.TrimSpace(strings.TrimSpace(r.Stdout) + "\n" + strings.TrimSpace(r.Stderr))
}

// Runner executes external commands. Implementations must be safe for
// concurrent use.
type Runner interface {
	// Run executes name with args and returns the result. A non-zero exit
	// code is not an error; err is reserved for start/lookup failures.
	Run(ctx context.Context, name string, args ...string) (Result, error)
	// RunInput is like Run but feeds stdin to the process.
	RunInput(ctx context.Context, stdin string, name string, args ...string) (Result, error)
	// LookPath reports whether the binary exists in PATH, returning its path.
	LookPath(name string) (string, error)
}

// System is the real Runner backed by os/exec.
type System struct{}

// NewSystem returns a Runner that executes real commands.
func NewSystem() *System { return &System{} }

func (s *System) Run(ctx context.Context, name string, args ...string) (Result, error) {
	return s.RunInput(ctx, "", name, args...)
}

func (s *System) RunInput(ctx context.Context, stdin string, name string, args ...string) (Result, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	err := cmd.Run()
	res := Result{Stdout: out.String(), Stderr: errb.String()}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			res.ExitCode = ee.ExitCode()
			return res, nil
		}
		return res, fmt.Errorf("exec %s: %w", name, err)
	}
	return res, nil
}

func (s *System) LookPath(name string) (string, error) {
	return exec.LookPath(name)
}
