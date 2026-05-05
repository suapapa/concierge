package luggagestore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/suapapa/concierge/internal/luggage"
	"github.com/suapapa/concierge/internal/store"
)

// yamlSidecar matches legacy info.yaml fields.
type yamlSidecar struct {
	MimeType    string `yaml:"mimeType"`
	Filename    string `yaml:"filename"`
	OwnerUserID int64  `yaml:"ownerUserId,omitempty"`
	ExpiresAt   string `yaml:"expiresAt,omitempty"`
}

// BackfillYAMLToDB scans tmpDir for per-key directories containing info.yaml,
// upserts rows into luggage_objects, then removes each info.yaml file.
func BackfillYAMLToDB(ctx context.Context, st *store.Store, tmpDir string) (processed int, err error) {
	if st == nil {
		return 0, fmt.Errorf("luggagestore: nil store")
	}
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return 0, fmt.Errorf("read tmp dir: %w", err)
	}
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		key := ent.Name()
		if err := luggage.ValidateKey(key); err != nil {
			continue
		}
		keyDir := filepath.Join(tmpDir, key)
		infoPath := filepath.Join(keyDir, "info.yaml")
		data, err := os.ReadFile(infoPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return processed, fmt.Errorf("read %s: %w", infoPath, err)
		}
		var side yamlSidecar
		if err := yaml.Unmarshal(data, &side); err != nil {
			return processed, fmt.Errorf("parse %s: %w", infoPath, err)
		}
		if side.Filename == "" {
			continue
		}
		payloadPath := filepath.Join(keyDir, side.Filename)
		stPayload, err := os.Stat(payloadPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return processed, fmt.Errorf("stat payload %s: %w", payloadPath, err)
		}
		var exp time.Time
		if side.ExpiresAt != "" {
			exp, err = time.Parse(time.RFC3339Nano, side.ExpiresAt)
			if err != nil {
				return processed, fmt.Errorf("expiresAt in %s: %w", infoPath, err)
			}
		} else {
			exp = time.Now().UTC().Add(24 * time.Hour)
		}
		if err := st.UpsertLuggage(ctx, key, side.OwnerUserID, side.Filename, side.MimeType, stPayload.Size(), exp.UTC()); err != nil {
			return processed, fmt.Errorf("upsert key %s: %w", key, err)
		}
		if err := os.Remove(infoPath); err != nil && !os.IsNotExist(err) {
			return processed, fmt.Errorf("remove %s: %w", infoPath, err)
		}
		processed++
	}
	return processed, nil
}
