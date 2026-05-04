package luggage

// SaveResponse is returned after a successful upload.
type SaveResponse struct {
	Key string `json:"key"`
}

// FileInfo is persisted next to the uploaded payload as info.yaml.
type FileInfo struct {
	MimeType      string `yaml:"mimeType"`
	Filename      string `yaml:"filename"`
	OwnerUserID   int64  `yaml:"ownerUserId,omitempty"`
	// ExpiresAt is RFC3339Nano in UTC, set on upload for stat / UI (older objects may omit it).
	ExpiresAt string `yaml:"expiresAt,omitempty"`
}

// KeyStat describes one stored object for the statistics endpoint.
type KeyStat struct {
	Key           string `json:"key"`
	Filename      string `json:"filename"`
	MimeType      string `json:"mimeType"`
	OwnerUserID   int64  `json:"ownerUserId"`
	ActiveRefs    int    `json:"activeRefs"`
	FileSize      int64  `json:"fileSize"`
	Directory     string `json:"directory"`
	ExpiresAt     string `json:"expiresAt,omitempty"`
}

// StatOptions filters Stat results. When FilterUserID is non-nil, only objects owned by that user id are included.
type StatOptions struct {
	FilterUserID *int64
}

// StatResponse aggregates storage and reference metrics.
type StatResponse struct {
	TotalKeys  int            `json:"totalKeys"`
	TotalSize  int64          `json:"totalSize"`
	ActiveRefs map[string]int `json:"activeRefs"`
	Keys       []KeyStat      `json:"keys"`
}
