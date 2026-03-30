package skills_test

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/tenntenn/golden"

	skills "github.com/ka2n/skills-go"
	"github.com/ka2n/skills-go/provider/git"
	"github.com/ka2n/skills-go/provider/local"
)

var flagUpdate bool

func init() {
	flag.BoolVar(&flagUpdate, "update", false, "update golden files")
}

func mustParseSource(t *testing.T, input string) skills.ParsedSource {
	t.Helper()
	ps, err := skills.ParseSource(input)
	if err != nil {
		t.Fatalf("ParseSource(%q): %v", input, err)
	}
	return ps
}

// testOpts returns InstallOptions with isolated homeDir and cwd.
func testOpts(t *testing.T, projectDir string) *skills.InstallOptions {
	t.Helper()
	home := t.TempDir()
	return &skills.InstallOptions{
		Cwd:     projectDir,
		HomeDir: home,
		Agents:  skills.DefaultAgents(home),
	}
}

// --- Source Parsing ---

func TestSourceParsing(t *testing.T) {
	tests := []struct {
		input    string
		wantType skills.SourceType
		wantURL  string
	}{
		{"vercel-labs/agent-skills", skills.SourceGitHub, "https://github.com/vercel-labs/agent-skills.git"},
		{"vercel-labs/agent-skills@pr-review", skills.SourceGitHub, "https://github.com/vercel-labs/agent-skills.git"},
		{"https://github.com/anthropics/claude-code-sdk-python", skills.SourceGitHub, "https://github.com/anthropics/claude-code-sdk-python.git"},
		{"https://github.com/vercel-labs/agent-skills/tree/main/skills/pr-review", skills.SourceGitHub, "https://github.com/vercel-labs/agent-skills.git"},
		{"https://gitlab.com/group/repo", skills.SourceGitLab, "https://gitlab.com/group/repo.git"},
		{"gitlab:group/repo", skills.SourceGitLab, "https://gitlab.com/group/repo.git"},
		{"./local/path", skills.SourceLocal, ""},
		{"https://example.com", skills.SourceWellKnown, "https://example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := mustParseSource(t, tt.input)
			want := skills.ParsedSource{Type: tt.wantType, URL: tt.wantURL}
			opts := cmpopts.IgnoreFields(skills.ParsedSource{}, "Subpath", "Ref", "SkillFilter")
			if tt.wantURL == "" {
				opts = cmpopts.IgnoreFields(skills.ParsedSource{}, "URL", "Subpath", "Ref", "SkillFilter")
			}
			if diff := cmp.Diff(want, got, opts); diff != "" {
				t.Errorf("ParseSource(%q) mismatch (-want +got):\n%s", tt.input, diff)
			}
		})
	}
}

func TestSourceParsingDetails(t *testing.T) {
	t.Run("skill filter", func(t *testing.T) {
		got := mustParseSource(t, "vercel-labs/agent-skills@pr-review")
		if diff := cmp.Diff("pr-review", got.SkillFilter); diff != "" {
			t.Errorf("SkillFilter mismatch:\n%s", diff)
		}
	})

	t.Run("subpath and ref", func(t *testing.T) {
		got := mustParseSource(t, "https://github.com/vercel-labs/agent-skills/tree/main/skills/pr-review")
		if diff := cmp.Diff("skills/pr-review", got.Subpath); diff != "" {
			t.Errorf("Subpath mismatch:\n%s", diff)
		}
		if diff := cmp.Diff("main", got.Ref); diff != "" {
			t.Errorf("Ref mismatch:\n%s", diff)
		}
	})

	t.Run("path traversal rejected", func(t *testing.T) {
		_, err := skills.ParseSource("owner/repo/../../etc/passwd")
		if err == nil {
			t.Error("expected error for path traversal, got nil")
		}
	})

	t.Run("owner/repo extraction", func(t *testing.T) {
		ps := mustParseSource(t, "vercel-labs/agent-skills")
		if diff := cmp.Diff("vercel-labs/agent-skills", skills.GetOwnerRepo(ps)); diff != "" {
			t.Errorf("GetOwnerRepo mismatch:\n%s", diff)
		}
	})
}

// --- Name Sanitization ---

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"pr-review", "pr-review"},
		{"My Skill", "my-skill"},
		{"../escape", "escape"},
		{"a/b/c", "a-b-c"},
		{"", "unnamed-skill"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if diff := cmp.Diff(tt.want, skills.SanitizeName(tt.input)); diff != "" {
				t.Errorf("SanitizeName(%q) mismatch:\n%s", tt.input, diff)
			}
		})
	}
}

// --- Skill Parsing ---

func TestSkillParsing(t *testing.T) {
	content := `---
name: test-skill
description: A test skill
---

# Test Skill

Instructions here.
`
	got, err := skills.ParseSkillBytes([]byte(content))
	if err != nil {
		t.Fatal(err)
	}

	want := &skills.Skill{Name: "test-skill", Description: "A test skill"}
	opts := cmpopts.IgnoreFields(skills.Skill{}, "Metadata", "Body", "RawContent", "Path", "PluginName")
	if diff := cmp.Diff(want, got, opts); diff != "" {
		t.Errorf("ParseSkillBytes mismatch (-want +got):\n%s", diff)
	}
	if got.RawContent != content {
		t.Error("RawContent not preserved")
	}
}

