package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	"github.com/spf13/cobra"

	jobworker "github.com/joshuarubin/teleport-job-worker/pkg/proto/jobworker/v1"
)

// TLS contains the tls configuration passed in via cli flags
type TLS struct {
	CACertFileName string
	CertFileName   string
	KeyFileName    string
}

// Config contains all configuration passed in via cli flags
type Config struct {
	Addr            string
	TLS             TLS
	ShutdownTimeout time.Duration
	CPUMax          string
	MemoryMax       string
	IOMax           []string
}

const (
	DefaultShutdownTimeout  = 30 * time.Second
	DefaultKeepaliveTime    = 30 * time.Second
	DefaultKeepaliveTimeout = 20 * time.Second
	DefaultKeepaliveMinTime = 15 * time.Second
)

func (c *Config) Flags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&c.Addr, "listen-addr", ":8000", "listen address")

	const caCertFlag = "tls-ca-cert"
	cmd.Flags().StringVar(&c.TLS.CACertFileName, caCertFlag, "", "tls ca cert file name to use for validating client certificates (required)")
	_ = cmd.MarkFlagRequired(caCertFlag)

	const certFlag = "tls-cert"
	cmd.Flags().StringVar(&c.TLS.CertFileName, certFlag, "", "tls server certificate file name (required)")
	_ = cmd.MarkFlagRequired(certFlag)

	const keyFlag = "tls-key"
	cmd.Flags().StringVar(&c.TLS.KeyFileName, keyFlag, "", "tls server key file name (required)")
	_ = cmd.MarkFlagRequired(keyFlag)

	cmd.Flags().DurationVar(&c.ShutdownTimeout, "shutdown-timeout", DefaultShutdownTimeout, "time to wait for connections to close before forcing shutdown")

	cmd.Flags().StringVar(&c.CPUMax, "max-cpu", "25000 100000", "cpu.max value to set in cgroup for each job")
	cmd.Flags().StringVar(&c.MemoryMax, "max-memory", "128M", "memory.max value to set in cgroup for each job")
	cmd.Flags().StringSliceVar(&c.IOMax, "max-io", []string{}, "io.max value to set in cgroup for each job")
}

type Server struct {
	jobworker.UnimplementedJobWorkerServiceServer

	cfg    *Config
	s      *grpc.Server
	health *health.Server
}

func New(cfg *Config) (*Server, error) {
	srv := Server{
		cfg: cfg,
	}

	tlsConfig, err := srv.tlsConfig()
	if err != nil {
		return nil, err
	}

	srv.s = grpc.NewServer(
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    DefaultKeepaliveTime,
			Timeout: DefaultKeepaliveTimeout,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             DefaultKeepaliveMinTime,
			PermitWithoutStream: true,
		}),
		grpc.Creds(credentials.NewTLS(tlsConfig)),
	)
	srv.health = health.NewServer()
	healthpb.RegisterHealthServer(srv.s, srv.health)
	reflection.Register(srv.s)

	jobworker.RegisterJobWorkerServiceServer(srv.s, &srv)

	return &srv, nil
}

func (s *Server) tlsConfig() (*tls.Config, error) {
	crt, err := tls.LoadX509KeyPair(s.cfg.TLS.CertFileName, s.cfg.TLS.KeyFileName)
	if err != nil {
		return nil, fmt.Errorf("error loading server keypair: %w", err)
	}

	caCert, err := os.ReadFile(s.cfg.TLS.CACertFileName)
	if err != nil {
		return nil, fmt.Errorf("error loading ca-cert file: %w", err)
	}

	clientCAs := x509.NewCertPool()
	clientCAs.AppendCertsFromPEM(caCert)

	return &tls.Config{
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCAs,
		Certificates: []tls.Certificate{crt},
		MinVersion:   tls.VersionTLS13,
	}, nil
}

func (s *Server) Serve() error {
	lis, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return err
	}

	slog.Info("listening", "addr", lis.Addr())

	return s.s.Serve(lis)
}

func (s *Server) Stop() {
	s.s.Stop()
}

func (s *Server) GracefulStop() {
	s.s.GracefulStop()
}

func (s *Server) StartJob(ctx context.Context, _ *jobworker.StartJobRequest) (*jobworker.StartJobResponse, error) {
	peer, ok := peer.FromContext(ctx)
	if ok && peer.AuthInfo != nil {
		if tlsInfo, ok := peer.AuthInfo.(credentials.TLSInfo); ok {
			for _, v := range tlsInfo.State.PeerCertificates {
				fmt.Printf("client cert subject: %s\n", v.Subject)
			}
		}
	}

	return nil, status.Errorf(codes.Unimplemented, "method StartJob not implemented")
}

func (s *Server) StopJob(context.Context, *jobworker.StopJobRequest) (*jobworker.StopJobResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method StopJob not implemented")
}

func (s *Server) JobStatus(context.Context, *jobworker.JobStatusRequest) (*jobworker.JobStatusResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method JobStatus not implemented")
}

func (s *Server) StreamJobOutput(*jobworker.StreamJobOutputRequest, grpc.ServerStreamingServer[jobworker.StreamJobOutputResponse]) error {
	return status.Errorf(codes.Unimplemented, "method StreamJobOutput not implemented")
}
