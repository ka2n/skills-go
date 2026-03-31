package skills_test

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	skills "github.com/ka2n/skills-go"
)

func TestDiscoverSkillsFS_Basic(t *testing.T) {
	fsys := fstest.MapFS{
		"skills/my-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: my-skill\ndescription: A test skill\n---\n\n# My Skill\n"),
		},
	}

	discovered, err := skills.DiscoverSkillsFS(fsys, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(discovered) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(discovered))
	}
	if diff := cmp.Diff("my-skill", discovered[0].Name); diff != "" {
		t.Errorf("Name mismatch:\n%s", diff)
	}
	if diff := cmp.Diff("A test skill", discovered[0].Description); diff != "" {
		t.Errorf("Description mismatch:\n%s", diff)
	}
	// Path should be FS-relative
	if diff := cmp.Diff("skills/my-skill", discovered[0].Path); diff != "" {
		t.Errorf("Path mismatch (expected FS-relative):\n%s", diff)
	}
}

func TestDiscoverSkillsFS_MultipleSkills(t *testing.T) {
	fsys := fstest.MapFS{
		"skills/skill-a/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: skill-a\ndescription: Skill A\n---\n"),
		},
		"skills/skill-b/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: skill-b\ndescription: Skill B\n---\n"),
		},
		"skills/skill-b/helper.sh": &fstest.MapFile{
			Data: []byte("#!/bin/bash\necho hello\n"),
		},
	}

	discovered, err := skills.DiscoverSkillsFS(fsys, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(discovered) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(discovered))
	}

	names := make([]string, len(discovered))
	for i, s := range discovered {
		names[i] = s.Name
	}
	wantNames := []string{"skill-a", "skill-b"}
	if diff := cmp.Diff(wantNames, names, cmpopts.SortSlices(func(a, b string) bool { return a < b })); diff != "" {
		t.Errorf("skill names mismatch:\n%s", diff)
	}
}

func TestDiscoverSkillsFS_Subpath(t *testing.T) {
	fsys := fstest.MapFS{
		"some/nested/path/my-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: nested-skill\ndescription: Nested\n---\n"),
		},
	}

	discovered, err := skills.DiscoverSkillsFS(fsys, "some/nested/path", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(discovered) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(discovered))
	}
	if diff := cmp.Diff("nested-skill", discovered[0].Name); diff != "" {
		t.Errorf("Name mismatch:\n%s", diff)
	}
}

func TestDiscoverSkillsFS_DirectSkillDir(t *testing.T) {
	fsys := fstest.MapFS{
		"my-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: direct-skill\ndescription: Direct\n---\n"),
		},
	}

	discovered, err := skills.DiscoverSkillsFS(fsys, "my-skill", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(discovered) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(discovered))
	}
	if diff := cmp.Diff("direct-skill", discovered[0].Name); diff != "" {
		t.Errorf("Name mismatch:\n%s", diff)
	}
}

func TestDiscoverSkillsFS_FullDepth(t *testing.T) {
	fsys := fstest.MapFS{
		"a/b/c/deep-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: deep-skill\ndescription: Deep\n---\n"),
		},
		"skills/top-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: top-skill\ndescription: Top\n---\n"),
		},
	}

	opts := &skills.DiscoverOptions{FullDepth: true}
	discovered, err := skills.DiscoverSkillsFS(fsys, "", opts)
	if err != nil {
		t.Fatal(err)
	}

	names := make([]string, len(discovered))
	for i, s := range discovered {
		names[i] = s.Name
	}
	wantNames := []string{"deep-skill", "top-skill"}
	if diff := cmp.Diff(wantNames, names, cmpopts.SortSlices(func(a, b string) bool { return a < b })); diff != "" {
		t.Errorf("skill names mismatch:\n%s", diff)
	}
}