func TestSkillMarshalRoundTrip(t *testing.T) {
	original := &skills.Skill{Name: "test-skill", Description: "A test skill", Body: "\n# Test\n"}
	data, err := original.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	got, err := skills.ParseSkillBytes(data)
	if err != nil {
		t.Fatal(err)
	}

	opts := cmpopts.IgnoreFields(skills.Skill{}, "Metadata", "RawContent", "Path", "PluginName")
	if diff := cmp.Diff(original, got, opts); diff != "" {
		t.Errorf("round-trip mismatch (-want +got):\n%s", diff)
	}
}

// --- Lock File Compatibility ---

func TestLockFileCompat(t *testing.T) {
	t.Run("global lock v3", func(t *testing.T) {
		globalJSON := `{
  "version": 3,
  "skills": {
    "pr-review": {
      "source": "vercel-labs/agent-skills",
      "sourceType": "github",
      "sourceUrl": "https://github.com/vercel-labs/agent-skills.git",
      "skillPath": "skills/pr-review/SKILL.md",
      "skillFolderHash": "abc123",
      "installedAt": "2024-01-15T10:30:00Z",
      "updatedAt": "2024-01-15T10:30:00Z"
    }
  },
  "dismissed": { "findSkillsPrompt": true },
  "lastSelectedAgents": ["claude-code", "cursor"]
}`
		got, err := skills.ReadGlobalLock(writeTemp(t, globalJSON))
		if err != nil {
			t.Fatal(err)
		}
		want := &skills.GlobalLock{
			Version: 3,
			Skills: map[string]skills.GlobalLockEntry{
				"pr-review": {
					Source:          "vercel-labs/agent-skills",
					SourceType:      "github",
					SourceURL:       "https://github.com/vercel-labs/agent-skills.git",
					SkillPath:       "skills/pr-review/SKILL.md",
					SkillFolderHash: "abc123",
					InstalledAt:     "2024-01-15T10:30:00Z",
					UpdatedAt:       "2024-01-15T10:30:00Z",
				},
			},
			Dismissed:          map[string]bool{"findSkillsPrompt": true},
			LastSelectedAgents: []string{"claude-code", "cursor"},
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("GlobalLock mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("project lock v1", func(t *testing.T) {
		projectJSON := `{
  "version": 1,
  "skills": {
    "commit": {
      "source": "vercel-labs/agent-skills",
      "sourceType": "github",
      "computedHash": "deadbeef"
    }
  }
}`
		got, err := skills.ReadProjectLock(writeTemp(t, projectJSON))
		if err != nil {
			t.Fatal(err)
		}
		want := &skills.ProjectLock{
			Version: 1,
			Skills: map[string]skills.ProjectLockEntry{
				"commit": {Source: "vercel-labs/agent-skills", SourceType: "github", ComputedHash: "deadbeef"},
			},
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("ProjectLock mismatch (-want +got):\n%s", diff)
		}
	})
}

// --- Discovery & Install ---

func TestDiscoverSkillsLocal(t *testing.T) {
	tmp := t.TempDir()
	writeSkill(t, tmp, "skills/my-skill", "my-skill", "My test skill")

	discovered, err := skills.DiscoverSkills(tmp, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(discovered) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(discovered))
	}
	if diff := cmp.Diff("my-skill", discovered[0].Name); diff != "" {
		t.Errorf("Name mismatch:\n%s", diff)
	}
}

func TestInstallAndList(t *testing.T) {
	srcDir := t.TempDir()
	writeSkill(t, srcDir, "skills/test-skill", "test-skill", "Test")

	discovered, err := skills.DiscoverSkills(srcDir, "", nil)
	if err != nil {
		t.Fatal(err)
	}

	projectDir := t.TempDir()
	opts := testOpts(t, projectDir)
	opts.Mode = skills.InstallCopy

	result := skills.InstallSkillForAgent(discovered[0], skills.AgentClaudeCode, opts)
	if !result.Success {
		t.Fatalf("install failed: %s", result.Error)
	}

	// Verify directory tree via golden txtar
	got := golden.Txtar(t, projectDir)
	if diff := golden.Check(t, flagUpdate, "testdata", t.Name(), got); diff != "" {
		t.Errorf("directory tree mismatch:\n%s", diff)
	}

	// Verify listed (isolated home — no global skill leakage)
	installed, _ := skills.ListInstalledSkills(opts)
	names := make([]string, len(installed))
	for i, s := range installed {
		names[i] = s.Name
	}
	if diff := cmp.Diff([]string{"test-skill"}, names, cmpopts.SortSlices(func(a, b string) bool { return a < b })); diff != "" {
		t.Errorf("installed skill names mismatch:\n%s", diff)
	}
}

func TestInstallSymlinkStructure(t *testing.T) {
	srcDir := t.TempDir()
	writeSkill(t, srcDir, "skills/test-compat", "test-compat", "Compatibility test skill")

	projectDir := t.TempDir()
	opts := testOpts(t, projectDir)
	opts.Mode = skills.InstallSymlink

	result := skills.InstallSkillForAgent(
		mustDiscover(t, srcDir)[0], skills.AgentClaudeCode, opts)
	if !result.Success {
		t.Fatalf("install failed: %s", result.Error)
	}

	// Canonical file exists
	if _, err := os.Stat(filepath.Join(projectDir, ".agents/skills/test-compat/SKILL.md")); err != nil {
		t.Error("canonical SKILL.md missing")
	}

	// Agent path is symlink
	info, err := os.Lstat(filepath.Join(projectDir, ".claude/skills/test-compat"))
	if err != nil {
		t.Fatal("agent skill dir missing")
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("expected symlink, got mode %v", info.Mode())
	}

	// Symlink target
	target, _ := os.Readlink(filepath.Join(projectDir, ".claude/skills/test-compat"))
	if diff := cmp.Diff("../../.agents/skills/test-compat", target); diff != "" {
		t.Errorf("symlink target mismatch:\n%s", diff)
	}
}

func TestInitSkill(t *testing.T) {
	tmp := t.TempDir()
	path, err := skills.InitSkill(tmp, "my-new-skill")
	if err != nil {
		t.Fatal(err)
	}
	got := golden.Txtar(t, filepath.Dir(path))
	if diff := golden.Check(t, flagUpdate, "testdata", t.Name(), got); diff != "" {
		t.Errorf("init skill mismatch:\n%s", diff)
	}
}

func TestComputeFolderHash(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "SKILL.md"), []byte("test content"), 0o644)
	os.WriteFile(filepath.Join(tmp, "README.md"), []byte("readme"), 0o644)

	hash1, _ := local.ComputeFolderHash(tmp)
	hash2, _ := local.ComputeFolderHash(tmp)
	if hash1 == "" {
		t.Fatal("hash is empty")
	}
	if hash1 != hash2 {
		t.Error("same content produced different hashes")
	}

	os.WriteFile(filepath.Join(tmp, "SKILL.md"), []byte("changed"), 0o644)
	hash3, _ := local.ComputeFolderHash(tmp)
	if hash1 == hash3 {
		t.Error("different content produced same hash")
	}
}

