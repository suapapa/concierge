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
)

var flagTmpDir = "/tmp/concierge"

type SaveResponse struct {
	Key string `json:"key"`
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
	flag.Parse()

	os.MkdirAll(flagTmpDir, 0755)

	r := gin.Default()

	r.POST("/save", func(c *gin.Context) {
		// 파일 업로드 처리
		file, err := c.FormFile("file")
		if err != nil {
			c.JSON(400, gin.H{"error": "file is required"})
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

		// 파일 저장
		src, err := file.Open()
		if err != nil {
			c.JSON(500, gin.H{"error": "failed to open uploaded file"})
			return
		}
		defer src.Close()

		// 파일 확장자 유지
		ext := filepath.Ext(file.Filename)
		filename := key + ext
		filePath := filepath.Join(flagTmpDir, filename)

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

		// MIME 타입을 파일명이나 메타데이터로 저장할 수도 있지만,
		// 지금은 파일만 저장하고 키만 반환
		// 필요시 나중에 메타데이터 파일을 별도로 저장할 수 있음

		// TTL이 지나면 파일 삭제 (goroutine으로 처리)
		go func() {
			time.Sleep(time.Duration(ttlMinutes) * time.Minute)
			os.Remove(filePath)
		}()

		c.JSON(200, SaveResponse{Key: key})
	})

	r.GET("/fetch/:key", func(c *gin.Context) {
		key := c.Param("key")
		if key == "" {
			c.JSON(400, gin.H{"error": "key is required"})
			return
		}

		// 키로 시작하는 파일 찾기 (확장자 포함)
		pattern := filepath.Join(flagTmpDir, key+"*")
		matches, err := filepath.Glob(pattern)
		if err != nil {
			c.JSON(500, gin.H{"error": "failed to search file"})
			return
		}

		if len(matches) == 0 {
			c.JSON(404, gin.H{"error": "file not found"})
			return
		}

		// 첫 번째 매칭 파일 사용
		filePath := matches[0]

		// 파일 존재 확인
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			c.JSON(404, gin.H{"error": "file not found"})
			return
		}

		// 파일 전송
		c.File(filePath)
	})

	r.Run(":8080")
}
