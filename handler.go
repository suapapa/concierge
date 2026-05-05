package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/suapapa/concierge/internal/auth"
	"github.com/suapapa/concierge/internal/luggage"
	"github.com/suapapa/concierge/internal/store"
)

// Handlers wires HTTP entrypoints to the luggage service.
type Handlers struct {
	Svc            *luggage.Service
	Store          *store.Store // PostgreSQL (required at process startup)
	CookieSecure   bool         // forwarded from config for auth cookie flags
	MaxUploadBytes int          // global cap from -l / env; per-user cap is the lesser of this and DB quotas
}

// CreateAPIKeyResponse is returned from POST /api/v1/api-keys (plaintext secret only once in `key`).
type CreateAPIKeyResponse struct {
	ID        int64     `json:"id"`
	Key       string    `json:"key"`
	Prefix    string    `json:"prefix"`
	Label     string    `json:"label,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// PostLuggage handles file upload
// @Summary      Upload a file
// @Description  Upload a file and get a unique key. Requires session cookie (after Google login), `Authorization: Bearer concierge_…` user API key (database mode), or legacy Bearer token. Owner is the logged-in user; legacy uploads use owner 0.
// @Tags         luggage
// @Accept       multipart/form-data
// @Produce      json
// @Security     BearerAuth
// @Param        file    formData  file    true   "File to upload"
// @Param        key     formData  string  false  "Custom key (optional, auto-generated if not provided)"
// @Param        mime    formData  string  false  "MIME type (optional, auto-detected if not provided)"
// @Param        ttl     formData  int     false  "Time to live in minutes (default: 3)"
// @Success      200     {object}  luggage.SaveResponse
// @Failure      400     {object}  map[string]string  "Bad request (file required or size exceeds limit)"
// @Failure      401     {object}  map[string]string  "Unauthorized"
// @Failure      500     {object}  map[string]string  "Internal server error"
// @Router       /luggage [post]
func (h *Handlers) PostLuggage(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}

	mimeType := c.PostForm("mime")
	if mimeType == "" {
		mimeType = file.Header.Get("Content-Type")
	}

	ttl := 3 * time.Minute
	if ttlStr := c.PostForm("ttl"); ttlStr != "" {
		var minutes int
		if _, scanErr := fmt.Sscanf(ttlStr, "%d", &minutes); scanErr == nil && minutes > 0 {
			ttl = time.Duration(minutes) * time.Minute
		}
	}

	src, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to open uploaded file"})
		return
	}
	defer src.Close()

	ownerID := int64(0)
	reservedDaily := false
	globalLim := int64(h.MaxUploadBytes)
	if globalLim <= 0 {
		globalLim = h.Svc.MaxUploadBytes()
	}
	var maxPayload int64

	if !auth.IsLegacyBearer(c) {
		uid, ok := auth.UserID(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			return
		}
		ownerID = uid

		if ownerID > 0 {
			u, err := h.Store.UserByID(c.Request.Context(), ownerID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			if u == nil {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
				return
			}
			effectiveSingle := min(globalLim, u.MaxSingleFileBytes)
			maxPayload = effectiveSingle
			if file.Size > 0 && file.Size > effectiveSingle {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("file exceeds allowed size (%d bytes)", effectiveSingle)})
				return
			}
			addBytes := file.Size
			if addBytes <= 0 {
				addBytes = effectiveSingle
			}
			poolUsed, err := h.Store.SumLuggageBytesByOwner(c.Request.Context(), ownerID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			if poolUsed+addBytes > u.MaxPoolBytes {
				c.JSON(http.StatusForbidden, gin.H{"error": "storage pool quota exceeded for your account"})
				return
			}
			if err := h.Store.ReserveDailyUpload(c.Request.Context(), ownerID); err != nil {
				switch {
				case errors.Is(err, store.ErrDailyUploadQuotaExceeded):
					c.JSON(http.StatusForbidden, gin.H{"error": "daily upload limit reached for your account"})
				case errors.Is(err, pgx.ErrNoRows):
					c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
				default:
					c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				}
				return
			}
			reservedDaily = true
		}
	}

	resp, err := h.Svc.Upload(c.Request.Context(), luggage.UploadParams{
		Reader:            src,
		Filename:          file.Filename,
		Size:              file.Size,
		MIMEType:          mimeType,
		TTL:               ttl,
		CustomKey:         c.PostForm("key"),
		OwnerUserID:       ownerID,
		MaxPayloadBytes:   maxPayload,
	})
	if err != nil {
		if reservedDaily {
			_ = h.Store.ReleaseDailyUpload(context.Background(), ownerID)
		}
		switch {
		case errors.Is(err, luggage.ErrInvalidKey),
			errors.Is(err, luggage.ErrKeyExists),
			errors.Is(err, luggage.ErrMissingReader),
			errors.Is(err, luggage.ErrMissingFilename),
			errors.Is(err, luggage.ErrPayloadTooLarge):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}

	c.JSON(http.StatusOK, resp)
}

// DeleteLuggage removes a stored object by key.
// @Summary      Delete a file by key
// @Description  Admins may delete any object; guests may delete only objects they own (see ownerUserId in metadata). Legacy Bearer token behaves as admin.
// @Tags         luggage
// @Security     BearerAuth
// @Param        key   path      string  true  "File key"
// @Success      204   "No content"
// @Failure      400   {object}  map[string]string
// @Failure      401   {object}  map[string]string
// @Failure      403   {object}  map[string]string
// @Failure      404   {object}  map[string]string
// @Failure      500   {object}  map[string]string
// @Router       /luggage/{key} [delete]
func (h *Handlers) DeleteLuggage(c *gin.Context) {
	key := c.Param("key")
	info, err := h.Svc.ReadFileInfo(c.Request.Context(), key)
	if err != nil {
		switch {
		case errors.Is(err, luggage.ErrNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
			return
		case errors.Is(err, luggage.ErrInvalidKey):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid key"})
			return
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}
	role, _ := auth.Role(c)
	if !auth.IsLegacyBearer(c) && role == auth.RoleGuest {
		uid, ok := auth.UserID(c)
		if !ok || info.OwnerUserID != uid {
			c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden"})
			return
		}
	}
	if err := h.Svc.Delete(c.Request.Context(), key); err != nil {
		if errors.Is(err, luggage.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

// GetLuggage retrieves a file by key
// @Summary      Get a file by key
// @Description  Retrieve a file using the key returned from upload. The file can be downloaded with original filename or viewed inline.
// @Tags         luggage
// @Accept       json
// @Produce      application/octet-stream
// @Param        key       path      string  true   "File key"
// @Param        download  query     string  false  "Set to 'true' to download with original filename"
// @Success      200       {file}    file    "File content"
// @Failure      404       {object}  map[string]string  "File not found"
// @Failure      500       {object}  map[string]string  "Internal server error"
// @Router       /luggage/{key} [get]
func (h *Handlers) GetLuggage(c *gin.Context) {
	key := c.Param("key")
	lease, err := h.Svc.OpenGet(c.Request.Context(), key)
	if err != nil {
		if errors.Is(err, luggage.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
			return
		}
		if errors.Is(err, luggage.ErrInvalidKey) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid key"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer lease.Close()

	if lease.MimeType != "" {
		c.Header("Content-Type", lease.MimeType)
	}
	if c.Query("download") == "true" {
		c.FileAttachment(lease.FilePath, lease.Filename)
		return
	}
	c.File(lease.FilePath)
}

// GetHealth checks the health status of the service
// @Summary      Health check
// @Description  Check if the service is healthy by verifying temporary directory access, active download ref store (PostgreSQL), and database connectivity.
// @Tags         health
// @Accept       json
// @Produce      json
// @Success      200     {object}  map[string]string  "Service is healthy"
// @Failure      503     {object}  map[string]string  "Service is unhealthy"
// @Router       /health [get]
func (h *Handlers) GetHealth(c *gin.Context) {
	if err := h.Svc.Health(c.Request.Context()); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "unhealthy",
			"error":  err.Error(),
		})
		return
	}
	if err := h.Store.Ping(c.Request.Context()); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "unhealthy",
			"error":  "database: " + err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"timestamp": time.Now().Format(time.RFC3339),
	})
}

// GetStat returns statistics about stored files
// @Summary      Get statistics
// @Description  Admins and legacy Bearer see all keys. Guests see only their own uploads. Requires session, user API key, or legacy Bearer.
// @Tags         stat
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Success      200     {object}  luggage.StatResponse
// @Failure      401     {object}  map[string]string
// @Failure      500     {object}  map[string]string
// @Router       /stat [get]
func (h *Handlers) GetStat(c *gin.Context) {
	opts := luggage.StatOptions{}
	if !auth.IsLegacyBearer(c) {
		role, _ := auth.Role(c)
		if role == auth.RoleGuest {
			uid, ok := auth.UserID(c)
			if !ok {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
				return
			}
			opts.FilterUserID = &uid
		}
	}
	resp, err := h.Svc.Stat(c.Request.Context(), opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// PostAuthLogout ends the current DB session (clears cookie even if no session row exists).
// @Summary      Log out
// @Description  Deletes the server-side session for the concierge_session cookie.
// @Tags         auth
// @Security     BearerAuth
// @Success      204  "No content"
// @Router       /auth/logout [post]
func (h *Handlers) PostAuthLogout(c *gin.Context) {
	rawHex, err := c.Cookie(auth.SessionCookieName())
	if err != nil || rawHex == "" {
		c.Status(http.StatusNoContent)
		return
	}
	raw, err := hex.DecodeString(rawHex)
	if err == nil && len(raw) > 0 {
		_ = h.Store.DeleteSession(c.Request.Context(), raw)
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     auth.SessionCookieName(),
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	c.Status(http.StatusNoContent)
}

// AdminListUsers returns all users (admin only).
// @Summary      List users
// @Description  Returns all registered users and roles.
// @Tags         admin
// @Security     BearerAuth
// @Produce      json
// @Success      200  {array}   store.User
// @Failure      403  {object}  map[string]string
// @Failure      500  {object}  map[string]string
// @Router       /admin/users [get]
func (h *Handlers) AdminListUsers(c *gin.Context) {
	users, err := h.Store.ListUsers(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, users)
}

// AdminPatchUser updates a user's role and/or per-user quotas (max pool bytes, max single file bytes, daily upload cap).
// @Summary      Update user (role and quotas)
// @Description  Sets role to admin or guest and/or storage quotas. Omitted fields are left unchanged. Cannot demote the last remaining admin. Defaults for new users: 100 MiB pool, 10 MiB per file, 10 uploads per UTC day.
// @Tags         admin
// @Security     BearerAuth
// @Accept       json
// @Param        id    path      int64   true  "User id"
// @Param        body  body      object  true  "Payload"  example({"role":"guest","maxPoolBytes":104857600,"maxSingleFileBytes":10485760,"dailyMaxUploads":10})
// @Success      204   "No content"
// @Failure      400   {object}  map[string]string
// @Failure      403   {object}  map[string]string
// @Failure      404   {object}  map[string]string
// @Failure      500   {object}  map[string]string
// @Router       /admin/users/{id} [patch]
func (h *Handlers) AdminPatchUser(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}
	var body struct {
		Role               *string `json:"role"`
		MaxPoolBytes       *int64  `json:"maxPoolBytes"`
		MaxSingleFileBytes *int64  `json:"maxSingleFileBytes"`
		DailyMaxUploads    *int    `json:"dailyMaxUploads"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	if body.Role == nil && body.MaxPoolBytes == nil && body.MaxSingleFileBytes == nil && body.DailyMaxUploads == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no fields to update"})
		return
	}
	ctx := c.Request.Context()
	target, err := h.Store.UserByID(ctx, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if target == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	if body.Role != nil {
		role := strings.ToLower(strings.TrimSpace(*body.Role))
		if role != auth.RoleAdmin && role != auth.RoleGuest {
			c.JSON(http.StatusBadRequest, gin.H{"error": "role must be admin or guest"})
			return
		}
		if target.Role == auth.RoleAdmin && role == auth.RoleGuest {
			n, err := h.Store.CountAdmins(ctx)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			if n <= 1 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "cannot demote the last admin"})
				return
			}
		}
		if err := h.Store.SetUserRole(ctx, id, role); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	if body.MaxPoolBytes != nil || body.MaxSingleFileBytes != nil || body.DailyMaxUploads != nil {
		pool := target.MaxPoolBytes
		single := target.MaxSingleFileBytes
		daily := target.DailyMaxUploads
		if body.MaxPoolBytes != nil {
			pool = *body.MaxPoolBytes
		}
		if body.MaxSingleFileBytes != nil {
			single = *body.MaxSingleFileBytes
		}
		if body.DailyMaxUploads != nil {
			daily = *body.DailyMaxUploads
		}
		if err := h.Store.UpdateUserQuotas(ctx, id, pool, single, daily); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}
	c.Status(http.StatusNoContent)
}

