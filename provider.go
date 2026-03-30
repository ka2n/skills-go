package skills

import (
	"context"
	"fmt"
)

// Fetcher is the core abstraction for fetching skills from a source.
// This replaces the direct git dependency, allowing any storage backend.
type Fetcher interface {
	// Fetch retrieves skills from the given parsed source into a local directory.
	// It returns the path to the local directory containing the fetched content.
	// The caller is responsible for calling Cleanup when done.
	Fetch(ctx context.Context, source ParsedSource) (localDir string, cleanup func(), err error)
}

// HostProvider is an interface for remote skill host providers (well-known endpoints, etc.).
type HostProvider interface {
	ID() string
	DisplayName() string
	Match(url string) (matches bool, sourceIdentifier string)
	FetchSkill(ctx context.Context, url string) (*RemoteSkill, error)
	FetchAllSkills(ctx context.Context, url string) ([]*RemoteSkill, error)
	ToRawURL(url string) string
	GetSourceIdentifier(url string) string
}

// ProviderRegistry manages host providers.
type ProviderRegistry struct {
	providers []HostProvider
}

// NewProviderRegistry creates a new provider registry.
func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{}
}

// Register adds a provider to the registry.
func (r *ProviderRegistry) Register(p HostProvider) error {
	for _, existing := range r.providers {
		if existing.ID() == p.ID() {
			return fmt.Errorf("provider with id %q already registered", p.ID())
		}
	}
	r.providers = append(r.providers, p)
	return nil
}

// FindProvider finds a provider that matches the given URL.
func (r *ProviderRegistry) FindProvider(url string) HostProvider {
	for _, p := range r.providers {
		if matches, _ := p.Match(url); matches {
			return p
		}
	}
	return nil
}

// Providers returns all registered providers.
func (r *ProviderRegistry) Providers() []HostProvider {
	return append([]HostProvider{}, r.providers...)
}

// HashProvider abstracts how skill folder hashes are obtained for update checking.
type HashProvider interface {
	// FetchFolderHash returns the hash for a skill folder in a remote source.
	// ownerRepo is "owner/repo", skillPath is the path within the repo.
	FetchFolderHash(ctx context.Context, ownerRepo string, skillPath string) (string, error)
}
