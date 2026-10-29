package execx

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Call records a single command invocation on the Mock runner.
type Call struct {
	Name  string
	Args  []string
	Stdin string
}

// String renders the call as a shell-like string for assertions.
func (c Call) String() string {
	return strings.TrimSpace(c.Name + " " + strings.Join(c.Args, " "))
}

// Mock is a Runner for tests. Register responses keyed by command prefix.
type Mock struct {
	mu        sync.Mutex
	calls     []Call
	responses map[string]Result
	errors    map[string]error
	missing   map[string]bool
}

// NewMock returns an empty mock runner.
func NewMock() *Mock {
	return &Mock{
		responses: map[string]Result{},
		errors:    map[string]error{},
		missing:   map[string]bool{},
	}
}

// Respond registers a Result for commands whose rendered string starts with prefix.
func (m *Mock) Respond(prefix string, res Result) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses[prefix] = res
}

// Fail registers an error for commands whose rendered string starts with prefix.
func (m *Mock) Fail(prefix string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errors[prefix] = err
}

// SetMissing marks a binary as absent from PATH.
func (m *Mock) SetMissing(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.missing[name] = true
}

// Calls returns a copy of recorded calls.
func (m *Mock) Calls() []Call {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Call, len(m.calls))
	copy(out, m.calls)
	return out
}

// CallStrings returns recorded calls rendered as strings.
func (m *Mock) CallStrings() []string {
	calls := m.Calls()
	out := make([]string, len(calls))
	for i, c := range calls {
		out[i] = c.String()
	}
	return out
}

func (m *Mock) Run(ctx context.Context, name string, args ...string) (Result, error) {
	return m.RunInput(ctx, "", name, args...)
}

func (m *Mock) RunInput(ctx context.Context, stdin string, name string, args ...string) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	call := Call{Name: name, Args: args, Stdin: stdin}
	m.calls = append(m.calls, call)
	key := call.String()
	// Longest-prefix match.
	var best string
	for p := range m.responses {
		if strings.HasPrefix(key, p) && len(p) > len(best) {
			best = p
		}
	}
	for p, err := range m.errors {
		if strings.HasPrefix(key, p) && len(p) >= len(best) {
			return Result{}, err
		}
	}
	if best != "" {
		return m.responses[best], nil
	}
	return Result{}, nil
}

func (m *Mock) LookPath(name string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.missing[name] {
		return "", fmt.Errorf("%s: executable file not found in $PATH", name)
	}
	return "/usr/bin/" + name, nil
}
