package staticui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestMount_servesRootAndSPA(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<!doctype html><html>ui</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "x.js"), []byte("//x"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := gin.New()
	r.GET("/api/v1/health", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	if !Mount(r, root, true) {
		t.Fatal("expected Mount to return true")
	}

	t.Run("GET /", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("code %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "ui") {
			t.Fatalf("body %q", w.Body.String())
		}
	})

	t.Run("GET /assets/x.js", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/assets/x.js", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("code %d", w.Code)
		}
	})

	t.Run("GET /deep SPA", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/some/deep/path", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("code %d", w.Code)
		}
	})

	t.Run("GET /api/v1/health", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
		if w.Code != http.StatusNoContent {
			t.Fatalf("code %d", w.Code)
		}
	})

	t.Run("GET unknown under /api", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/nope", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("code %d", w.Code)
		}
	})
}

func TestMount_missingIndex(t *testing.T) {
	r := gin.New()
	if Mount(r, t.TempDir(), true) {
		t.Fatal("expected Mount to return false")
	}
}
