// @title           Concierge API
// @version         1.0
// @description     A temporary file storage service with TTL support and optional Google OAuth.

// @contact.name   Homin Lee
// @contact.url    https://github.com/suapapa
// @contact.email  ff4500@gmail.com

// @license.name  MIT
// @license.url   https://opensource.org/licenses/MIT

// @host      localhost:8080
// @BasePath  /api/v1

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Type "Bearer" followed by a space: legacy admin token file contents, or a per-user key starting with `concierge_` from POST /api-keys (database mode).

// @securityDefinitions.apikey SessionCookie
// @in cookie
// @name concierge_session
// @description Opaque session cookie set after successful Google OAuth callback.

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/suapapa/concierge/docs"
	"github.com/suapapa/concierge/internal/activerefs"
	"github.com/suapapa/concierge/internal/auth"
	"github.com/suapapa/concierge/internal/config"
	"github.com/suapapa/concierge/internal/luggage"
	"github.com/suapapa/concierge/internal/staticui"
	"github.com/suapapa/concierge/internal/store"
	swaggerfiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

func main() {
	cfg := config.Default()
	flag.StringVar(&cfg.TmpDir, "t", cfg.TmpDir, "temporary directory")
	flag.IntVar(&cfg.SizeLimit, "l", cfg.SizeLimit, "size limit in bytes")
	flag.StringVar(&cfg.Port, "p", cfg.Port, "listen port")
	flag.BoolVar(&cfg.Release, "r", cfg.Release, "release mode")
	flag.StringVar(&cfg.TokenPath, "token", cfg.TokenPath, "path to legacy bearer token file")
	flag.Parse()
	cfg.ApplyEnv()

	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "invalid config: %v\n", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(cfg.TmpDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "create temporary directory: %v\n", err)
		os.Exit(1)
	}

	appCtx, stopApp := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopApp()

	refStore := activerefs.NewStore(cfg.ActiveRefs, cfg.LockFile)
	svc := luggage.NewService(appCtx, cfg.TmpDir, cfg.SizeLimit, refStore)

	legacyToken, err := readBearerToken(cfg.TokenPath)
	if err != nil {
		log.Printf("token: %v", err)
	}

	var st *store.Store
	if cfg.DatabaseURL != "" {
		st, err = store.Connect(appCtx, cfg.DatabaseURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "database: %v\n", err)
			os.Exit(1)
		}
		defer st.Close()
		if err := st.Migrate(appCtx); err != nil {
			fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
			os.Exit(1)
		}
	}

	h := &Handlers{
		Svc:            svc,
		Store:          st,
		CookieSecure:   cfg.CookieSecure,
		MaxUploadBytes: cfg.SizeLimit,
	}

	r := newEngine(cfg.Release)

	api := r.Group("/api/v1")
	api.GET("/health", h.GetHealth)
	api.GET("/luggage/:key", h.GetLuggage)

	if st != nil {
		oauthH, err := auth.NewOAuth(appCtx, &cfg, st)
		if err != nil {
			fmt.Fprintf(os.Stderr, "oauth: %v\n", err)
			os.Exit(1)
		}
		api.GET("/auth/google", oauthH.Start)
		api.GET("/auth/google/callback", oauthH.Callback)
	}

	protected := api.Group("")
	protected.Use(auth.RequireUserOrLegacy(legacyToken, st, st))
	protected.POST("/luggage", h.PostLuggage)
	protected.DELETE("/luggage/:key", h.DeleteLuggage)
	protected.GET("/stat", h.GetStat)
	protected.POST("/auth/logout", h.PostAuthLogout)
	protected.GET("/api-keys", h.GetAPIKeys)
	protected.POST("/api-keys", h.PostAPIKey)
	protected.DELETE("/api-keys/:id", h.DeleteAPIKey)

	admin := api.Group("/admin")
	admin.Use(auth.RequireUserOrLegacy(legacyToken, st, st))
	admin.Use(auth.RequireAdmin())
	admin.GET("/users", h.AdminListUsers)
	admin.PATCH("/users/:id", h.AdminPatchUser)

	if !cfg.Release {
		docs.SwaggerInfo.BasePath = "/api/v1"
		r.GET("/docs", func(c *gin.Context) {
			c.Redirect(http.StatusFound, "/swagger/index.html")
		})
		r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerfiles.Handler))
	}

	if staticui.Mount(r, cfg.StaticUIDir, cfg.Release) {
		log.Printf("serving dashboard static files from %s", cfg.StaticUIDir)
	} else {
		log.Printf("dashboard static UI not found at %s (build with: cd fe && npm run build)", cfg.StaticUIDir)
	}

	addr := ":" + cfg.Port
	srv := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("server: %v", err)
			stopApp()
		}
	}()

	<-appCtx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown: %v", err)
	}
}

func readBearerToken(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return strings.TrimSpace(string(b)), nil
}

func newEngine(release bool) *gin.Engine {
	r := gin.New()
	if release {
		gin.SetMode(gin.ReleaseMode)
		r.Use(gin.Recovery())
		return r
	}
	r.Use(gin.Logger(), gin.Recovery())
	return r
}
