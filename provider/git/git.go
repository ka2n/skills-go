// Package git implements skills.Fetcher using the git command-line tool.
package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	skills "github.com/ka2n/skills-go"
)

var _ skills.Fetcher = (*Fetcher)(nil)

// Fetcher implements skills.Fetcher using git clone.
type Fetcher struct {
	// GitBin is the path to the git binary. Defaults to "git".
	GitBin string
	// Env is additional environment variables for git commands.
	Env []string
}

// Fetch clones a git repository to a temp directory.
// For local sources, it returns the path directly without cloning.
func (f *Fetcher) Fetch(ctx context.Context, source skills.ParsedSource) (string, func(), error) {
	if source.Type == skills.SourceLocal {
		return source.URL, func() {}, nil
	}

	gitBin := f.GitBin
	if gitBin == "" {
		gitBin = "git"
	}

	tmpDir, err := os.MkdirTemp("", "skills-")
	if err != nil {
		return "", nil, fmt.Errorf("creating temp dir: %w", err)
	}
	cleanup := func() {
		os.RemoveAll(tmpDir)
	}

	args := []string{"clone", "--depth", "1"}
	if source.Ref != "" {
		args = append(args, "--branch", source.Ref)
	}
	args = append(args, source.URL, tmpDir)

	cmd := exec.CommandContext(ctx, gitBin, args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	cmd.Env = append(cmd.Env, f.Env...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		cleanup()
		msg := strings.TrimSpace(string(output))
		if strings.Contains(msg, "Authentication failed") || strings.Contains(msg, "Repository not found") {
			return "", nil, fmt.Errorf("authentication failed for %s: %s", source.URL, msg)
		}
		return "", nil, fmt.Errorf("git clone failed for %s: %s", source.URL, msg)
	}

	return tmpDir, cleanup, nil
}
