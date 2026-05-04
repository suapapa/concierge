package main

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/suapapa/concierge/internal/luggage"
)

// Handlers wires HTTP entrypoints to the luggage service.
type Handlers struct {
	Svc *luggage.Service
}

// PostLuggage handles file upload
// @Summary      Upload a file
// @Description  Upload a file and get a unique key. The file will be automatically deleted after TTL expires (default: 3 minutes) if there are no active references.
// @Tags         luggage
// @Accept       multipart/form-data
// @Produce      json
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

	resp, err := h.Svc.Upload(c.Request.Context(), luggage.UploadParams{
		Reader:    src,
		Filename:  file.Filename,
		Size:      file.Size,
		MIMEType:  mimeType,
		TTL:       ttl,
		CustomKey: c.PostForm("key"),
	})
	if err != nil {
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
// @Description  Check if the service is healthy by verifying temporary directory access and active refs file readability
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
	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"timestamp": time.Now().Format(time.RFC3339),
	})
}

// GetStat returns statistics about stored files
// @Summary      Get statistics
// @Description  Get statistics about all stored files including total keys, total size, active references, and details for each key
// @Tags         stat
// @Accept       json
// @Produce      json
// @Success      200     {object}  luggage.StatResponse
// @Failure      500     {object}  map[string]string  "Internal server error"
// @Router       /stat [get]
func (h *Handlers) GetStat(c *gin.Context) {
	resp, err := h.Svc.Stat(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}
