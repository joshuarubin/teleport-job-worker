package worker

import (
	"syscall"
)

// mountProc mounts the /proc filesystem. it is in a separate linux file
// because syscall.Mount does not exist on all GOOS
func mountProc() error {
	// TODO(jrubin) does this need to be unmounted? is that possible after Exec?
	return syscall.Mount("proc", "/proc", "proc", 0, "")
}
