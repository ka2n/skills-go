// Package wellknown implements skills.HostProvider for RFC 8615 well-known endpoints.
//
// Organizations can publish skills at:
//
//	https://example.com/.well-known/agent-skills/  (preferred)
//	https://example.com/.well-known/skills/         (legacy fallback)
package wellknown

import (
	"context"
	"github.com/goccy/go-json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"

	skills "github.com/ka2n/skills-go"
)

// Index represents the index.json structure for well-known skills.
type Index struct {
	Skills []SkillEntry `json:"skills"`
}

// SkillEntry represents a skill entry in the well-known index.
type SkillEntry struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Files       []string `json:"files"`
}

var _ skills.HostProvider = (*Provider)(nil)

// Provider implements skills.HostProvider for RFC 8615 well-known endpoints.
type Provider struct {
	Client *http.Client
}

var wellKnownDirs = []string{".well-known/agent-skills", ".well-known/skills"}

func (p *Provider) ID() string         { return "well-known" }
func (p *Provider) DisplayName() string { return "Well-Known Skills" }

func (p *Provider) Match(rawURL string) (bool, string) {
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return false, ""
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false, ""
	}
	switch u.Hostname() {
	case "github.com", "gitlab.com", "huggingface.co":
		return false, ""
	}
	return true, "wellknown/" + u.Hostname()
}

func (p *Provider) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return http.DefaultClient
}

type indexResult struct {
	Index           Index
	ResolvedBaseURL string
	WellKnownPath   string
}

func (p *Provider) fetchIndex(ctx context.Context, baseURL string) (*indexResult, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}
	basePath := strings.TrimSuffix(u.Path, "/")

	type candidate struct {
		indexURL      string
		resolvedBase  string
		wellKnownPath string
	}
	var candidates []candidate

	for _, wkPath := range wellKnownDirs {
		// Path-relative
		candidates = append(candidates, candidate{
			indexURL:      fmt.Sprintf("%s://%s%s", u.Scheme, u.Host, path.Join(basePath, wkPath, "index.json")),
			resolvedBase:  fmt.Sprintf("%s://%s%s", u.Scheme, u.Host, basePath),
			wellKnownPath: wkPath,
		})
		// Root fallback
		if basePath != "" {
			candidates = append(candidates, candidate{
				indexURL:      fmt.Sprintf("%s://%s/%s", u.Scheme, u.Host, path.Join(wkPath, "index.json")),
				resolvedBase:  fmt.Sprintf("%s://%s", u.Scheme, u.Host),
				wellKnownPath: wkPath,
			})
		}
	}

	for _, c := range candidates {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.indexURL, nil)
		if err != nil {
			continue
		}
		resp, err := p.client().Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}
		var index Index
		if err := json.NewDecoder(resp.Body).Decode(&index); err != nil {
			resp.Body.Close()
			continue
		}
		resp.Body.Close()

		if len(index.Skills) == 0 {
			continue
		}
		valid := true
		for _, entry := range index.Skills {
			if !isValidEntry(entry) {
				valid = false
				break
			}
		}
		if valid {
			return &indexResult{Index: index, ResolvedBaseURL: c.resolvedBase, WellKnownPath: c.wellKnownPath}, nil
		}
	}
	return nil, fmt.Errorf("no well-known skills index found at %s", baseURL)
}

// nameRe validates skill names: 1-64 chars, lowercase alphanumeric and hyphens.
var nameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)

func isValidEntry(e SkillEntry) bool {
	if e.Name == "" || e.Description == "" || len(e.Files) == 0 {
		return false
	}
	if len(e.Name) > 1 && !nameRe.MatchString(e.Name) {
		return false
	}
	hasSkillMd := false
	for _, f := range e.Files {
		if strings.HasPrefix(f, "/") || strings.HasPrefix(f, "\\") || strings.Contains(f, "..") {
			return false
		}
		if strings.EqualFold(f, "skill.md") {
			hasSkillMd = true
		}
	}
	return hasSkillMd
}

