package commands

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/joshuarubin/teleport-job-worker/internal/server"
)

type serve struct {
	cfg server.Config
	srv *server.Server
}

func Serve() *cobra.Command {
	var s serve

	cmd := cobra.Command{
		Use:   "serve",
		Short: "Start the job-worker server and listen for connections",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return s.serve(cmd.Context())
		},
	}

	s.cfg.Flags(&cmd)

	return &cmd
}

func (s *serve) serve(ctx context.Context) error {
	var err error
	if s.srv, err = server.New(&s.cfg); err != nil {
		return err
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	done := make(chan struct{})

	go func() {
		defer close(done)
		err = s.srv.Serve()
	}()

	select {
	case <-done:
		return err
	case sig := <-sigCh:
		slog.Warn("caught signal", "sig", sig)
		return s.gracefulStop()
	case <-ctx.Done():
		slog.Warn("application context done", "err", ctx.Err())
		return s.gracefulStop()
	}
}

func (s *serve) gracefulStop() error {
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout)
	defer cancel()

	done := make(chan struct{})

	go func() {
		defer close(done)
		s.srv.GracefulStop()
	}()

	select {
	case <-done:
		slog.Info("shutdown gracefully")
		return nil
	case <-ctx.Done():
		slog.Warn("timed out waiting to shutdown")
		return ctx.Err()
	}
}
