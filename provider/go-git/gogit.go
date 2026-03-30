// Package gogit implements skills.Fetcher using go-git v6 (pure Go git implementation).
// This is an alternative to provider/git that does not require the git CLI.
package gogit

import (
	"context"
	"errors"
	"fmt"
	"os"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/transport"

	skills "github.com/ka2n/skills-go"
)

var _ skills.Fetcher = (*Fetcher)(nil)

// Fetcher implements skills.Fetcher using go-git.
type Fetcher struct {
	// Auth is the transport authentication method.
	// For GitHub/GitLab tokens, use &http.BasicAuth{Username: "x-access-token", Password: token}.
	// For SSH agent, use ssh.NewSSHAgentAuth("git").
	// nil for public repositories.
	Auth transport.AuthMethod
}

// Fetch clones a git repository to a temp directory.
// For local sources, it returns the path directly without cloning.
func (f *Fetcher) Fetch(ctx context.Context, source skills.ParsedSource) (string, func(), error) {
	if source.Type == skills.SourceLocal {
		return source.URL, func() {}, nil
	}

	tmpDir, err := os.MkdirTemp("", "skills-")
	if err != nil {
		return "", nil, fmt.Errorf("creating temp dir: %w", err)
	}
	cleanup := func() {
		os.RemoveAll(tmpDir)
	}

	opts := &gogit.CloneOptions{
		URL:          source.URL,
		Depth:        1,
		SingleBranch: true,
		Auth:         f.Auth,
	}

	if source.Ref != "" {
		opts.ReferenceName = plumbing.NewBranchReferenceName(source.Ref)
	}

	_, err = gogit.PlainCloneContext(ctx, tmpDir, opts)
	if err != nil {
		// Try as tag if branch clone failed and ref was specified
		if source.Ref != "" && opts.ReferenceName.IsBranch() {
			os.RemoveAll(tmpDir)
			os.MkdirAll(tmpDir, 0o755)
			opts.ReferenceName = plumbing.NewTagReferenceName(source.Ref)
			_, err = gogit.PlainCloneContext(ctx, tmpDir, opts)
		}
	}
	if err != nil {
		cleanup()
		if errors.Is(err, transport.ErrRepositoryNotFound) {
			return "", nil, fmt.Errorf("repository not found: %s", source.URL)
		}
		return "", nil, fmt.Errorf("clone failed for %s: %w", source.URL, err)
	}

	return tmpDir, cleanup, nil
}
