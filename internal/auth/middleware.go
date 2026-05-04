package auth

import (
	"context"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// SessionLookup resolves opaque session cookies to a user id and role.
type SessionLookup interface {
	LookupSession(ctx context.Context, rawToken []byte) (userID int64, role string, err error)
}

// APIKeyLookup resolves Bearer secrets (e.g. `concierge_…`) to a user id and role.
type APIKeyLookup interface {
	LookupAPIKey(ctx context.Context, rawSecret string) (userID int64, role string, err error)
}

// bearerToken returns the trimmed token after "Bearer ", or empty if missing.
func bearerToken(c *gin.Context) string {
	return strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
}

const sessionCookieName = "concierge_session"

// SessionCookieName is the HTTP cookie name for opaque DB-backed sessions.
func SessionCookieName() string { return sessionCookieName }

// RequireUserOrLegacy requires a valid session cookie, user API key (`Authorization: Bearer concierge_…`), or matching legacy Bearer token.
func RequireUserOrLegacy(legacyToken string, sessions SessionLookup, apiKeys APIKeyLookup) gin.HandlerFunc {
	return func(c *gin.Context) {
		if t := strings.TrimSpace(legacyToken); t != "" {
			authz := bearerToken(c)
			if authz == t {
				c.Set(CtxLegacyKey, true)
				c.Set(CtxUserIDKey, int64(0))
				c.Set(CtxRoleKey, RoleAdmin)
				c.Next()
				return
			}
		}
		if apiKeys != nil {
			if authz := bearerToken(c); authz != "" && strings.HasPrefix(authz, "concierge_") {
				uid, role, err := apiKeys.LookupAPIKey(c.Request.Context(), authz)
				if err == nil {
					c.Set(CtxUserIDKey, uid)
					c.Set(CtxRoleKey, role)
					c.Next()
					return
				}
			}
		}
		if sessions == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}
		rawHex, err := c.Cookie(sessionCookieName)
		if err != nil || rawHex == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}
		raw, err := hex.DecodeString(rawHex)
		if err != nil || len(raw) == 0 {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}
		uid, role, err := sessions.LookupSession(c.Request.Context(), raw)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}
		c.Set(CtxUserIDKey, uid)
		c.Set(CtxRoleKey, role)
		c.Next()
	}
}

// RequireAdmin must run after RequireUserOrLegacy. Legacy bearer counts as admin.
func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, _ := c.Get(CtxRoleKey)
		rs, _ := role.(string)
		if rs != RoleAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// IsLegacyBearer reports whether the request was authorized via the static token file.
func IsLegacyBearer(c *gin.Context) bool {
	v, ok := c.Get(CtxLegacyKey)
	return ok && v.(bool)
}

// UserID returns the authenticated user id (0 for legacy bearer uploads).
func UserID(c *gin.Context) (int64, bool) {
	v, ok := c.Get(CtxUserIDKey)
	if !ok {
		return 0, false
	}
	id, ok := v.(int64)
	return id, ok
}

// Role returns the authenticated role.
func Role(c *gin.Context) (string, bool) {
	v, ok := c.Get(CtxRoleKey)
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}
