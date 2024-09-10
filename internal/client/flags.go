package client

import "github.com/spf13/cobra"

// TLS contains the tls configuration passed in via cli flags
type TLS struct {
	CACertFileName string
	CertFileName   string
	KeyFileName    string
}

// Config contains all configuration passed in via cli flags
type Config struct {
	Addr string
	TLS  TLS
}

func (c *Config) Flags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&c.Addr, "addr", ":8000", "server address")

	const caCertFlag = "tls-ca-cert"
	cmd.Flags().StringVar(&c.TLS.CACertFileName, caCertFlag, "", "tls ca cert file name to use for validating server certificate")

	const certFlag = "tls-cert"
	cmd.Flags().StringVar(&c.TLS.CertFileName, certFlag, "", "tls client certificate file name (required)")
	_ = cmd.MarkFlagRequired(certFlag)

	const keyFlag = "tls-key"
	cmd.Flags().StringVar(&c.TLS.KeyFileName, keyFlag, "", "tls client key file name (required)")
	_ = cmd.MarkFlagRequired(keyFlag)
}