func (p *Provider) FetchSkill(ctx context.Context, rawURL string) (*skills.RemoteSkill, error) {
	result, err := p.fetchIndex(ctx, rawURL)
	if err != nil {
		return nil, err
	}

	skillName := extractSkillName(rawURL)
	if skillName == "" && len(result.Index.Skills) == 1 {
		skillName = result.Index.Skills[0].Name
	}
	if skillName == "" {
		return nil, fmt.Errorf("multiple skills available, use FetchAllSkills")
	}

	for _, entry := range result.Index.Skills {
		if entry.Name == skillName {
			return p.fetchSkillByEntry(ctx, result.ResolvedBaseURL, entry, result.WellKnownPath)
		}
	}
	return nil, fmt.Errorf("skill %q not found in index", skillName)
}

// extractSkillName extracts a skill name from a well-known URL path.
// e.g. "https://example.com/.well-known/agent-skills/my-skill" → "my-skill"
func extractSkillName(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	// Look for /.well-known/{agent-skills,skills}/<name>
	p := strings.TrimSuffix(u.Path, "/")
	for _, wk := range wellKnownDirs {
		prefix := "/" + wk + "/"
		if after, ok := strings.CutPrefix(p, prefix); ok {
			if after != "" && after != "index.json" && !strings.Contains(after, "/") {
				return after
			}
		}
		// Also check if wk dir appears deeper in the path
		if idx := strings.Index(p, "/"+wk+"/"); idx >= 0 {
			after := p[idx+len("/"+wk+"/"):]
			if after != "" && after != "index.json" && !strings.Contains(after, "/") {
				return after
			}
		}
	}
	return ""
}

func (p *Provider) fetchSkillByEntry(ctx context.Context, baseURL string, entry SkillEntry, wkPath string) (*skills.RemoteSkill, error) {
	skillBaseURL := strings.TrimSuffix(baseURL, "/") + "/" + path.Join(wkPath, entry.Name)

	skillMdURL := skillBaseURL + "/SKILL.md"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, skillMdURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch SKILL.md: %d", resp.StatusCode)
	}

	contentBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	content := string(contentBytes)

	s, err := skills.ParseSkillBytes([]byte(content))
	if err != nil {
		return nil, err
	}

	files := map[string]string{"SKILL.md": content}

	for _, filePath := range entry.Files {
		if strings.EqualFold(filePath, "skill.md") {
			continue
		}
		fileURL := skillBaseURL + "/" + filePath
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
		if err != nil {
			continue
		}
		resp, err := p.client().Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}
		fileBytes, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			continue
		}
		files[filePath] = string(fileBytes)
	}

	return &skills.RemoteSkill{
		Name:        s.Name,
		Description: s.Description,
		Content:     content,
		InstallName: entry.Name,
		SourceURL:   skillMdURL,
		ProviderID:  "well-known",
		Files:       files,
	}, nil
}

func (p *Provider) FetchAllSkills(ctx context.Context, rawURL string) ([]*skills.RemoteSkill, error) {
	result, err := p.fetchIndex(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	var out []*skills.RemoteSkill
	for _, entry := range result.Index.Skills {
		s, err := p.fetchSkillByEntry(ctx, result.ResolvedBaseURL, entry, result.WellKnownPath)
		if err != nil {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

func (p *Provider) ToRawURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	if strings.HasSuffix(strings.ToLower(rawURL), "/skill.md") {
		return rawURL
	}
	if name := extractSkillName(rawURL); name != "" {
		// Strip everything from /.well-known/ onwards to get the base
		urlPath := u.Path
		for _, wk := range wellKnownDirs {
			if idx := strings.Index(urlPath, "/"+wk); idx >= 0 {
				urlPath = urlPath[:idx]
				break
			}
		}
		return fmt.Sprintf("%s://%s%s/%s/%s/SKILL.md", u.Scheme, u.Host, urlPath, wellKnownDirs[0], name)
	}
	basePath := strings.TrimSuffix(u.Path, "/")
	return fmt.Sprintf("%s://%s%s/%s/index.json", u.Scheme, u.Host, basePath, wellKnownDirs[0])
}

func (p *Provider) GetSourceIdentifier(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "unknown"
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}
