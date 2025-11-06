// @title           Concierge API
// @version         1.0
// @description     A temporary file storage service with TTL support

// @contact.name   Homin Lee
// @contact.url    https://github.com/suapapa
// @contact.email  ff4500@gmail.com

// @license.name  MIT
// @license.url   https://opensource.org/licenses/MIT

// @host      localhost:8080
// @BasePath  /

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Type "Bearer" followed by a space and API token.

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/suapapa/concierge/docs"
	swaggerfiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

const tokenPath = "/secret/token"

var (
	flagTmpDir    = "/tmp/concierge"
	flagSizeLimit = 1024 * 1024 * 5 // 5MB
	flagPort      = "8080"
	flagRelease   = false

	// 활성 파일 참조 추적 파일 경로
	activeRefsFile string
	// 락 파일 경로
	activeRefsLockFile string
)

func main() {
	flag.StringVar(&flagTmpDir, "t", flagTmpDir, "temporary directory")
	flag.IntVar(&flagSizeLimit, "l", flagSizeLimit, "size limit")
	flag.StringVar(&flagPort, "p", flagPort, "port")
	flag.BoolVar(&flagRelease, "r", flagRelease, "release mode")
	flag.Parse()

	if err := os.MkdirAll(flagTmpDir, 0755); err != nil {
		fmt.Printf("error: failed to create temporary directory: %v\n", err)
		os.Exit(1)
	}

	// activeRefs 파일 경로 설정
	activeRefsFile = filepath.Join(flagTmpDir, "active_refs.yaml")
	// 락 파일 경로 설정
	activeRefsLockFile = filepath.Join(flagTmpDir, "active_refs.lock")

	r := gin.New()
	if flagRelease {
		gin.SetMode(gin.ReleaseMode)
		r.Use(gin.Recovery())
	} else {
		r.Use(gin.Logger())
		r.Use(gin.Recovery())
	}

	var token string
	tokenBytes, err := os.ReadFile(tokenPath)
	if err != nil {
		log.Printf("Failed to read secret from %s: %v", tokenPath, err)
	} else {
		token = strings.TrimSpace(string(tokenBytes))
	}

	r.Use(func(c *gin.Context) {
		if c.Request.Method == "GET" || token == "" {
			c.Next()
			return
		}

		if c.GetHeader("Authorization") != "Bearer "+token {
			c.JSON(401, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}

		log.Println("authorized")
		c.Next()
	})

	r.POST("/luggage", PostLuggageHandler)
	r.GET("/luggage/:key", GetLuggageHandler)
	r.GET("/stat", GetStatHandler)
	r.GET("/health", GetHealthHandler)

	if !flagRelease {
		docs.SwaggerInfo.BasePath = "/"
		r.GET("/docs", func(c *gin.Context) {
			c.Redirect(302, "/swagger/index.html")
		})
		r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerfiles.Handler))
	}

	if err := r.Run(":" + flagPort); err != nil {
		fmt.Printf("error: failed to start server: %v\n", err)
		os.Exit(1)
	}
}