// GetAPIKeys lists API keys for the signed-in user (not available for legacy bearer).
// @Summary      List API keys
// @Description  Returns metadata for keys owned by the current user. Secrets are never listed.
// @Tags         api-keys
// @Produce      json
// @Security     BearerAuth
// @Security     SessionCookie
// @Success      200  {array}   store.APIKeyMeta
// @Failure      401  {object}  map[string]string
// @Failure      503  {object}  map[string]string
// @Router       /api-keys [get]
func (h *Handlers) GetAPIKeys(c *gin.Context) {
	if auth.IsLegacyBearer(c) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	uid, ok := auth.UserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	keys, err := h.Store.ListAPIKeys(c.Request.Context(), uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if keys == nil {
		keys = []store.APIKeyMeta{}
	}
	c.JSON(http.StatusOK, keys)
}

// PostAPIKey creates a new API key; the full secret is returned once in `key`.
// @Summary      Create API key
// @Description  Generates a `concierge_…` secret. Use `Authorization: Bearer <key>` on protected routes. Same role as the user (admin keys may call admin APIs).
// @Tags         api-keys
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Security     SessionCookie
// @Param        body  body      object  false  "Optional label"  example({"label":"CI"})
// @Success      200   {object}  main.CreateAPIKeyResponse
// @Failure      400   {object}  map[string]string
// @Failure      401   {object}  map[string]string
// @Failure      503   {object}  map[string]string
// @Router       /api-keys [post]
func (h *Handlers) PostAPIKey(c *gin.Context) {
	if auth.IsLegacyBearer(c) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	uid, ok := auth.UserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	var body struct {
		Label string `json:"label"`
	}
	if err := c.ShouldBindJSON(&body); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	label := strings.TrimSpace(body.Label)
	if len(label) > 200 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "label too long"})
		return
	}
	meta, secret, err := h.Store.CreateAPIKey(c.Request.Context(), uid, label)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, CreateAPIKeyResponse{
		ID:        meta.ID,
		Key:       secret,
		Prefix:    meta.Prefix,
		Label:     meta.Label,
		CreatedAt: meta.CreatedAt,
	})
}

// DeleteAPIKey revokes an API key owned by the current user.
// @Summary      Delete API key
// @Description  Removes a key by id if it belongs to the signed-in user.
// @Tags         api-keys
// @Security     BearerAuth
// @Security     SessionCookie
// @Param        id    path      int64  true  "API key id"
// @Success      204   "No content"
// @Failure      400   {object}  map[string]string
// @Failure      401   {object}  map[string]string
// @Failure      404   {object}  map[string]string
// @Failure      503   {object}  map[string]string
// @Router       /api-keys/{id} [delete]
func (h *Handlers) DeleteAPIKey(c *gin.Context) {
	if auth.IsLegacyBearer(c) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	uid, ok := auth.UserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	idStr := c.Param("id")
	keyID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || keyID < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid key id"})
		return
	}
	if err := h.Store.DeleteAPIKey(c.Request.Context(), uid, keyID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "api key not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}
