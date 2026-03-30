package skills_test

import (
	"github.com/goccy/go-json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/tenntenn/golden"
)

// TestCrossValidationNpxVsGo installs the same skill via npx skills and our Go CLI,
// then compares directory trees (via txtar) and lockfile structure (via cmp.Diff).
//
// Requires: node, npx, and the skills binary (go install ./cmd/skills/).
func TestCrossValidationNpxVsGo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping cross-validation test")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	skillsBin, err := exec.LookPath("skills")
	if err != nil {
		t.Skip("skills binary not available; run 'go install ./cmd/skills/' first")
	}

	source := "vercel-labs/agent-skills"
	skillName := "deploy-to-vercel"

	npxDir := t.TempDir()
	goDir := t.TempDir()

	// Install via npx (use node wrapper for Nix compatibility)
	npxScript := fmt.Sprintf(`
		const {execSync} = require('child_process');
		execSync('npx skills add %s --skill %s --agent claude-code -y', {
			cwd: '%s', encoding: 'utf-8', timeout: 120000, stdio: 'pipe'
		});
	`, source, skillName, npxDir)
	if out, err := exec.Command("node", "-e", npxScript).CombinedOutput(); err != nil {
		t.Fatalf("npx skills add failed: %v\n%s", err, out)
	}

	// Install via Go CLI
	goCmd := exec.Command(skillsBin, "add", source, "--skill", skillName, "--agent", "claude-code", "-y")
	goCmd.Dir = goDir
	if out, err := goCmd.CombinedOutput(); err != nil {
		t.Fatalf("skills add (Go) failed: %v\n%s", err, out)
	}

	// 1. SKILL.md content comparison
	npxSkillMd := mustReadFile(t, filepath.Join(npxDir, ".claude/skills", skillName, "SKILL.md"))
	goSkillMd := mustReadFile(t, filepath.Join(goDir, ".claude/skills", skillName, "SKILL.md"))
	if diff := cmp.Diff(npxSkillMd, goSkillMd); diff != "" {
		t.Errorf("SKILL.md content mismatch (-npx +go):\n%s", diff)
	}

	// 2. Installed file tree comparison (resolve symlinks, compare as txtar)
	npxTree := golden.Txtar(t, resolveSymlink(filepath.Join(npxDir, ".claude/skills", skillName)))
	goTree := golden.Txtar(t, resolveSymlink(filepath.Join(goDir, ".claude/skills", skillName)))
	if diff := cmp.Diff(npxTree, goTree); diff != "" {
		t.Errorf("installed file tree mismatch (-npx +go):\n%s", diff)
	}

	// 3. Lockfile structure comparison
	type lockEntry struct {
		Source       string `json:"source"`
		SourceType   string `json:"sourceType"`
		ComputedHash string `json:"computedHash"`
	}
	type lockFile struct {
		Version int                  `json:"version"`
		Skills  map[string]lockEntry `json:"skills"`
	}

	var npxLock, goLock lockFile
	json.Unmarshal([]byte(mustReadFile(t, filepath.Join(npxDir, "skills-lock.json"))), &npxLock)
	json.Unmarshal([]byte(mustReadFile(t, filepath.Join(goDir, "skills-lock.json"))), &goLock)

	// Compare ignoring hash values (algorithm may differ)
	lockOpts := cmpopts.IgnoreFields(lockEntry{}, "ComputedHash")
	if diff := cmp.Diff(npxLock, goLock, lockOpts); diff != "" {
		t.Errorf("lockfile structure mismatch (-npx +go):\n%s", diff)
	}

	// Both must have non-empty hashes
	if npxLock.Skills[skillName].ComputedHash == "" {
		t.Error("npx lockfile has empty computedHash")
	}
	if goLock.Skills[skillName].ComputedHash == "" {
		t.Error("Go lockfile has empty computedHash")
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

func resolveSymlink(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return resolved
}
