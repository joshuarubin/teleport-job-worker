package jobworker

import (
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

func (w *JobWorker) prepJobEnv() error {
	if err := w.cgroup.createLeaf(); err != nil {
		return err
	}

	// TODO(jrubin) does this need to be unmounted? is that possible after Exec?
	return syscall.Mount("proc", "/proc", "proc", 0, "")
}

const filePerm = 0o400

// createRoot creates the root cgroup. this is only done once. sets the proper
// values on cgroup.subtree_control so that cpu, memory and io can be managed on
// leaf cgroups.
func (g *cgroup) createRoot() error {
	// Requires cgroup v2.
	const prefix = "/sys/fs/cgroup"

	cg, err := os.MkdirTemp(prefix, "job-worker-")
	if err != nil {
		return err
	}

	g.name = cg

	return os.WriteFile(filepath.Join(cg, "cgroup.subtree_control"), []byte("+cpu +memory +io"), filePerm)
}

// createLeaf creates a cgroup inside the container. it creates the root group
// first if it hasn't yet been created. once the leaf has been created, it sets
// the values for cpu.max, memory.max and io.max. it also adds the current
// process's pid to cgroup.procs.
func (g *cgroup) createLeaf() error {
	g.createOnce.Do(func() {
		g.createErr = g.createRoot()
	})
	if g.createErr != nil {
		return g.createErr
	}

	subCgroup, err := os.MkdirTemp(g.name, "job-")
	if err != nil {
		return err
	}

	type cgroupValue struct {
		file  string
		value string
	}

	cgroupData := []cgroupValue{{
		file: "cgroup.procs", value: strconv.Itoa(os.Getpid()),
	}}

	if v := g.cfg.CPUMax; v != "" {
		cgroupData = append(cgroupData, cgroupValue{
			file: "cpu.max", value: v,
		})
	}

	if v := g.cfg.MemoryMax; v != "" {
		cgroupData = append(cgroupData, cgroupValue{
			file: "memory.max", value: v,
		})
	}

	for _, v := range g.cfg.IOMax {
		if v != "" {
			cgroupData = append(cgroupData, cgroupValue{
				file: "io.max", value: v,
			})
		}
	}

	for _, v := range cgroupData {
		if err = os.WriteFile(filepath.Join(subCgroup, v.file), []byte(v.value), filePerm); err != nil {
			return err
		}
	}

	return nil
}
