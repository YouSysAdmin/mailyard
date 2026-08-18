// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package blob abstracts object storage for email attachments. A nil
// Store means inline storage (base64 in the database), which stays
// the default - the fs and s3 backends move attachment bytes out of
// the database and leave only metadata plus a storage key behind.
package blob

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// Store is the minimal object interface both integrations need.
type Store interface {
	Put(ctx context.Context, key string, r io.Reader, contentType string) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}

// Config selects and parameterizes a backend.
type Config struct {
	// Backend is "" (inline, no store), "fs" or "s3".
	Backend string

	// FSPath is the filesystem backend's base directory.
	FSPath string

	// S3 settings. Endpoint is optional (AWS default), set it for
	// MinIO or other S3-compatible services together with path style.
	S3Endpoint     string
	S3Region       string
	S3Bucket       string
	S3AccessKey    string
	S3SecretKey    string
	S3UsePathStyle bool
}

// New returns the configured store, or nil when Backend is empty
// (inline mode).
func New(cfg Config) (Store, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Backend)) {
	case "":
		return nil, nil
	case "fs", "filesystem":
		return newFSStore(cfg.FSPath)
	case "s3":
		return newS3Store(cfg)
	default:
		return nil, fmt.Errorf("storage.backend %q invalid: want %q, %q or empty for inline", cfg.Backend, "fs", "s3")
	}
}

// SanitizeFilename reduces a client-supplied filename to a safe key
// segment.
func SanitizeFilename(name string) string {
	if name == "" {
		return "attachment"
	}

	out := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		}

		return '_'
	}, name)
	out = strings.Trim(out, "._")
	if out == "" {
		return "attachment"
	}

	if len(out) > 120 {
		out = out[:120]
	}

	return out
}