func TestDiscoverSkillsFS_OnParseError(t *testing.T) {
	fsys := fstest.MapFS{
		"skills/bad-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: bad\n---\n"), // missing description
		},
	}

	var parseErrors []string
	opts := &skills.DiscoverOptions{
		OnParseError: func(path string, err error) {
			parseErrors = append(parseErrors, path)
		},
	}
	discovered, err := skills.DiscoverSkillsFS(fsys, "", opts)
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

func TestDiscoverSkillsFS_OnDuplicate(t *testing.T) {
	fsys := fstest.MapFS{
		"skills/my-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: my-skill\ndescription: First\n---\n"),
		},
		".github/skills/my-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: my-skill\ndescription: Second\n---\n"),
		},
	}

	var duplicates []string
	opts := &skills.DiscoverOptions{
		FullDepth: true,
		OnDuplicate: func(name, path1, path2 string) {
			duplicates = append(duplicates, name)
		},
	}
	discovered, err := skills.DiscoverSkillsFS(fsys, "", opts)
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

func TestDiscoverSkillsFS_EmptyFS(t *testing.T) {
	fsys := fstest.MapFS{}

	discovered, err := skills.DiscoverSkillsFS(fsys, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(discovered) != 0 {
		t.Errorf("expected 0 skills, got %d", len(discovered))
	}
}

// TestDiscoverSkills_BackwardCompat verifies the string-based DiscoverSkills
// still returns absolute paths and works as before.
func TestDiscoverSkills_BackwardCompat(t *testing.T) {
	tmp := t.TempDir()
	writeSkill(t, tmp, "skills/compat-skill", "compat-skill", "Backward compat test")

	discovered, err := skills.DiscoverSkills(tmp, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(discovered) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(discovered))
	}
	if diff := cmp.Diff("compat-skill", discovered[0].Name); diff != "" {
		t.Errorf("Name mismatch:\n%s", diff)
	}
	// Path should be absolute
	if !filepath.IsAbs(discovered[0].Path) {
		t.Errorf("expected absolute path, got %s", discovered[0].Path)
	}
	// Path should point to the actual skill directory
	wantPath := filepath.Join(tmp, "skills", "compat-skill")
	if diff := cmp.Diff(wantPath, discovered[0].Path); diff != "" {
		t.Errorf("Path mismatch:\n%s", diff)
	}
}

// TestInstallSkillForAgent_WithSourceFS tests installing a skill from an fs.FS source.
func TestInstallSkillForAgent_WithSourceFS(t *testing.T) {
	fsys := fstest.MapFS{
		"skills/fs-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: fs-skill\ndescription: FS-sourced skill\n---\n\n# FS Skill\n"),
		},
		"skills/fs-skill/helper.sh": &fstest.MapFile{
			Data: []byte("#!/bin/bash\necho hello\n"),
		},
	}

	// Discover via FS
	discovered, err := skills.DiscoverSkillsFS(fsys, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(discovered) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(discovered))
	}

	// Install with SourceFS
	projectDir := t.TempDir()
	opts := testOpts(t, projectDir)
	opts.Mode = skills.InstallCopy
	opts.SourceFS = fsys

	result := skills.InstallSkillForAgent(discovered[0], skills.AgentClaudeCode, opts)
	if !result.Success {
		t.Fatalf("install failed: %s", result.Error)
	}

	// Verify SKILL.md was copied
	skillMdPath := filepath.Join(result.Path, "SKILL.md")
	data, err := os.ReadFile(skillMdPath)
	if err != nil {
		t.Fatalf("SKILL.md not found at %s: %v", skillMdPath, err)
	}
	if len(data) == 0 {
		t.Error("SKILL.md is empty")
	}

	// Verify helper.sh was copied
	helperPath := filepath.Join(result.Path, "helper.sh")
	helperData, err := os.ReadFile(helperPath)
	if err != nil {
		t.Fatalf("helper.sh not found: %v", err)
	}
	if diff := cmp.Diff("#!/bin/bash\necho hello\n", string(helperData)); diff != "" {
		t.Errorf("helper.sh content mismatch:\n%s", diff)
	}
}

