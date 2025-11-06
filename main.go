package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/goccy/go-yaml"
)

const tokenPath = "/secret/token"

var (
	flagTmpDir    = "/tmp/concierge"
	flagSizeLimit = 1024 * 1024 * 5 // 5MB
	flagPort      = "8080"
	// 활성 파일 참조 추적 파일 경로
	activeRefsFile string
	// 락 파일 경로
	activeRefsLockFile string
)

type SaveResponse struct {
	Key string `json:"key"`
}

type FileInfo struct {
	MimeType string `yaml:"mimeType"`
	Filename string `yaml:"filename"`
}

type KeyStat struct {
	Key        string `json:"key"`
	Filename   string `json:"filename"`
	MimeType   string `json:"mimeType"`
	ActiveRefs int    `json:"activeRefs"`
	FileSize   int64  `json:"fileSize"`
	Directory  string `json:"directory"`
}

type StatResponse struct {
	TotalKeys  int            `json:"totalKeys"`
	TotalSize  int64          `json:"totalSize"`
	ActiveRefs map[string]int `json:"activeRefs"`
	Keys       []KeyStat      `json:"keys"`
}

func generateKey() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func main() {
	flag.StringVar(&flagTmpDir, "t", flagTmpDir, "temporary directory")
	flag.IntVar(&flagSizeLimit, "l", flagSizeLimit, "size limit")
	flag.StringVar(&flagPort, "p", flagPort, "port")
	flag.Parse()

	if err := os.MkdirAll(flagTmpDir, 0755); err != nil {
		fmt.Printf("error: failed to create temporary directory: %v\n", err)
		os.Exit(1)
	}

	// activeRefs 파일 경로 설정
	activeRefsFile = filepath.Join(flagTmpDir, "active_refs.yaml")
	// 락 파일 경로 설정
	activeRefsLockFile = filepath.Join(flagTmpDir, "active_refs.lock")

	r := gin.Default()

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

	r.POST("/save", func(c *gin.Context) {
		// 파일 업로드 처리
		file, err := c.FormFile("file")
		if err != nil {
			c.JSON(400, gin.H{"error": "file is required"})
			return
		}

		// 파일 크기 제한 확인
		if file.Size > int64(flagSizeLimit) {
			c.JSON(400, gin.H{"error": fmt.Sprintf("file size exceeds limit: %d bytes (max: %d bytes)", file.Size, flagSizeLimit)})
			return
		}

		// MIME 타입 (optional)
		mimeType := c.PostForm("mime")
		if mimeType == "" {
			mimeType = file.Header.Get("Content-Type")
		}

		// TTL 처리 (기본 3분)
		ttlMinutes := 3
		if ttlStr := c.PostForm("ttl"); ttlStr != "" {
			if _, err := fmt.Sscanf(ttlStr, "%d", &ttlMinutes); err != nil {
				ttlMinutes = 3
			}
		}

		// 고유 키 생성
		key, err := generateKey()
		if err != nil {
			c.JSON(500, gin.H{"error": "failed to generate key"})
			return
		}

		// 키별 디렉터리 생성
		keyDir := filepath.Join(flagTmpDir, key)
		if err := os.MkdirAll(keyDir, 0755); err != nil {
			c.JSON(500, gin.H{"error": "failed to create directory"})
			return
		}

		// 파일 저장
		src, err := file.Open()
		if err != nil {
			c.JSON(500, gin.H{"error": "failed to open uploaded file"})
			return
		}
		defer src.Close()

		// 원본 파일명 사용
		filename := file.Filename
		filePath := filepath.Join(keyDir, filename)

		dst, err := os.Create(filePath)
		if err != nil {
			c.JSON(500, gin.H{"error": "failed to create file"})
			return
		}
		defer dst.Close()

		if _, err := io.Copy(dst, src); err != nil {
			c.JSON(500, gin.H{"error": "failed to save file"})
			return
		}

		// info.yaml에 메타데이터 저장
		info := FileInfo{
			MimeType: mimeType,
			Filename: filename,
		}
		infoData, err := yaml.Marshal(info)
		if err != nil {
			c.JSON(500, gin.H{"error": "failed to marshal info"})
			return
		}

		infoPath := filepath.Join(keyDir, "info.yaml")
		if err := os.WriteFile(infoPath, infoData, 0644); err != nil {
			c.JSON(500, gin.H{"error": "failed to save info.yaml"})
			return
		}

		// TTL이 지나면 디렉터리 전체 삭제 (goroutine으로 처리)
		// 활성 참조가 없을 때만 삭제
		go func() {
			time.Sleep(time.Duration(ttlMinutes) * time.Minute)
			// 활성 참조가 있는지 확인
			refCount, err := getActiveRefCount(key)
			if err == nil && refCount > 0 {
				// 활성 참조가 있으면 삭제를 지연 (1초마다 재확인)
				ticker := time.NewTicker(1 * time.Second)
				defer ticker.Stop()
				for range ticker.C {
					refCount, err := getActiveRefCount(key)
					if err != nil || refCount == 0 {
						os.RemoveAll(keyDir)
						if err := deleteActiveRef(key); err != nil {
							fmt.Printf("warning: failed to delete active ref for key %s: %v\n", key, err)
						}
						return
					}
				}
			} else {
				os.RemoveAll(keyDir)
				if err := deleteActiveRef(key); err != nil {
					fmt.Printf("warning: failed to delete active ref for key %s: %v\n", key, err)
				}
			}
		}()

		c.JSON(200, SaveResponse{Key: key})
	})

	r.GET("/fetch/:key", func(c *gin.Context) {
		key := c.Param("key")
		if key == "" {
			c.JSON(400, gin.H{"error": "key is required"})
			return
		}

		keyDir := filepath.Join(flagTmpDir, key)
		infoPath := filepath.Join(keyDir, "info.yaml")

		// info.yaml 읽기
		infoData, err := os.ReadFile(infoPath)
		if err != nil {
			c.JSON(404, gin.H{"error": "file not found"})
			return
		}

		var info FileInfo
		if err := yaml.Unmarshal(infoData, &info); err != nil {
			c.JSON(500, gin.H{"error": "failed to parse info.yaml"})
			return
		}

		// 파일 경로 확인
		filePath := filepath.Join(keyDir, info.Filename)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			c.JSON(404, gin.H{"error": "file not found"})
			return
		}

		// 활성 참조 증가 (파일 전송 중 삭제 방지)
		if err := incrementActiveRef(key); err != nil {
			// 참조 증가 실패는 로그만 남기고 계속 진행
			fmt.Printf("warning: failed to increment active ref for key %s: %v\n", key, err)
		}

		// 파일 전송 완료 후 참조 감소
		defer func() {
			if err := decrementActiveRef(key); err != nil {
				fmt.Printf("warning: failed to decrement active ref for key %s: %v\n", key, err)
			}
		}()

		// MIME 타입 설정
		if info.MimeType != "" {
			c.Header("Content-Type", info.MimeType)
		}

		// 파일 전송 (원본 파일명으로 다운로드)
		c.FileAttachment(filePath, info.Filename)
	})

	r.GET("/health", func(c *gin.Context) {
		// 임시 디렉터리 접근 가능 여부 확인
		if _, err := os.Stat(flagTmpDir); err != nil {
			c.JSON(503, gin.H{
				"status": "unhealthy",
				"error":  fmt.Sprintf("temporary directory not accessible: %v", err),
			})
			return
		}

		// 활성 참조 파일 읽기 가능 여부 확인
		_, err := readActiveRefs()
		if err != nil {
			// 파일이 없어도 정상 (아직 저장된 파일이 없을 수 있음)
			// 하지만 읽기 자체가 실패하면 문제
			if !os.IsNotExist(err) {
				c.JSON(503, gin.H{
					"status": "unhealthy",
					"error":  fmt.Sprintf("failed to read active refs: %v", err),
				})
				return
			}
		}

		c.JSON(200, gin.H{
			"status":    "healthy",
			"timestamp": time.Now().Format(time.RFC3339),
		})
	})

	r.GET("/stat", func(c *gin.Context) {
		// 활성 참조 정보 읽기
		activeRefs, err := readActiveRefs()
		if err != nil {
			c.JSON(500, gin.H{"error": fmt.Sprintf("failed to read active refs: %v", err)})
			return
		}

		// 임시 디렉터리 내의 모든 키 디렉터리 스캔
		entries, err := os.ReadDir(flagTmpDir)
		if err != nil {
			c.JSON(500, gin.H{"error": fmt.Sprintf("failed to read directory: %v", err)})
			return
		}

		var keys []KeyStat
		var totalSize int64

		for _, entry := range entries {
			if !entry.IsDir() {
				// 디렉터리가 아니면 스킵 (active_refs.yaml, active_refs.lock 등)
				continue
			}

			key := entry.Name()
			keyDir := filepath.Join(flagTmpDir, key)
			infoPath := filepath.Join(keyDir, "info.yaml")

			// info.yaml 읽기
			infoData, err := os.ReadFile(infoPath)
			if err != nil {
				// info.yaml이 없으면 스킵 (불완전한 디렉터리)
				continue
			}

			var info FileInfo
			if err := yaml.Unmarshal(infoData, &info); err != nil {
				// 파싱 실패 시 스킵
				continue
			}

			// 파일 경로 확인
			filePath := filepath.Join(keyDir, info.Filename)
			fileStat, err := os.Stat(filePath)
			if err != nil {
				// 파일이 없으면 스킵
				continue
			}

			// 활성 참조 수 가져오기
			refCount := activeRefs[key]

			keyStat := KeyStat{
				Key:        key,
				Filename:   info.Filename,
				MimeType:   info.MimeType,
				ActiveRefs: refCount,
				FileSize:   fileStat.Size(),
				Directory:  keyDir,
			}

			keys = append(keys, keyStat)
			totalSize += fileStat.Size()
		}

		response := StatResponse{
			TotalKeys:  len(keys),
			TotalSize:  totalSize,
			ActiveRefs: activeRefs,
			Keys:       keys,
		}

		c.JSON(200, response)
	})

	if err := r.Run(":" + flagPort); err != nil {
		fmt.Printf("error: failed to start server: %v\n", err)
		os.Exit(1)
	}
}
