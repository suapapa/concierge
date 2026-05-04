package config

import (
	"os"
	"strings"
	"time"
)

const envPrefix = "CONCIERGE_"

// ApplyEnv overlays environment variables onto cfg (call after flag.Parse).
func (c *Config) ApplyEnv() {
	if v := os.Getenv(envPrefix + "TMP_DIR"); v != "" {
		c.TmpDir = v
	}
	if v := os.Getenv(envPrefix + "TOKEN_PATH"); v != "" {
		c.TokenPath = v
	}
	if v := os.Getenv(envPrefix + "DATABASE_URL"); v != "" {
		c.DatabaseURL = v
	}
	if v := os.Getenv(envPrefix + "GOOGLE_CLIENT_ID"); v != "" {
		c.GoogleClientID = v
	}
	if v := os.Getenv(envPrefix + "GOOGLE_CLIENT_SECRET"); v != "" {
		c.GoogleClientSecret = v
	}
	if v := os.Getenv(envPrefix + "OAUTH_REDIRECT_URL"); v != "" {
		c.OAuthRedirectURL = v
	}
	if v := os.Getenv(envPrefix + "SESSION_SECRET"); v != "" {
		c.SessionSecret = v
	}
	if v := os.Getenv(envPrefix + "BOOTSTRAP_ADMIN_EMAILS"); v != "" {
		c.BootstrapAdminEmails = parseEmailList(v)
	}
	if v := os.Getenv(envPrefix + "SESSION_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			c.SessionTTL = d
		}
	}
	if v := os.Getenv(envPrefix + "POST_LOGIN_REDIRECT"); v != "" {
		c.PostLoginRedirect = v
	}
	if v := os.Getenv(envPrefix + "COOKIE_SECURE"); v != "" {
		c.CookieSecure = strings.EqualFold(v, "1") || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
	}
}

func parseEmailList(s string) []string {
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(strings.ToLower(p))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
