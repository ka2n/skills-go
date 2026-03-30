// Package local implements skills.Fetcher for local filesystem paths
// and provides local disk hashing for skill folders.
package local

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	skills "github.com/ka2n/skills-go"
)

var _ skills.Fetcher = (*Fetcher)(nil)

// Fetcher implements skills.Fetcher for local filesystem paths.
type Fetcher struct{}

// Fetch returns the resolved local path directly.
func (f *Fetcher) Fetch(_ context.Context, source skills.ParsedSource) (string, func(), error) {
	if source.URL == "" {
		return "", nil, fmt.Errorf("local path is empty")
	}
	abs, err := filepath.Abs(source.URL)
	if err != nil {
		return "", nil, err
	}
	return abs, func() {}, nil
}

// ComputeFolderHash computes a SHA-256 hash from all files in a directory.
// Files are sorted by relative path for deterministic output.
func ComputeFolderHash(dir string) (string, error) {
	type fileEntry struct {
		relativePath string
		content      []byte
	}
	var files []fileEntry

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if name == ".git" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		rel = strings.ReplaceAll(rel, "\\", "/")
		files = append(files, fileEntry{relativePath: rel, content: content})
		return nil
	})
	if err != nil {
		return "", err
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].relativePath < files[j].relativePath
	})

	h := sha256.New()
	for _, f := range files {
		h.Write([]byte(f.relativePath))
		h.Write(f.content)
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// ComputeContentHash computes a SHA-256 hash of a string.
func ComputeContentHash(content string) string {
	h := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", h[:])
}
