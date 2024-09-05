//go:build !linux
// +build !linux

package job

import "syscall"

func sysProcAttr() *syscall.SysProcAttr {
	return nil
}
