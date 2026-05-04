// Package config holds application configuration loaded from flags or environment.
package config

import (
	"fmt"
	"path/filepath"
)

const (
	defaultTmpDir    = "/tmp/concierge"
	defaultSizeLimit = 5 * 1024 * 1024 // 5 MiB
	defaultPort      = "8080"
	defaultTokenPath = "/secret/token"
)

// Config is runtime configuration for the Concierge server.
type Config struct {
	TmpDir     string
	SizeLimit  int
	Port       string
	Release    bool
	TokenPath  string
	ActiveRefs string
	LockFile   string
}

// Validate checks required fields and returns an error if the config is unusable.
func (c *Config) Validate() error {
	c.DerivePaths()
	if c.TmpDir == "" {
		return fmt.Errorf("tmpDir is required")
	}
	if c.SizeLimit <= 0 {
		return fmt.Errorf("sizeLimit must be positive")
	}
	if c.Port == "" {
		return fmt.Errorf("port is required")
	}
	return nil
}

// Default returns baseline defaults before flag parsing overrides them.
func Default() Config {
	return Config{
		TmpDir:    defaultTmpDir,
		SizeLimit: defaultSizeLimit,
		Port:      defaultPort,
		Release:   false,
		TokenPath: defaultTokenPath,
	}
}

// DerivePaths sets ActiveRefs and LockFile from TmpDir. Call after TmpDir is final.
func (c *Config) DerivePaths() {
	c.ActiveRefs = filepath.Join(c.TmpDir, "active_refs.yaml")
	c.LockFile = filepath.Join(c.TmpDir, "active_refs.lock")
}
