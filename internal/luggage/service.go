package luggage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/suapapa/concierge/internal/activerefs"
)

// ErrNotFound means no luggage exists for the given key.
var ErrNotFound = errors.New("luggage: not found")

const defaultTTL = 3 * time.Minute

// Service coordinates temporary file storage and active-reference tracking.
type Service struct {
	appCtx    context.Context
	tmpDir    string
	sizeLimit int64
	refs      *activerefs.Store
}

// NewService constructs a Service. appCtx should be cancelled on process shutdown
// so background expiry goroutines exit cleanly.
func NewService(appCtx context.Context, tmpDir string, sizeLimit int, refs *activerefs.Store) *Service {
	return &Service{
		appCtx:    appCtx,
		tmpDir:    tmpDir,
		sizeLimit: int64(sizeLimit),
		refs:      refs,
	}
}

// MaxUploadBytes returns the global upload size cap from service construction (-l / CONCIERGE_*).
func (s *Service) MaxUploadBytes() int64 {
	if s == nil {
		return 0
	}
	return s.sizeLimit
}

// UploadParams carries a single upload after the HTTP layer parsed the multipart form.
type UploadParams struct {
	Reader        io.Reader
	Filename      string
	Size          int64
	MIMEType      string
	TTL           time.Duration
	CustomKey     string
	OwnerUserID   int64
	// MaxPayloadBytes caps this upload; when > 0, the effective limit is min(service limit, MaxPayloadBytes).
	MaxPayloadBytes int64
}

// Upload stores payload and metadata under a new or caller-chosen key.
func (s *Service) Upload(ctx context.Context, p UploadParams) (*SaveResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.Reader == nil {
		return nil, ErrMissingReader
	}
	if p.Filename == "" {
		return nil, ErrMissingFilename
	}
	limit := s.sizeLimit
	if p.MaxPayloadBytes > 0 && p.MaxPayloadBytes < limit {
		limit = p.MaxPayloadBytes
	}
	if p.Size > 0 && p.Size > limit {
		return nil, fmt.Errorf("%d bytes: %w", p.Size, ErrPayloadTooLarge)
	}
	if p.TTL <= 0 {
		p.TTL = defaultTTL
	}

	key := p.CustomKey
	if key != "" {
		if err := ValidateKey(key); err != nil {
			return nil, err
		}
	} else {
		var err error
		key, err = generateKey()
		if err != nil {
			return nil, err
		}
	}

	keyDir := filepath.Join(s.tmpDir, key)
	if _, err := os.Stat(keyDir); err == nil {
		return nil, ErrKeyExists
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("stat key dir: %w", err)
	}
	if err := os.MkdirAll(keyDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}

	cleanup := func() { _ = os.RemoveAll(keyDir) }

	filePath := filepath.Join(keyDir, p.Filename)
	dst, err := os.Create(filePath)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("create payload: %w", err)
	}
	limited := io.LimitReader(p.Reader, limit+1)
	written, err := io.Copy(dst, limited)
	_ = dst.Close()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("save payload: %w", err)
	}
	if written > limit {
		cleanup()
		return nil, ErrPayloadTooLarge
	}

	info := FileInfo{MimeType: p.MIMEType, Filename: p.Filename, OwnerUserID: p.OwnerUserID}
	infoData, err := yaml.Marshal(info)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("marshal info: %w", err)
	}
	infoPath := filepath.Join(keyDir, "info.yaml")
	if err := os.WriteFile(infoPath, infoData, 0o644); err != nil {
		cleanup()
		return nil, fmt.Errorf("write info: %w", err)
	}

	s.scheduleRemoval(key, keyDir, p.TTL)

	return &SaveResponse{Key: key}, nil
}

func (s *Service) scheduleRemoval(key, keyDir string, ttl time.Duration) {
	ctx := s.appCtx
	go func() {
		t := time.NewTimer(ttl)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if s.tryExpire(ctx, key, keyDir) {
			return
		}
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if s.tryExpire(ctx, key, keyDir) {
					return
				}
			}
		}
	}()
}

func (s *Service) tryExpire(ctx context.Context, key, keyDir string) bool {
	n, err := s.refs.Count(ctx, key)
	if err == nil && n > 0 {
		return false
	}
	if err := os.RemoveAll(keyDir); err != nil && !os.IsNotExist(err) {
		log.Printf("luggage: remove key dir %s: %v", keyDir, err)
	}
	if err := s.refs.DeleteKey(context.Background(), key); err != nil {
		log.Printf("luggage: delete active ref %s: %v", key, err)
	}
	return true
}

// GetLease grants access to a stored file until Close decrements the active reference.
type GetLease struct {
	FilePath string
	MimeType string
	Filename string
	release  func()
	once     sync.Once
}

// Close releases the download lease; it is safe to call more than once.
func (l *GetLease) Close() {
	if l == nil || l.release == nil {
		return
	}
	l.once.Do(l.release)
}

