package skills

import (
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"
)

// SourceType identifies how a source was parsed.
type SourceType string

const (
	SourceGitHub    SourceType = "github"
	SourceGitLab    SourceType = "gitlab"
	SourceGit       SourceType = "git"
	SourceLocal     SourceType = "local"
	SourceWellKnown SourceType = "well-known"
)

// ParsedSource is the result of parsing a source string.
type ParsedSource struct {
	Type        SourceType
	URL         string // For local sources, this is the absolute path.
	Subpath     string
	Ref         string
	SkillFilter string
}

// sourceAliases maps common shorthand to canonical source.
var sourceAliases = map[string]string{
	"coinbase/agentWallet": "coinbase/agentic-wallet-skills",
}

// ParseSource parses a source string into a structured format.
// Supports: local paths, GitHub URLs/shorthand, GitLab URLs, well-known URLs, git URLs.
func ParseSource(input string) (ParsedSource, error) {
	if alias, ok := sourceAliases[input]; ok {
		input = alias
	}

	// Prefix shorthand: github:... / gitlab:...
	if after, ok := strings.CutPrefix(input, "github:"); ok {
		return ParseSource(after)
	}
	if after, ok := strings.CutPrefix(input, "gitlab:"); ok {
		return ParseSource("https://gitlab.com/" + after)
	}

	// Local path
	if isLocalPath(input) {
		resolved, _ := filepath.Abs(input)
		return ParsedSource{Type: SourceLocal, URL: resolved}, nil
	}

	// Try URL parse for http(s) URLs
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		if ps, err := parseHTTPSource(input); err == nil {
			return ps, nil
		}
	}

	// GitHub shorthand (no scheme, no local path prefix)
	if !strings.Contains(input, ":") && !strings.HasPrefix(input, ".") && !strings.HasPrefix(input, "/") {
		if ps, err := parseGitHubShorthand(input); err == nil {
			return ps, nil
		} else if err != nil {
			// propagate sanitizeSubpath errors
			return ParsedSource{}, err
		}
	}

	// Well-known URL
	if isWellKnownURL(input) {
		return ParsedSource{Type: SourceWellKnown, URL: input}, nil
	}

	// Fallback: direct git URL
	return ParsedSource{Type: SourceGit, URL: input}, nil
}

// parseHTTPSource handles http(s):// URLs for GitHub, GitLab, and well-known.
func parseHTTPSource(input string) (ParsedSource, error) {
	u, err := url.Parse(input)
	if err != nil {
		return ParsedSource{}, err
	}

	segments := splitPath(u.Path)

	switch u.Hostname() {
	case "github.com":
		return parseGitHubURL(u, segments)
	case "gitlab.com":
		return parseGitLabURL(u, segments)
	default:
		// Check for GitLab-style /-/tree/ pattern on any host
		if ps, err := parseGitLabURL(u, segments); err == nil {
			return ps, nil
		}
		// Well-known
		if isWellKnownURL(input) {
			return ParsedSource{Type: SourceWellKnown, URL: input}, nil
		}
		return ParsedSource{}, fmt.Errorf("unrecognized URL: %s", input)
	}
}

// parseGitHubURL parses github.com URLs.
// Patterns:
//   - /owner/repo
//   - /owner/repo/tree/ref
//   - /owner/repo/tree/ref/subpath...
func parseGitHubURL(u *url.URL, segments []string) (ParsedSource, error) {
	if len(segments) < 2 {
		return ParsedSource{}, fmt.Errorf("GitHub URL needs owner/repo")
	}
	owner, repo := segments[0], trimGitSuffix(segments[1])
	gitURL := fmt.Sprintf("https://github.com/%s/%s.git", owner, repo)

	// /owner/repo
	if len(segments) == 2 {
		return ParsedSource{Type: SourceGitHub, URL: gitURL}, nil
	}

	// /owner/repo/tree/ref[/subpath...]
	if len(segments) >= 4 && segments[2] == "tree" {
		ref := segments[3]
		if len(segments) > 4 {
			subpath := path.Join(segments[4:]...)
			subpath, err := sanitizeSubpath(subpath)
			if err != nil {
				return ParsedSource{}, err
			}
			return ParsedSource{Type: SourceGitHub, URL: gitURL, Ref: ref, Subpath: subpath}, nil
		}
		return ParsedSource{Type: SourceGitHub, URL: gitURL, Ref: ref}, nil
	}

	// Fallback: just owner/repo (ignore extra segments)
	return ParsedSource{Type: SourceGitHub, URL: gitURL}, nil
}

