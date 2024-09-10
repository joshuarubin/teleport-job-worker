package commands

import (
	"github.com/spf13/cobra"

	"github.com/joshuarubin/teleport-job-worker/internal/client"
)

type start struct {
	cfg client.Config
}

func Start() *cobra.Command {
	var s start

	cmd := cobra.Command{
		Use:   "start [flags] -- command [args]...",
		Short: "Start a job on the job-worker server",
		RunE: func(_ *cobra.Command, _ []string) error {
			// TODO(jrubin)
			return nil
		},
	}

	s.cfg.Flags(&cmd)

	return &cmd
}
