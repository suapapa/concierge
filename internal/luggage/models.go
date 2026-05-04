package luggage

// SaveResponse is returned after a successful upload.
type SaveResponse struct {
	Key string `json:"key"`
}

// FileInfo is persisted next to the uploaded payload as info.yaml.
type FileInfo struct {
	MimeType string `yaml:"mimeType"`
	Filename string `yaml:"filename"`
}

// KeyStat describes one stored object for the statistics endpoint.
type KeyStat struct {
	Key        string `json:"key"`
	Filename   string `json:"filename"`
	MimeType   string `json:"mimeType"`
	ActiveRefs int    `json:"activeRefs"`
	FileSize   int64  `json:"fileSize"`
	Directory  string `json:"directory"`
}

// StatResponse aggregates storage and reference metrics.
type StatResponse struct {
	TotalKeys  int            `json:"totalKeys"`
	TotalSize  int64          `json:"totalSize"`
	ActiveRefs map[string]int `json:"activeRefs"`
	Keys       []KeyStat      `json:"keys"`
}
