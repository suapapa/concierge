// @title           Concierge API
// @version         1.0
// @description     A temporary file storage service with TTL support

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
// @description Type "Bearer" followed by a space and API token.

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
	"github.com/suapapa/concierge/internal/config"
	"github.com/suapapa/concierge/internal/luggage"
	swaggerfiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

func main() {
	cfg := config.Default()
	flag.StringVar(&cfg.TmpDir, "t", cfg.TmpDir, "temporary directory")
	flag.IntVar(&cfg.SizeLimit, "l", cfg.SizeLimit, "size limit in bytes")
	flag.StringVar(&cfg.Port, "p", cfg.Port, "listen port")
	flag.BoolVar(&cfg.Release, "r", cfg.Release, "release mode")
	flag.Parse()

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

	token, err := readBearerToken(cfg.TokenPath)
	if err != nil {
		log.Printf("token: %v", err)
	}

	r := newEngine(cfg.Release)
	r.Use(authMiddleware(token))

	h := &Handlers{Svc: svc}
	api := r.Group("/api/v1")
	{
		api.POST("/luggage", h.PostLuggage)
		api.GET("/luggage/:key", h.GetLuggage)
		api.GET("/stat", h.GetStat)
		api.GET("/health", h.GetHealth)
	}

	r.StaticFile("/", "web/index.html")

	if !cfg.Release {
		docs.SwaggerInfo.BasePath = "/api/v1"
		r.GET("/docs", func(c *gin.Context) {
			c.Redirect(http.StatusFound, "/swagger/index.html")
		})
		r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerfiles.Handler))
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

func authMiddleware(token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodGet || token == "" {
			c.Next()
			return
		}
		if c.GetHeader("Authorization") != "Bearer "+token {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}
		log.Println("authorized")
		c.Next()
	}
}
