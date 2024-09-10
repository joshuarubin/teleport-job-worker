package main

import (
	"context"
	"errors"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/joshuarubin/teleport-job-worker/internal/commands"
)

func main() {
	if err := run(); err != nil {
		if code, ok := exitCode(err); ok {
			os.Exit(code)
		}

		os.Exit(1)
	}
}

func run() error {
	root := cobra.Command{
		Use:   "job-worker",
		Short: "A prototype job worker service that provides an api to run arbitrary linux processes",

		// silence these because when the reexecuted as a child, we don't want
		// to show cobra usage errors or additional error output from the job
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(commands.Child())
	root.AddCommand(commands.Output())
	root.AddCommand(commands.Serve())
	root.AddCommand(commands.Start())
	root.AddCommand(commands.Status())
	root.AddCommand(commands.Stop())

	ctx := context.Background()

	cmd, err := root.ExecuteContextC(ctx)
	if _, ok := exitCode(err); ok {
		// we have a proper exit code from the command
		return err
	}

	if err != nil {
		// this is copied from cobra so that for everything but a non-zero exit
		// code from a command we still want to show usage errors and additional
		// error output
		root.Println(cmd.UsageString())
		root.PrintErrln(root.ErrPrefix(), err.Error())
	}

	return err
}

func exitCode(err error) (int, bool) {
	var eerr *exec.ExitError
	if errors.As(err, &eerr) {
		return eerr.ExitCode(), true
	}
	return 0, false
}
