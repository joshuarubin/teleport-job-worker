//go:build !linux

package worker

// mountProc is here for all non-linux builds but does nothing and exists only
// to make builds work
func mountProc() error {
	return nil
}
