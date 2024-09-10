package commands

import (
	"github.com/spf13/cobra"

	"github.com/joshuarubin/teleport-job-worker/internal/client"
)

type stop struct {
	cfg client.Config
}

func Stop() *cobra.Command {
	var s stop
	cmd := cobra.Command{
		Use:   "stop [flags] job-id",
		Short: "Stop a job on the job-worker server",
		RunE: func(_ *cobra.Command, _ []string) error {
			// TODO(jrubin)
			return nil
		},
	}

	s.cfg.Flags(&cmd)

	return &cmd
}
