// Package local implements skills.Fetcher for local filesystem paths
// and provides local disk hashing for skill folders.
package local

import (
	"context"
	"fmt"
	"path/filepath"

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

// ComputeFolderHash delegates to skills.ComputeFolderHash for backward compatibility.
func ComputeFolderHash(dir string) (string, error) {
	return skills.ComputeFolderHash(dir)
}

// ComputeContentHash delegates to skills.ComputeContentHash for backward compatibility.
func ComputeContentHash(content string) string {
	return skills.ComputeContentHash(content)
}
