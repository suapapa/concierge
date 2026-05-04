// Package config holds application configuration loaded from flags or environment.
package config

import (
	"fmt"
	"path/filepath"
	"time"
)

const (
	defaultTmpDir    = "/tmp/concierge"
	defaultSizeLimit = 5 * 1024 * 1024 // 5 MiB
	defaultPort      = "8080"
	defaultTokenPath = "/secret/token"
	defaultSessionTTL = 7 * 24 * time.Hour
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

	// DatabaseURL enables Google OAuth + DB-backed sessions. Empty keeps legacy bearer-only auth for mutating routes.
	DatabaseURL string
	// Google OAuth (required when DatabaseURL is set).
	GoogleClientID     string
	GoogleClientSecret string
	OAuthRedirectURL   string
	// SessionSecret is used for future crypto; when DatabaseURL is set it must be at least 16 bytes.
	SessionSecret string
	// BootstrapAdminEmails: first-time users with these emails (lowercased) get role admin.
	BootstrapAdminEmails []string
	SessionTTL           time.Duration
	// PostLoginRedirect is the path or URL users hit after successful OAuth (default "/").
	PostLoginRedirect string
	// CookieSecure sets the Secure flag on auth cookies (use true behind HTTPS).
	CookieSecure bool
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
	if c.DatabaseURL != "" {
		if c.GoogleClientID == "" || c.GoogleClientSecret == "" || c.OAuthRedirectURL == "" {
			return fmt.Errorf("when %sDATABASE_URL is set, GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET, and OAUTH_REDIRECT_URL are required", envPrefix)
		}
		if len(c.SessionSecret) < 16 {
			return fmt.Errorf("when %sDATABASE_URL is set, SESSION_SECRET must be at least 16 characters", envPrefix)
		}
	}
	return nil
}

// Default returns baseline defaults before flag parsing overrides them.
func Default() Config {
	return Config{
		TmpDir:               defaultTmpDir,
		SizeLimit:            defaultSizeLimit,
		Port:                 defaultPort,
		Release:              false,
		TokenPath:            defaultTokenPath,
		SessionTTL:           defaultSessionTTL,
		PostLoginRedirect:    "/",
	}
}

// DerivePaths sets ActiveRefs and LockFile from TmpDir. Call after TmpDir is final.
func (c *Config) DerivePaths() {
	c.ActiveRefs = filepath.Join(c.TmpDir, "active_refs.yaml")
	c.LockFile = filepath.Join(c.TmpDir, "active_refs.lock")
}
