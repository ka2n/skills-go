package github

import (
	"os"
	"os/exec"
	"strings"
	"sync"
)

// TokenFunc is a function that returns a GitHub token.
type TokenFunc func() string

// AutoToken returns a TokenFunc that resolves a GitHub token
// from the environment on first call, caching the result.
// Resolution order: GITHUB_TOKEN, GH_TOKEN, then `gh auth token`.
func AutoToken() TokenFunc {
	return sync.OnceValue(getToken)
}

// StaticToken returns a TokenFunc that always returns the given token.
func StaticToken(token string) TokenFunc {
	return func() string { return token }
}

func getToken() string {
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		return t
	}
	if t := os.Getenv("GH_TOKEN"); t != "" {
		return t
	}
	out, err := exec.Command("gh", "auth", "token").Output()
	if err == nil {
		if t := strings.TrimSpace(string(out)); t != "" {
			return t
		}
	}
	return ""
}
