// Package config holds application configuration loaded from flags or environment.
package config

import (
	"fmt"
	"time"

	"github.com/suapapa/concierge/internal/store"
)

const (
	defaultTmpDir = "./concierge_archive/"
	defaultPort   = "8080"
	defaultTokenPath  = "/secret/token"
	defaultSessionTTL = 7 * 24 * time.Hour
)

// Config is runtime configuration for the Concierge server.
type Config struct {
	TmpDir     string
	SizeLimit  int
	Port       string
	Release    bool
	TokenPath string

	// DatabaseURL is required: PostgreSQL DSN for sessions, API keys, luggage metadata, and quotas.
	DatabaseURL string
	// LuggageBackfill, when true (e.g. CONCIERGE_LUGGAGE_BACKFILL=1), runs a one-time yaml→DB scan at startup then clears each info.yaml.
	LuggageBackfill bool
	// Google OAuth (required).
	GoogleClientID     string
	GoogleClientSecret string
	OAuthRedirectURL   string
	// SessionSecret is used for OAuth state signing and cookies; must be at least 16 bytes.
	SessionSecret string
	// BootstrapAdminEmails: first-time users with these emails (lowercased) get role admin.
	BootstrapAdminEmails []string
	SessionTTL           time.Duration
	// PostLoginRedirect is the path or URL users hit after successful OAuth (default "/").
	PostLoginRedirect string
	// CookieSecure sets the Secure flag on auth cookies (use true behind HTTPS).
	CookieSecure bool
	// StaticUIDir is the directory with the Vite production bundle (index.html, assets/).
	// When that index.html exists at startup, GET / and client routes serve the React app.
	StaticUIDir string

	// LuggageExpirySweepInterval is how often the server runs a DB-driven expiry sweep (0 disables).
	LuggageExpirySweepInterval time.Duration
	// LuggageExpirySweepOnce, when true, runs sweep rounds until no expired keys remain then exits (no HTTP; for CronJob).
	LuggageExpirySweepOnce bool
	// LuggageExpirySweepBatch caps keys loaded from the database per sweep query / round.
	LuggageExpirySweepBatch int
}

// Validate checks required fields and returns an error if the config is unusable.
func (c *Config) Validate() error {
	if c.TmpDir == "" {
		return fmt.Errorf("tmpDir is required")
	}
	if c.LuggageExpirySweepOnce {
		if c.DatabaseURL == "" {
			return fmt.Errorf("%sDATABASE_URL is required", envPrefix)
		}
		if c.LuggageExpirySweepBatch <= 0 {
			return fmt.Errorf("luggage expiry sweep batch must be positive")
		}
		if c.SizeLimit <= 0 {
			return fmt.Errorf("sizeLimit must be positive")
		}
		if c.LuggageExpirySweepInterval < 0 {
			return fmt.Errorf("luggage expiry sweep interval must not be negative")
		}
		return nil
	}
	if c.SizeLimit <= 0 {
		return fmt.Errorf("sizeLimit must be positive")
	}
	if c.Port == "" {
		return fmt.Errorf("port is required")
	}
	if c.DatabaseURL == "" {
		return fmt.Errorf("%sDATABASE_URL is required", envPrefix)
	}
	if c.LuggageExpirySweepBatch <= 0 {
		return fmt.Errorf("luggage expiry sweep batch must be positive")
	}
	if c.LuggageExpirySweepInterval < 0 {
		return fmt.Errorf("luggage expiry sweep interval must not be negative")
	}
	if c.GoogleClientID == "" || c.GoogleClientSecret == "" || c.OAuthRedirectURL == "" {
		return fmt.Errorf("%sGOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET, and OAUTH_REDIRECT_URL are required", envPrefix)
	}
	if len(c.SessionSecret) < 16 {
		return fmt.Errorf("%sSESSION_SECRET must be at least 16 characters", envPrefix)
	}
	return nil
}

// Default returns baseline defaults before flag parsing overrides them.
func Default() Config {
	return Config{
		TmpDir:            defaultTmpDir,
		// Align with store.DefaultMaxSingleFileBytes so min(-l, per-user) does not
		// undercut new-user DB quotas; operators still tighten with -l.
		SizeLimit:         int(store.DefaultMaxSingleFileBytes),
		Port:              defaultPort,
		Release:           false,
		TokenPath:         defaultTokenPath,
		SessionTTL:        defaultSessionTTL,
		PostLoginRedirect: "/",
		OAuthRedirectURL:  "http://localhost:8080/api/v1/auth/google/callback",
		StaticUIDir:       "fe/dist",

		LuggageExpirySweepInterval: time.Minute,
		LuggageExpirySweepBatch:    500,
	}
}