// parseGitLabURL parses GitLab URLs.
// Patterns:
//   - /group[/subgroup]/repo
//   - /group[/subgroup]/repo/-/tree/ref
//   - /group[/subgroup]/repo/-/tree/ref/subpath...
func parseGitLabURL(u *url.URL, segments []string) (ParsedSource, error) {
	if u.Hostname() == "github.com" {
		return ParsedSource{}, fmt.Errorf("not a GitLab URL")
	}

	// Find /-/tree/ separator
	treeIdx := -1
	for i, s := range segments {
		if s == "-" && i+1 < len(segments) && segments[i+1] == "tree" {
			treeIdx = i
			break
		}
	}

	if treeIdx >= 2 && treeIdx+2 < len(segments) {
		// Has /-/tree/ref[/subpath]
		repoPath := trimGitSuffix(path.Join(segments[:treeIdx]...))
		ref := segments[treeIdx+2]
		gitURL := fmt.Sprintf("%s://%s/%s.git", u.Scheme, u.Host, repoPath)

		if treeIdx+3 < len(segments) {
			subpath := path.Join(segments[treeIdx+3:]...)
			subpath, err := sanitizeSubpath(subpath)
			if err != nil {
				return ParsedSource{}, err
			}
			return ParsedSource{Type: SourceGitLab, URL: gitURL, Ref: ref, Subpath: subpath}, nil
		}
		return ParsedSource{Type: SourceGitLab, URL: gitURL, Ref: ref}, nil
	}

	// gitlab.com without /-/tree/: must be gitlab.com host and have owner/repo
	if u.Hostname() == "gitlab.com" && len(segments) >= 2 {
		repoPath := trimGitSuffix(path.Join(segments...))
		return ParsedSource{
			Type: SourceGitLab,
			URL:  fmt.Sprintf("https://gitlab.com/%s.git", repoPath),
		}, nil
	}

	return ParsedSource{}, fmt.Errorf("not a GitLab URL")
}

// parseGitHubShorthand parses owner/repo, owner/repo/subpath, owner/repo@skill.
func parseGitHubShorthand(input string) (ParsedSource, error) {
	// owner/repo@skill-name
	if atIdx := strings.LastIndex(input, "@"); atIdx > 0 {
		base := input[:atIdx]
		skill := input[atIdx+1:]
		parts := strings.SplitN(base, "/", 3)
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			return ParsedSource{
				Type:        SourceGitHub,
				URL:         fmt.Sprintf("https://github.com/%s/%s.git", parts[0], parts[1]),
				SkillFilter: skill,
			}, nil
		}
	}

	// owner/repo or owner/repo/subpath...
	parts := strings.SplitN(input, "/", 3)
	if len(parts) >= 2 && parts[0] != "" && parts[1] != "" {
		ps := ParsedSource{
			Type: SourceGitHub,
			URL:  fmt.Sprintf("https://github.com/%s/%s.git", parts[0], parts[1]),
		}
		if len(parts) == 3 && parts[2] != "" {
			subpath, err := sanitizeSubpath(parts[2])
			if err != nil {
				return ParsedSource{}, err
			}
			ps.Subpath = subpath
		}
		return ps, nil
	}

	return ParsedSource{}, fmt.Errorf("not a shorthand: %s", input)
}

// GetOwnerRepo extracts owner/repo from a parsed source for lockfile tracking.
func GetOwnerRepo(ps ParsedSource) string {
	if ps.Type == SourceLocal {
		return ""
	}
	// SSH URLs: git@host:path
	if strings.HasPrefix(ps.URL, "git@") {
		_, after, ok := strings.Cut(ps.URL, ":")
		if ok {
			p := trimGitSuffix(after)
			if strings.Contains(p, "/") {
				return p
			}
		}
		return ""
	}
	// HTTP(S)
	u, err := url.Parse(ps.URL)
	if err != nil {
		return ""
	}
	p := strings.TrimPrefix(u.Path, "/")
	p = trimGitSuffix(p)
	if strings.Contains(p, "/") {
		return p
	}
	return ""
}

func isLocalPath(input string) bool {
	return filepath.IsAbs(input) ||
		strings.HasPrefix(input, "./") ||
		strings.HasPrefix(input, "../") ||
		input == "." ||
		input == ".."
}

func isWellKnownURL(input string) bool {
	if !strings.HasPrefix(input, "http://") && !strings.HasPrefix(input, "https://") {
		return false
	}
	u, err := url.Parse(input)
	if err != nil {
		return false
	}
	switch u.Hostname() {
	case "github.com", "gitlab.com", "raw.githubusercontent.com":
		return false
	}
	if strings.HasSuffix(input, ".git") {
		return false
	}
	return true
}

func sanitizeSubpath(subpath string) (string, error) {
	normalized := strings.ReplaceAll(subpath, "\\", "/")
	for _, seg := range strings.Split(normalized, "/") {
		if seg == ".." {
			return "", fmt.Errorf("unsafe subpath: %q contains path traversal segments", subpath)
		}
	}
	return subpath, nil
}

// splitPath splits a URL path into non-empty segments.
func splitPath(p string) []string {
	p = strings.TrimPrefix(p, "/")
	p = strings.TrimSuffix(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

func trimGitSuffix(s string) string {
	return strings.TrimSuffix(s, ".git")
}
