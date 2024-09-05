//go:build !linux
// +build !linux

package jobworker

func (w *JobWorker) prepJobEnv() error {
	return nil
}
