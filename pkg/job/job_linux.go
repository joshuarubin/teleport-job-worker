package job

import "syscall"

func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWPID | // New pid namespace
			syscall.CLONE_NEWNS | // New mount namespace group
			syscall.CLONE_NEWNET, // New network namespace
		Unshareflags: syscall.CLONE_NEWNS, // Isolate process mounts from host
	}
}
