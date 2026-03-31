package wellknown

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	skills "github.com/ka2n/skills-go"
)

var _ skills.Fetcher = (*Fetcher)(nil)

// Fetcher implements [skills.Fetcher] for RFC 8615 well-known endpoints.
// It fetches skills via HTTP and writes them to a temporary directory
// that looks like a normal skill repository on disk.
type Fetcher struct {
	// Provider configures the HTTP client. If nil, defaults are used.
	Provider *Provider
}

func (f *Fetcher) provider() *Provider {
	if f.Provider != nil {
		return f.Provider
	}
	return &Provider{}
}

// Fetch fetches skills from a well-known endpoint and writes them to a temp directory.
// The returned localDir contains one subdirectory per skill, each with a SKILL.md.
func (f *Fetcher) Fetch(ctx context.Context, source skills.ParsedSource) (string, func(), error) {
	p := f.provider()

	if matches, _ := p.Match(source.URL); !matches {
		return "", nil, fmt.Errorf("wellknown: source %s is not a well-known endpoint", source.URL)
	}

	allSkills, err := p.FetchAllSkills(ctx, source.URL)
	if err != nil {
		return "", nil, fmt.Errorf("wellknown: fetching skills: %w", err)
	}

	if len(allSkills) == 0 {
		return "", nil, fmt.Errorf("wellknown: no skills found at %s", source.URL)
	}

	tmpDir, err := os.MkdirTemp("", "skills-wellknown-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { os.RemoveAll(tmpDir) }

	for _, skill := range allSkills {
		skillDir := filepath.Join(tmpDir, skills.SanitizeName(skill.Name))
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			cleanup()
			return "", nil, err
		}

		files := skill.Files
		if files == nil && skill.RawContent != "" {
			files = map[string]string{"SKILL.md": skill.RawContent}
		}

		for name, content := range files {
			filePath := filepath.Join(skillDir, name)
			if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
				cleanup()
				return "", nil, err
			}
			if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
				cleanup()
				return "", nil, err
			}
		}
	}

	return tmpDir, cleanup, nil
}
