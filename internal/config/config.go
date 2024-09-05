package config

import "time"

type TLS struct {
	CACertFile string
	CertFile   string
	KeyFile    string
}

type Config struct {
	Addr            string
	TLS             TLS
	ShutdownTimeout time.Duration
	CPUMax          string
	MemoryMax       string
	IOMax           []string
}
