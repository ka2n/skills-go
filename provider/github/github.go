// Package github implements skills.HashProvider using the GitHub Trees API.
package github

import (
	"context"
	"github.com/goccy/go-json"
	"fmt"
	"net/http"
	"strings"

	skills "github.com/ka2n/skills-go"
)

var _ skills.HashProvider = (*HashProvider)(nil)

// HashProvider implements skills.HashProvider using the GitHub Trees API.
type HashProvider struct {
	// Token resolves a GitHub token. If nil, requests are unauthenticated.
	// Use AutoToken() for env/gh-cli resolution, or StaticToken("...") for explicit tokens.
	Token TokenFunc

	// Client is the HTTP client to use. Defaults to http.DefaultClient.
	Client *http.Client
}

func (p *HashProvider) token() string {
	if p.Token != nil {
		return p.Token()
	}
	return ""
}

func (p *HashProvider) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return http.DefaultClient
}

// FetchFolderHash fetches the tree SHA for a skill folder.
// ownerRepo is "owner/repo", skillPath is the path within the repo
// (e.g. "skills/my-skill/SKILL.md"). Tries main and master branches.
func (p *HashProvider) FetchFolderHash(ctx context.Context, ownerRepo, skillPath string) (string, error) {
	folderPath := strings.ReplaceAll(skillPath, "\\", "/")
	folderPath = strings.TrimSuffix(folderPath, "/SKILL.md")
	folderPath = strings.TrimSuffix(folderPath, "SKILL.md")
	folderPath = strings.TrimSuffix(folderPath, "/")

	token := p.token()

	for _, branch := range []string{"main", "master"} {
		hash, err := p.fetchTreeSHA(ctx, ownerRepo, branch, folderPath, token)
		if err == nil && hash != "" {
			return hash, nil
		}
	}
	return "", fmt.Errorf("could not fetch folder hash for %s/%s", ownerRepo, skillPath)
}

type treeResponse struct {
	SHA  string `json:"sha"`
	Tree []struct {
		Path string `json:"path"`
		Type string `json:"type"`
		SHA  string `json:"sha"`
	} `json:"tree"`
}

func (p *HashProvider) fetchTreeSHA(ctx context.Context, ownerRepo, branch, folderPath, token string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/git/trees/%s?recursive=1", ownerRepo, branch)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "skills-go")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := p.client().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var data treeResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}

	if folderPath == "" {
		return data.SHA, nil
	}

	for _, entry := range data.Tree {
		if entry.Type == "tree" && entry.Path == folderPath {
			return entry.SHA, nil
		}
	}

	return "", fmt.Errorf("folder %q not found in tree", folderPath)
}
