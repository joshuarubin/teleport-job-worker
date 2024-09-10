package commands

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/joshuarubin/teleport-job-worker/internal/server"
)

type child struct {
	cfg server.Config
}

func Child() *cobra.Command {
	var c child
	cmd := cobra.Command{
		Use:    "child",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO(jrubin)
			fmt.Fprintf(cmd.ErrOrStderr(), "child args: %v\n", args)
			return nil
		},
	}

	c.cfg.Flags(&cmd)

	return &cmd
}
