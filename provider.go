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

// Providers bundles the provider interfaces needed by high-level APIs.
// All fields are optional; nil fields cause the corresponding functionality
// to be skipped or use defaults.
type Providers struct {
	// Fetcher retrieves skills from git-based sources.
	Fetcher Fetcher
	// HashProvider checks remote hashes for global skill update detection.
	HashProvider HashProvider
	// HostProviders is the registry for well-known endpoint providers.
	HostProviders *ProviderRegistry
	// SourceParsers are custom source parsers tried before built-in logic.
	// Use this to support source formats not handled by ParseSource
	// (e.g. Azure DevOps, Bitbucket shorthand, self-hosted GitLab).
	SourceParsers []SourceParser
}

// parseSource is a convenience that routes through custom parsers if configured.
func (p *Providers) parseSource(input string) (ParsedSource, error) {
	if p == nil {
		return ParseSource(input)
	}
	return ParseSourceWith(input, p.SourceParsers)
}

func (p *Providers) fetcher() Fetcher {
	if p == nil {
		return nil
	}
	return p.Fetcher
}

func (p *Providers) hashProvider() HashProvider {
	if p == nil {
		return nil
	}
	return p.HashProvider
}