// TestInstallSkillForAgent_WithSourceFS_Symlink tests symlink mode with FS source.
func TestInstallSkillForAgent_WithSourceFS_Symlink(t *testing.T) {
	fsys := fstest.MapFS{
		"skills/sym-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: sym-skill\ndescription: Symlink FS test\n---\n"),
		},
	}

	discovered, err := skills.DiscoverSkillsFS(fsys, "", nil)
	if err != nil {
		t.Fatal(err)
	}

	projectDir := t.TempDir()
	opts := testOpts(t, projectDir)
	opts.Mode = skills.InstallSymlink
	opts.SourceFS = fsys

	result := skills.InstallSkillForAgent(discovered[0], skills.AgentClaudeCode, opts)
	if !result.Success {
		t.Fatalf("install failed: %s", result.Error)
	}

	// Canonical file should exist
	canonicalSkillMd := filepath.Join(projectDir, ".agents/skills/sym-skill/SKILL.md")
	if _, err := os.Stat(canonicalSkillMd); err != nil {
		t.Error("canonical SKILL.md missing")
	}

	// Agent path should be a symlink
	agentPath := filepath.Join(projectDir, ".claude/skills/sym-skill")
	info, err := os.Lstat(agentPath)
	if err != nil {
		t.Fatal("agent skill dir missing")
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("expected symlink, got mode %v", info.Mode())
	}
}

// TestInstallSkillForAgent_WithSourceFS_ExcludesMetadata verifies that
// excluded files (metadata.json) are not copied from FS sources.
func TestInstallSkillForAgent_WithSourceFS_ExcludesMetadata(t *testing.T) {
	fsys := fstest.MapFS{
		"my-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: meta-test\ndescription: Metadata exclusion test\n---\n"),
		},
		"my-skill/metadata.json": &fstest.MapFile{
			Data: []byte(`{"should": "be excluded"}`),
		},
		"my-skill/README.md": &fstest.MapFile{
			Data: []byte("# README\n"),
		},
	}

	discovered, err := skills.DiscoverSkillsFS(fsys, "my-skill", nil)
	if err != nil {
		t.Fatal(err)
	}

	projectDir := t.TempDir()
	opts := testOpts(t, projectDir)
	opts.Mode = skills.InstallCopy
	opts.SourceFS = fsys

	result := skills.InstallSkillForAgent(discovered[0], skills.AgentClaudeCode, opts)
	if !result.Success {
		t.Fatalf("install failed: %s", result.Error)
	}

	// metadata.json should NOT be copied
	if _, err := os.Stat(filepath.Join(result.Path, "metadata.json")); err == nil {
		t.Error("metadata.json should have been excluded")
	}

	// README.md should be copied
	if _, err := os.Stat(filepath.Join(result.Path, "README.md")); err != nil {
		t.Error("README.md should have been copied")
	}
}

// TestDiscoverSkillsFS_SkipDirs verifies that .git, node_modules etc are skipped.
func TestDiscoverSkillsFS_SkipDirs(t *testing.T) {
	fsys := fstest.MapFS{
		"node_modules/some-pkg/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: should-skip\ndescription: In node_modules\n---\n"),
		},
		".git/hooks/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: git-hook\ndescription: In .git\n---\n"),
		},
		"skills/real-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: real-skill\ndescription: Valid skill\n---\n"),
		},
	}

	opts := &skills.DiscoverOptions{FullDepth: true}
	discovered, err := skills.DiscoverSkillsFS(fsys, "", opts)
	if err != nil {
		t.Fatal(err)
	}

	if len(discovered) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(discovered))
	}
	if diff := cmp.Diff("real-skill", discovered[0].Name); diff != "" {
		t.Errorf("Name mismatch:\n%s", diff)
	}
}
