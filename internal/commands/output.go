package commands

import (
	"github.com/spf13/cobra"

	"github.com/joshuarubin/teleport-job-worker/internal/client"
)

type output struct {
	cfg client.Config
}

func Output() *cobra.Command {
	var o output
	cmd := cobra.Command{
		Use:   "output [flags] job-id",
		Short: "Stream the output of a job on the job-worker server",
		RunE: func(_ *cobra.Command, _ []string) error {
			// TODO(jrubin)
			return nil
		},
	}

	o.cfg.Flags(&cmd)

	return &cmd
}
