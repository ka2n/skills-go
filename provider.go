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

// HashProvider abstracts how skill folder hashes are obtained for update checking.
type HashProvider interface {
	// FetchFolderHash returns the hash for a skill folder in a remote source.
	// ownerRepo is "owner/repo", skillPath is the path within the repo.
	FetchFolderHash(ctx context.Context, ownerRepo string, skillPath string) (string, error)
}

// Providers bundles the provider interfaces needed by high-level APIs.
// All fields are optional; nil fields cause the corresponding functionality
// to be skipped or use defaults.
//
// To combine multiple implementations, use [MultiFetcher], [MultiHashProvider],
// and [MultiSourceParser].
type Providers struct {
	// Fetcher retrieves skills from remote sources (git, well-known endpoints, etc.).
	// Use [MultiFetcher] to combine multiple fetchers.
	Fetcher Fetcher
	// HashProvider checks remote hashes for update detection.
	HashProvider HashProvider
	// SourceParser parses source strings into ParsedSource.
	// Tried before the built-in logic.
	SourceParser SourceParser
}

// MultiFetcher combines multiple Fetchers. Each is tried in order;
// the first that succeeds is used.
func MultiFetcher(fetchers ...Fetcher) Fetcher {
	return multiFetcher(fetchers)
}

type multiFetcher []Fetcher

func (mf multiFetcher) Fetch(ctx context.Context, source ParsedSource) (string, func(), error) {
	var lastErr error
	for _, f := range mf {
		dir, cleanup, err := f.Fetch(ctx, source)
		if err == nil {
			return dir, cleanup, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return "", nil, lastErr
	}
	return "", nil, fmt.Errorf("no fetcher available")
}

// MultiHashProvider combines multiple HashProviders. Each is tried in order;
// the first that succeeds is used.
func MultiHashProvider(providers ...HashProvider) HashProvider {
	return multiHashProvider(providers)
}

type multiHashProvider []HashProvider

func (mh multiHashProvider) FetchFolderHash(ctx context.Context, ownerRepo string, skillPath string) (string, error) {
	var lastErr error
	for _, hp := range mh {
		hash, err := hp.FetchFolderHash(ctx, ownerRepo, skillPath)
		if err == nil {
			return hash, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("no hash provider available")
}

// MultiSourceParser combines multiple SourceParsers. Each is tried in order;
// the first that recognizes the input wins.
func MultiSourceParser(parsers ...SourceParser) SourceParser {
	return func(input string) (ParsedSource, bool, error) {
		for _, p := range parsers {
			ps, ok, err := p(input)
			if err != nil {
				return ps, false, err
			}
			if ok {
				return ps, true, nil
			}
		}
		return ParsedSource{}, false, nil
	}
}
