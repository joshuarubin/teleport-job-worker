package commands

import (
	"github.com/spf13/cobra"

	"github.com/joshuarubin/teleport-job-worker/internal/client"
)

type status struct {
	cfg client.Config
}

func Status() *cobra.Command {
	var s status
	cmd := cobra.Command{
		Use:   "status [flags] job-id",
		Short: "Get the status of a job on the job-worker server",
		RunE: func(_ *cobra.Command, _ []string) error {
			// TODO(jrubin)
			return nil
		},
	}

	s.cfg.Flags(&cmd)

	return &cmd
}
