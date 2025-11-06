package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/goccy/go-yaml"
)

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

		// 파일 전송
		c.File(filePath)
	})

	if err := r.Run(":" + flagPort); err != nil {
		fmt.Printf("error: failed to start server: %v\n", err)
		os.Exit(1)
	}
}
