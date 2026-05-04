// Package staticui mounts a Vite production bundle (index.html + /assets) on a Gin engine.
package staticui

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

// Mount serves the UI from root (a directory containing index.html).
// It registers GET /, static files under /assets when root/assets exists, and NoRoute SPA fallback.
// Returns false if root/index.html is missing (caller may log; GET / stays unregistered).
func Mount(r *gin.Engine, root string, release bool) bool {
	index := filepath.Join(root, "index.html")
	st, err := os.Stat(index)
	if err != nil || st.IsDir() {
		return false
	}
	assetsDir := filepath.Join(root, "assets")
	if fi, err := os.Stat(assetsDir); err == nil && fi.IsDir() {
		r.Static("/assets", assetsDir)
	}
	absIndex, err := filepath.Abs(index)
	if err != nil {
		absIndex = index
	}
	r.GET("/", func(c *gin.Context) {
		c.File(absIndex)
	})
	r.NoRoute(spaFallback(absIndex, release))
	return true
}

func spaFallback(index string, release bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		p := c.Request.URL.Path
		if strings.HasPrefix(p, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		if !release {
			if p == "/docs" || p == "/swagger" || strings.HasPrefix(p, "/swagger/") {
				c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
				return
			}
		}
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.Status(http.StatusNotFound)
			return
		}
		c.File(index)
	}
}
