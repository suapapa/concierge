package luggage

import "errors"

// ErrInvalidKey means the key format is not allowed.
var ErrInvalidKey = errors.New("luggage: invalid key")

// ErrKeyExists means the storage directory for that key already exists.
var ErrKeyExists = errors.New("luggage: key already exists")

// ErrMissingReader is returned when UploadParams.Reader is nil.
var ErrMissingReader = errors.New("luggage: reader is required")

// ErrMissingFilename is returned when UploadParams.Filename is empty.
var ErrMissingFilename = errors.New("luggage: filename is required")

// ErrPayloadTooLarge is returned when the payload exceeds the configured limit.
var ErrPayloadTooLarge = errors.New("luggage: payload too large")
