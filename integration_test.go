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
	"github.com/ka2n/skills-go/sk"
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

// testOpts returns DestOptions with isolated homeDir and cwd.
func testOpts(t *testing.T, projectDir string) *skills.DestOptions {
	t.Helper()
	home := t.TempDir()
	return &skills.DestOptions{
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
		if diff := cmp.Diff("vercel-labs/agent-skills", ps.OwnerRepo()); diff != "" {
			t.Errorf("OwnerRepo mismatch:\n%s", diff)
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
	opts := cmpopts.IgnoreFields(skills.Skill{}, "Metadata", "RawFrontmatter", "Body", "RawContent", "Path", "PluginName")
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

	opts := cmpopts.IgnoreFields(skills.Skill{}, "Metadata", "RawFrontmatter", "RawContent", "Path", "PluginName")
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
		got, err := skills.ReadGlobalLockFile(writeTemp(t, globalJSON))
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
		got, err := skills.ReadProjectLockFile(writeTemp(t, projectJSON))
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

func TestDiscoverLocal(t *testing.T) {
	tmp := t.TempDir()
	writeSkill(t, tmp, "skills/my-skill", "my-skill", "My test skill")

	discovered, err := skills.Discover(tmp, "", nil)
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

	discovered, err := skills.Discover(srcDir, "", nil)
	if err != nil {
		t.Fatal(err)
	}

	projectDir := t.TempDir()
	dest := testOpts(t, projectDir)
	dest.Mode = skills.InstallCopy

	result := skills.Install(discovered[0], skills.AgentClaudeCode, nil, dest)
	if !result.Success {
		t.Fatalf("install failed: %s", result.Error)
	}

	// Verify directory tree via golden txtar
	got := golden.Txtar(t, projectDir)
	if diff := golden.Check(t, flagUpdate, "testdata", t.Name(), got); diff != "" {
		t.Errorf("directory tree mismatch:\n%s", diff)
	}

	// Verify listed (isolated home — no global skill leakage)
	installed, _ := skills.ListInstalled(dest)
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
	dest := testOpts(t, projectDir)
	dest.Mode = skills.InstallSymlink

	result := skills.Install(
		mustDiscover(t, srcDir)[0], skills.AgentClaudeCode, nil, dest)
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

	discovered, err := skills.Discover(localDir, "", nil)
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

func TestIntegrationInstallVercel(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	fetcher := &git.Fetcher{}
	localDir, cleanup, err := fetcher.Fetch(context.Background(), mustParseSource(t, "vercel-labs/agent-skills"))
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	discovered, _ := skills.Discover(localDir, "", nil)
	projectDir := t.TempDir()
	dest := testOpts(t, projectDir)
	dest.Mode = skills.InstallCopy

	n := min(3, len(discovered))
	lock := skills.NewProjectLock()

	for _, skill := range discovered[:n] {
		result := skills.Install(skill, skills.AgentClaudeCode, nil, dest)
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
	lock.WriteFile(lockPath)

	got, err := skills.ReadProjectLockFile(lockPath)
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

// --- OnParseError callback ---

func TestDiscoverOnParseError(t *testing.T) {
	tmp := t.TempDir()
	// Create a skill with invalid frontmatter (missing description)
	dir := filepath.Join(tmp, "skills", "bad-skill")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: bad\n---\n"), 0o644)

	var parseErrors []string
	opts := &skills.DiscoverOptions{
		OnParseError: func(path string, err error) {
			parseErrors = append(parseErrors, path+": "+err.Error())
		},
	}
	discovered, err := skills.Discover(tmp, "", opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(discovered) != 0 {
		t.Errorf("expected 0 valid skills, got %d", len(discovered))
	}
	if len(parseErrors) == 0 {
		t.Error("expected parse error callback to be called")
	}
}

// --- RawFrontmatter round-trip ---

func TestSkillMarshalPreservesUnknownFields(t *testing.T) {
	content := `---
name: test-skill
description: A test skill
allowed-tools:
  - bash
  - read
disable-model-invocation: true
---

# Test
`
	s, err := skills.ParseSkillBytes([]byte(content))
	if err != nil {
		t.Fatal(err)
	}

	data, err := s.Marshal()
	if err != nil {
		t.Fatal(err)
	}

	// Re-parse and check unknown fields survived
	reparsed, err := skills.ParseSkillBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	if reparsed.RawFrontmatter["disable-model-invocation"] != true {
		t.Error("disable-model-invocation was lost in round-trip")
	}
	tools, ok := reparsed.RawFrontmatter["allowed-tools"]
	if !ok {
		t.Fatal("allowed-tools was lost in round-trip")
	}
	toolList, ok := tools.([]any)
	if !ok || len(toolList) != 2 {
		t.Errorf("allowed-tools unexpected value: %v", tools)
	}
}

// --- Remove unknown agent guard ---

func TestRemoveUnknownAgent(t *testing.T) {
	tmp := t.TempDir()
	dest := &skills.DestOptions{Cwd: tmp, HomeDir: t.TempDir(), Agents: skills.DefaultAgents(t.TempDir())}
	err := skills.Uninstall("test", []skills.AgentType{"nonexistent-agent"}, dest)
	if err == nil {
		t.Error("expected error for unknown agent type")
	}
}

// --- SkippedFiles in remote install ---

func TestInstallSkipsUnsafePaths(t *testing.T) {
	projectDir := t.TempDir()
	dest := testOpts(t, projectDir)
	dest.Mode = skills.InstallCopy

	rs := &skills.Skill{
		Name:        "test-skill",
		Description: "test",
		Files: map[string]string{
			"SKILL.md":           "---\nname: test-skill\ndescription: test\n---\n",
			"../escape/evil.txt": "pwned",
		},
	}

	result := skills.Install(rs, skills.AgentClaudeCode, nil, dest)
	if !result.Success {
		t.Fatalf("install failed: %s", result.Error)
	}
	if len(result.SkippedFiles) == 0 {
		t.Error("expected skipped files for path traversal attempt")
	}
}

// --- Hidden files are now copied ---

func TestCopyPreservesHiddenFiles(t *testing.T) {
	srcDir := t.TempDir()
	skillDir := filepath.Join(srcDir, "skills", "dotfile-skill")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: dotfile-skill\ndescription: test\n---\n"), 0o644)
	os.WriteFile(filepath.Join(skillDir, ".env.example"), []byte("KEY=value"), 0o644)

	discovered := mustDiscover(t, srcDir)
	projectDir := t.TempDir()
	dest := testOpts(t, projectDir)
	dest.Mode = skills.InstallCopy

	result := skills.Install(discovered[0], skills.AgentClaudeCode, nil, dest)
	if !result.Success {
		t.Fatalf("install failed: %s", result.Error)
	}

	// .env.example should exist in the installed directory
	if _, err := os.Stat(filepath.Join(result.Path, ".env.example")); err != nil {
		t.Error(".env.example was not copied (hidden file exclusion too aggressive)")
	}
}

// --- Err field ---

func TestInstallResultErrField(t *testing.T) {
	projectDir := t.TempDir()
	dest := testOpts(t, projectDir)

	skill := &skills.Skill{Name: "test", Description: "test", Path: "/nonexistent"}
	result := skills.Install(skill, "nonexistent-agent", nil, dest)
	if result.Err == nil {
		t.Error("expected Err to be non-nil for unknown agent")
	}
}

// --- ResolveInstallPath ---

func TestResolveInstallPath(t *testing.T) {
	projectDir := t.TempDir()
	dest := testOpts(t, projectDir)

	path, err := skills.ResolveInstallPath("My Skill", skills.AgentClaudeCode, dest)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(path) {
		t.Error("expected absolute path")
	}
	if filepath.Base(path) != "my-skill" {
		t.Errorf("expected sanitized name 'my-skill', got %s", filepath.Base(path))
	}
}

// --- OnDuplicate callback ---

func TestDiscoverOnDuplicate(t *testing.T) {
	tmp := t.TempDir()
	writeSkill(t, tmp, "skills/my-skill", "my-skill", "First")
	writeSkill(t, tmp, ".github/skills/my-skill", "my-skill", "Second")

	var duplicates []string
	opts := &skills.DiscoverOptions{
		FullDepth: true,
		OnDuplicate: func(name, path1, path2 string) {
			duplicates = append(duplicates, name)
		},
	}
	discovered, err := skills.Discover(tmp, "", opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(discovered) != 1 {
		t.Errorf("expected 1 skill (first wins), got %d", len(discovered))
	}
	if len(duplicates) == 0 {
		t.Error("expected duplicate callback to be called")
	}
}

// --- ComputeFolderHash ---

func TestComputeFolderHashTopLevel(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "file.txt"), []byte("hello"), 0o644)

	hash, err := skills.ComputeFolderHash(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if hash == "" {
		t.Error("hash should not be empty")
	}
}

// --- RestoreFromProjectLock ---

func TestRestoreFromProjectLock(t *testing.T) {
	// Set up a source directory with skills
	srcDir := t.TempDir()
	writeSkill(t, srcDir, "skills/skill-a", "skill-a", "Skill A")
	writeSkill(t, srcDir, "skills/skill-b", "skill-b", "Skill B")

	// Install to get hashes
	projectDir := t.TempDir()
	dest := testOpts(t, projectDir)
	dest.Mode = skills.InstallCopy
	srcAbs, _ := filepath.Abs(srcDir)
	source := skills.ParsedSource{Type: skills.SourceLocal, URL: srcAbs}
	src := &skills.SourceRef{Parsed: &source}

	discovered, _ := skills.Discover(srcDir, "", nil)
	lock := skills.NewProjectLock()
	for _, skill := range discovered {
		r := skills.Install(skill, skills.AgentClaudeCode, src, dest)
		if !r.Success {
			t.Fatalf("setup install failed: %s", r.Error)
		}
		hash, _ := skills.ComputeFolderHash(r.Path)
		lock.SetSkill(skill.Name, skills.ProjectLockEntry{
			Source:       srcAbs,
			SourceType:   "local",
			ComputedHash: hash,
		})
	}

	// Now restore into a fresh project directory
	restoreDir := t.TempDir()
	restoreDest := testOpts(t, restoreDir)
	restoreDest.Mode = skills.InstallCopy

	restoreOpts := &sk.InstallOptions{
		DestOptions: *restoreDest,
	}

	results, err := sk.RestoreFromProjectLock(
		context.Background(), lock,
		[]skills.AgentType{skills.AgentClaudeCode}, restoreOpts,
	)
	if err != nil {
		t.Fatal(err)
	}

	successCount := 0
	for _, r := range results {
		if r.Success {
			successCount++
		}
	}
	if successCount != 2 {
		t.Errorf("expected 2 successful restores, got %d", successCount)
	}

	// Verify skills exist in restored directory
	installed, _ := skills.ListInstalled(restoreDest)
	if len(installed) != 2 {
		t.Errorf("expected 2 installed skills after restore, got %d", len(installed))
	}
}

// --- CheckProjectUpdates ---

func TestCheckProjectUpdates(t *testing.T) {
	srcDir := t.TempDir()
	writeSkill(t, srcDir, "skills/my-skill", "my-skill", "Original")

	// Compute hash of original
	srcAbs, _ := filepath.Abs(srcDir)
	hash, _ := skills.ComputeFolderHash(filepath.Join(srcDir, "skills/my-skill"))

	lock := skills.NewProjectLock()
	lock.SetSkill("my-skill", skills.ProjectLockEntry{
		Source:       srcAbs,
		SourceType:   "local",
		ComputedHash: hash,
	})

	// No updates yet
	updates, err := sk.CheckProjectUpdates(
		context.Background(), lock,
		&sk.InstallOptions{DestOptions: skills.DestOptions{Cwd: t.TempDir()}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 0 {
		t.Errorf("expected 0 updates, got %d", len(updates))
	}

	// Modify the source skill
	os.WriteFile(
		filepath.Join(srcDir, "skills/my-skill/SKILL.md"),
		[]byte("---\nname: my-skill\ndescription: Modified\n---\n\nUpdated content\n"),
		0o644,
	)

	// Now should detect update
	updates, err = sk.CheckProjectUpdates(
		context.Background(), lock,
		&sk.InstallOptions{DestOptions: skills.DestOptions{Cwd: t.TempDir()}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 1 {
		t.Errorf("expected 1 update, got %d", len(updates))
	}
	if len(updates) > 0 && updates[0].Name != "my-skill" {
		t.Errorf("expected update for my-skill, got %s", updates[0].Name)
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
	s, err := skills.Discover(dir, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(s) == 0 {
		t.Fatal("no skills discovered")
	}
	return s
}
