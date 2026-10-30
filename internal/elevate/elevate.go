// Package elevate re-executes wgo with root privileges when needed.
package elevate

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// Elevators tried in order.
var Elevators = []string{"sudo", "doas", "pkexec"}

// EnsureRoot re-execs the process via sudo/doas/pkexec when not running as
// root. It never returns on successful re-exec. Returns an error when no
// elevator is available; returns nil when already root.
func EnsureRoot() error {
	if os.Geteuid() == 0 {
		return nil
	}
	self, err := os.Executable()
	if err != nil {
		self = os.Args[0]
	}
	for _, tool := range Elevators {
		path, err := exec.LookPath(tool)
		if err != nil {
			continue
		}
		argv := append([]string{path, self}, os.Args[1:]...)
		// syscall.Exec replaces the process; the password prompt (if any)
		// happens in the normal terminal before the TUI starts.
		if err := syscall.Exec(path, argv, os.Environ()); err != nil {
			return fmt.Errorf("exec %s: %w", tool, err)
		}
	}
	return fmt.Errorf("wgo requires root; none of sudo, doas or pkexec were found in PATH")
}