// --- Integration tests (network) ---

func TestIntegrationGitClone(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	parsed := mustParseSource(t, "vercel-labs/agent-skills")
	fetcher := &git.Fetcher{}
	localDir, cleanup, err := fetcher.Fetch(context.Background(), parsed)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer cleanup()

	discovered, err := skills.DiscoverSkills(localDir, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(discovered) < 3 {
		t.Errorf("expected ≥3 skills, got %d", len(discovered))
	}
	for _, s := range discovered {
		t.Logf("  %s: %s", s.Name, s.Description)
	}
}

func TestIntegrationInstallVercelSkills(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	fetcher := &git.Fetcher{}
	localDir, cleanup, err := fetcher.Fetch(context.Background(), mustParseSource(t, "vercel-labs/agent-skills"))
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	discovered, _ := skills.DiscoverSkills(localDir, "", nil)
	projectDir := t.TempDir()
	opts := testOpts(t, projectDir)
	opts.Mode = skills.InstallCopy

	n := min(3, len(discovered))
	lock := skills.NewProjectLock()

	for _, skill := range discovered[:n] {
		result := skills.InstallSkillForAgent(skill, skills.AgentClaudeCode, opts)
		if !result.Success {
			t.Errorf("install %s: %s", skill.Name, result.Error)
			continue
		}
		hash, _ := local.ComputeFolderHash(result.Path)
		lock.SetSkill(skill.Name, skills.ProjectLockEntry{
			Source: "vercel-labs/agent-skills", SourceType: "github", ComputedHash: hash,
		})
		t.Logf("✓ %s → %s", skill.Name, result.Path)
	}

	lockPath := skills.ProjectLockPath(projectDir)
	lock.Write(lockPath)

	got, err := skills.ReadProjectLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(1, got.Version); diff != "" {
		t.Errorf("lockfile version:\n%s", diff)
	}
	if len(got.Skills) != n {
		t.Errorf("expected %d skills in lock, got %d", n, len(got.Skills))
	}
}

// --- helpers ---

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "data.json")
	os.WriteFile(f, []byte(content), 0o644)
	return f
}

func writeSkill(t *testing.T, base, relDir, name, desc string) {
	t.Helper()
	dir := filepath.Join(base, relDir)
	os.MkdirAll(dir, 0o755)
	content := "---\nname: " + name + "\ndescription: " + desc + "\n---\n\n# " + name + "\n"
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644)
}

func mustDiscover(t *testing.T, dir string) []*skills.Skill {
	t.Helper()
	s, err := skills.DiscoverSkills(dir, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(s) == 0 {
		t.Fatal("no skills discovered")
	}
	return s
}