// OpenGet increments the active reference count (best-effort) and returns file metadata.
func (s *Service) OpenGet(ctx context.Context, key string) (*GetLease, error) {
	if err := ValidateKey(key); err != nil {
		return nil, fmt.Errorf("validate key: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	keyDir := filepath.Join(s.tmpDir, key)
	infoPath := filepath.Join(keyDir, "info.yaml")
	infoData, err := os.ReadFile(infoPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("read info: %w", err)
	}

	var info FileInfo
	if err := yaml.Unmarshal(infoData, &info); err != nil {
		return nil, fmt.Errorf("parse info: %w", err)
	}

	filePath := filepath.Join(keyDir, info.Filename)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("stat payload: %w", err)
	}

	incFailed := false
	if err := s.refs.Increment(ctx, key); err != nil {
		log.Printf("luggage: increment active ref %s: %v", key, err)
		incFailed = true
	}

	lease := &GetLease{
		FilePath: filePath,
		MimeType: info.MimeType,
		Filename: info.Filename,
		release: func() {
			if incFailed {
				return
			}
			if err := s.refs.Decrement(context.Background(), key); err != nil {
				log.Printf("luggage: decrement active ref %s: %v", key, err)
			}
		},
	}
	return lease, nil
}

// ReadFileInfo loads persisted metadata for a key without incrementing active refs.
func (s *Service) ReadFileInfo(ctx context.Context, key string) (FileInfo, error) {
	var zero FileInfo
	if err := ValidateKey(key); err != nil {
		return zero, fmt.Errorf("validate key: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	keyDir := filepath.Join(s.tmpDir, key)
	infoPath := filepath.Join(keyDir, "info.yaml")
	infoData, err := os.ReadFile(infoPath)
	if err != nil {
		if os.IsNotExist(err) {
			return zero, ErrNotFound
		}
		return zero, fmt.Errorf("read info: %w", err)
	}
	var info FileInfo
	if err := yaml.Unmarshal(infoData, &info); err != nil {
		return zero, fmt.Errorf("parse info: %w", err)
	}
	return info, nil
}

// Delete removes a key directory and clears active-ref bookkeeping for that key.
func (s *Service) Delete(ctx context.Context, key string) error {
	if err := ValidateKey(key); err != nil {
		return fmt.Errorf("validate key: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	keyDir := filepath.Join(s.tmpDir, key)
	infoPath := filepath.Join(keyDir, "info.yaml")
	if _, err := os.Stat(infoPath); err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return fmt.Errorf("stat info: %w", err)
	}
	if err := os.RemoveAll(keyDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove key dir: %w", err)
	}
	if err := s.refs.DeleteKey(context.Background(), key); err != nil {
		log.Printf("luggage: delete active ref %s: %v", key, err)
	}
	return nil
}

// Health checks temp directory accessibility and active-ref store readability.
func (s *Service) Health(ctx context.Context) error {
	if _, err := os.Stat(s.tmpDir); err != nil {
		return fmt.Errorf("temporary directory: %w", err)
	}
	_, err := s.refs.Read(ctx)
	if err != nil {
		return fmt.Errorf("active refs: %w", err)
	}
	return nil
}

// Stat scans the temp directory and merges payload metadata with reference counts.
func (s *Service) Stat(ctx context.Context, opts StatOptions) (*StatResponse, error) {
	activeRefs, err := s.refs.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("read active refs: %w", err)
	}
	entries, err := os.ReadDir(s.tmpDir)
	if err != nil {
		return nil, fmt.Errorf("read tmp dir: %w", err)
	}

	var keys []KeyStat
	var totalSize int64
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		key := entry.Name()
		keyDir := filepath.Join(s.tmpDir, key)
		infoPath := filepath.Join(keyDir, "info.yaml")
		infoData, err := os.ReadFile(infoPath)
		if err != nil {
			continue
		}
		var info FileInfo
		if err := yaml.Unmarshal(infoData, &info); err != nil {
			continue
		}
		filePath := filepath.Join(keyDir, info.Filename)
		st, err := os.Stat(filePath)
		if err != nil {
			continue
		}
		if opts.FilterUserID != nil && info.OwnerUserID != *opts.FilterUserID {
			continue
		}
		keyStat := KeyStat{
			Key:         key,
			Filename:    info.Filename,
			MimeType:    info.MimeType,
			OwnerUserID: info.OwnerUserID,
			ActiveRefs:  activeRefs[key],
			FileSize:    st.Size(),
			Directory:   keyDir,
		}
		keys = append(keys, keyStat)
		totalSize += st.Size()
	}

	activeOut := activeRefs
	if opts.FilterUserID != nil {
		activeOut = make(map[string]int)
		for _, k := range keys {
			activeOut[k.Key] = activeRefs[k.Key]
		}
	}

	return &StatResponse{
		TotalKeys:  len(keys),
		TotalSize:  totalSize,
		ActiveRefs: activeOut,
		Keys:       keys,
	}, nil
}
