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
)

// ErrNotFound means no luggage exists for the given key.
var ErrNotFound = errors.New("luggage: not found")

const defaultTTL = 3 * time.Minute

// Service coordinates temporary file storage and active-reference tracking.
type Service struct {
	appCtx    context.Context
	tmpDir    string
	sizeLimit int64
	refs      ActiveRefKeeper
	meta      MetaStore // optional: nil uses info.yaml (unit tests)
}

// ServiceOption configures NewService.
type ServiceOption func(*Service)

// WithMetaStore enables PostgreSQL-backed metadata instead of info.yaml.
func WithMetaStore(m MetaStore) ServiceOption {
	return func(s *Service) {
		s.meta = m
	}
}

// NewService constructs a Service. appCtx should be cancelled on process shutdown
// so background work (e.g. periodic expiry sweeps started from main) exits cleanly.
func NewService(appCtx context.Context, tmpDir string, sizeLimit int, refs ActiveRefKeeper, opts ...ServiceOption) *Service {
	s := &Service{
		appCtx:    appCtx,
		tmpDir:    tmpDir,
		sizeLimit: int64(sizeLimit),
		refs:      refs,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// TmpDir returns the configured temporary root directory.
func (s *Service) TmpDir() string {
	if s == nil {
		return ""
	}
	return s.tmpDir
}

// ReadActiveRefCounts returns the current per-key download reference counts.
func (s *Service) ReadActiveRefCounts(ctx context.Context) (map[string]int, error) {
	if s == nil || s.refs == nil {
		return nil, fmt.Errorf("luggage: nil service or refs")
	}
	return s.refs.Read(ctx)
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

	expiresAt := time.Now().UTC().Add(p.TTL).Format(time.RFC3339Nano)
	info := FileInfo{
		MimeType:    p.MIMEType,
		Filename:    p.Filename,
		OwnerUserID: p.OwnerUserID,
		ExpiresAt:   expiresAt,
	}

	if s.meta != nil {
		if err := s.meta.Put(ctx, key, info, written); err != nil {
			cleanup()
			if delErr := s.meta.Delete(context.Background(), key); delErr != nil {
				log.Printf("luggage: rollback meta delete %s: %v", key, delErr)
			}
			return nil, fmt.Errorf("meta put: %w", err)
		}
	} else {
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
	}

	return &SaveResponse{Key: key}, nil
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
	if s.meta != nil {
		if err := s.meta.Delete(context.Background(), key); err != nil {
			log.Printf("luggage: delete meta row %s: %v", key, err)
		}
	}
	return true
}

// SweepExpired loads up to batchLimit keys whose metadata expires_at is strictly before asOf,
// then for each key runs the same removal path as tryExpire (skip when active download refs > 0).
// When MetaStore is nil (info.yaml mode), it returns (0, 0, nil).
func (s *Service) SweepExpired(ctx context.Context, asOf time.Time, batchLimit int) (examined int, removed int, err error) {
	if s == nil {
		return 0, 0, fmt.Errorf("luggage: nil service")
	}
	if batchLimit <= 0 {
		return 0, 0, fmt.Errorf("luggage: sweep batch limit must be positive")
	}
	if s.meta == nil {
		return 0, 0, nil
	}
	keys, err := s.meta.ListExpiredKeys(ctx, asOf.UTC(), batchLimit)
	if err != nil {
		return 0, 0, err
	}
	examined = len(keys)
	for _, key := range keys {
		if err := ValidateKey(key); err != nil {
			log.Printf("luggage: sweep skip invalid key from meta %q: %v", key, err)
			continue
		}
		keyDir := filepath.Join(s.tmpDir, key)
		if s.tryExpire(ctx, key, keyDir) {
			removed++
		}
	}
	return examined, removed, nil
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
	var info FileInfo
	if s.meta != nil {
		var err error
		info, err = s.meta.Get(ctx, key)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil, ErrNotFound
			}
			return nil, err
		}
	} else {
		infoPath := filepath.Join(keyDir, "info.yaml")
		infoData, err := os.ReadFile(infoPath)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, ErrNotFound
			}
			return nil, fmt.Errorf("read info: %w", err)
		}
		if err := yaml.Unmarshal(infoData, &info); err != nil {
			return nil, fmt.Errorf("parse info: %w", err)
		}
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
	if s.meta != nil {
		info, err := s.meta.Get(ctx, key)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return zero, ErrNotFound
			}
			return zero, err
		}
		return info, nil
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
	if s.meta != nil {
		_, err := s.meta.Get(ctx, key)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return ErrNotFound
			}
			return err
		}
	} else {
		infoPath := filepath.Join(keyDir, "info.yaml")
		if _, err := os.Stat(infoPath); err != nil {
			if os.IsNotExist(err) {
				return ErrNotFound
			}
			return fmt.Errorf("stat info: %w", err)
		}
	}
	if err := os.RemoveAll(keyDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove key dir: %w", err)
	}
	if err := s.refs.DeleteKey(context.Background(), key); err != nil {
		log.Printf("luggage: delete active ref %s: %v", key, err)
	}
	if s.meta != nil {
		if err := s.meta.Delete(context.Background(), key); err != nil {
			log.Printf("luggage: delete meta row %s: %v", key, err)
		}
	}
	return nil
}

// Health checks temp directory accessibility and active-ref store readability.
func (s *Service) Health(ctx context.Context) error {
	if _, err := os.Stat(s.tmpDir); err != nil {
		return fmt.Errorf("temporary directory: %w", err)
	}
	if _, err := s.refs.Read(ctx); err != nil {
		return fmt.Errorf("active download refs: %w", err)
	}
	return nil
}

// Stat scans stored objects and merges metadata with reference counts.
func (s *Service) Stat(ctx context.Context, opts StatOptions) (*StatResponse, error) {
	activeRefs, err := s.refs.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("read active refs: %w", err)
	}

	if s.meta != nil {
		keys, totalSize, err := s.meta.List(ctx, opts.FilterUserID)
		if err != nil {
			return nil, err
		}
		for i := range keys {
			keys[i].ActiveRefs = activeRefs[keys[i].Key]
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

	entries, err := os.ReadDir(s.tmpDir)
	if err != nil {
		return nil, fmt.Errorf("read tmp dir: %w", err)
	}

	// Non-nil slice so JSON encodes as [] not null (React expects keys to be an array).
	keys := make([]KeyStat, 0)
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
			ExpiresAt:   info.ExpiresAt,
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
