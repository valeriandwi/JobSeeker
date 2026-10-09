// Package storage: private on-disk storage for uploaded CV documents.
//
// Files are written under the configured DATA_DIR with opaque UUID names and
// mode 0600, and are never served by any static route. The database stores only
// the relative path, so a database dump does not reveal file contents.
package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// ErrInvalidDocumentPath reports a storage path that would escape DATA_DIR.
var ErrInvalidDocumentPath = errors.New("storage: invalid document path")

// SaveDocument writes an uploaded document and returns its path relative to
// dataDir. It creates the directory tree with restrictive permissions.
func SaveDocument(dataDir string, data []byte) (string, error) {
	if strings.TrimSpace(dataDir) == "" {
		return "", fmt.Errorf("data directory is not configured")
	}
	sub := filepath.Join(dataDir, "resumes")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		return "", fmt.Errorf("creating data directory: %w", err)
	}
	name := uuid.NewString() + ".bin"
	full := filepath.Join(sub, name)
	if err := os.WriteFile(full, data, 0o600); err != nil {
		return "", fmt.Errorf("writing document: %w", err)
	}
	return filepath.Join("resumes", name), nil
}

// ReadDocument reads a document previously written by SaveDocument. The path is
// validated so a tampered database value cannot escape the data directory.
func ReadDocument(dataDir, relPath string) ([]byte, error) {
	full, err := resolveDocumentPath(dataDir, relPath)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(full)
}

// RemoveDocument deletes a stored document, ignoring an already-missing file.
func RemoveDocument(dataDir, relPath string) error {
	full, err := resolveDocumentPath(dataDir, relPath)
	if err != nil {
		return err
	}
	if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing document: %w", err)
	}
	return nil
}

// resolveDocumentPath joins and validates a relative storage path.
func resolveDocumentPath(dataDir, relPath string) (string, error) {
	if strings.TrimSpace(dataDir) == "" {
		return "", fmt.Errorf("data directory is not configured")
	}
	if relPath == "" {
		return "", fmt.Errorf("document path is empty")
	}
	clean := filepath.Clean(relPath)
	if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return "", fmt.Errorf("%w: refusing to resolve %q", ErrInvalidDocumentPath, relPath)
	}
	base, err := filepath.Abs(dataDir)
	if err != nil {
		return "", fmt.Errorf("resolving data directory: %w", err)
	}
	full := filepath.Join(base, clean)
	rel, err := filepath.Rel(base, full)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("%w: refusing to access %q outside the data directory", ErrInvalidDocumentPath, relPath)
	}
	return full, nil
}
