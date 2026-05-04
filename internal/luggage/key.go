package luggage

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

// ValidateKey rejects empty keys and path-traversal characters.
func ValidateKey(key string) error {
	if key == "" {
		return fmt.Errorf("key is required: %w", ErrInvalidKey)
	}
	if strings.Contains(key, "..") || strings.ContainsAny(key, `/\`) {
		return ErrInvalidKey
	}
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return ErrInvalidKey
		}
	}
	return nil
}

func generateKey() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("random key: %w", err)
	}
	return hex.EncodeToString(b), nil
}
