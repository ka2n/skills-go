package skills

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// InstallMode determines how skills are installed.
type InstallMode string

const (
	InstallSymlink InstallMode = "symlink"
	InstallCopy    InstallMode = "copy"
)

const (
	AgentsDir         = ".agents"
	SkillsSubdir      = "skills"
	UniversalSkillsDir = ".agents/skills"
)

// InstallResult describes the outcome of an install operation.
type InstallResult struct {
	Success       bool
	Path          string
	CanonicalPath string
	Mode          InstallMode
	SymlinkFailed bool
	Error         string
	// SkillName is the name of the skill that was installed.
	SkillName string
	// Err is the structured error (nil on success). Use this for errors.Is/As.
	Err error
	// SkippedFiles lists files that were skipped during remote install (e.g. unsafe paths).
	SkippedFiles []string
	// GlobalLockEntry is populated when a global install succeeds, for lock file updates.
	GlobalLockEntry *GlobalLockEntry
	// ProjectLockEntry is populated when a project install succeeds, for lock file updates.
	ProjectLockEntry *ProjectLockEntry
}

// InstallOptions configures an install operation.
type InstallOptions struct {
	Global  bool
	Cwd     string
	HomeDir string
	Mode    InstallMode
	Agents  map[AgentType]AgentConfig
	// SourceFS is the source filesystem for reading skill files.
	// When set, skill.Path is treated as an FS-relative path and files are read
	// from SourceFS instead of the OS filesystem. Writing still goes to the OS.
	// If nil, skill.Path is treated as an OS absolute path (backward compat).
	SourceFS fs.FS
	// Source is the parsed source used for this install, used to populate lock entries in InstallResult.
	Source *ParsedSource
	// FetchRoot is the root directory of the fetched source (e.g. the clone directory).
	// Used to compute repo-relative skill paths for lock entries.
	FetchRoot string
	// Scope controls which scopes ListInstalledSkills searches.
	// Default (ScopeAll) returns project+global when Global=false, global-only when Global=true.
	Scope ListScope
	// Providers bundles provider interfaces (Fetcher, HashProvider, SourceParsers).
	// When set, high-level APIs use these instead of individual provider arguments.
	Providers *Providers
}

// ListScope controls which scopes ListInstalledSkills returns.
type ListScope int

const (
	// ScopeAll returns both project and global skills (default for backward compat).
	ScopeAll ListScope = iota
	// ScopeProject returns only project-scoped skills.
	ScopeProject
	// ScopeGlobal returns only globally-installed skills.
	ScopeGlobal
)

func (o *InstallOptions) cwd() string {
	if o.Cwd != "" {
		return o.Cwd
	}
	cwd, _ := os.Getwd()
	return cwd
}

func (o *InstallOptions) homeDir() string {
	if o.HomeDir != "" {
		return o.HomeDir
	}
	return UserHomeDir()
}

func (o *InstallOptions) agents() map[AgentType]AgentConfig {
	if o.Agents != nil {
		return o.Agents
	}
	return DefaultAgents(o.homeDir())
}

func (o *InstallOptions) mode() InstallMode {
	if o.Mode != "" {
		return o.Mode
	}
	return InstallSymlink
}

// SanitizeName sanitizes a skill name for use as a directory name.
func SanitizeName(name string) string {
	sanitized := strings.ToLower(name)

	// Replace runs of non-allowed characters with a single hyphen
	var b strings.Builder
	prevHyphen := false
	for _, r := range sanitized {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' {
			b.WriteRune(r)
			prevHyphen = false
		} else if !prevHyphen {
			b.WriteByte('-')
			prevHyphen = true
		}
	}
	sanitized = b.String()

	// Trim leading/trailing dots and hyphens
	sanitized = strings.TrimLeft(sanitized, ".-")
	sanitized = strings.TrimRight(sanitized, ".-")

	if len(sanitized) > 255 {
		sanitized = sanitized[:255]
	}
	if sanitized == "" {
		sanitized = "unnamed-skill"
	}
	return sanitized
}

// CanonicalSkillsDir returns the canonical .agents/skills directory.
// homeDir is the user's home directory (used when global is true).
func CanonicalSkillsDir(global bool, cwd, homeDir string) string {
	var baseDir string
	if global {
		baseDir = homeDir
	} else {
		baseDir = cwd
	}
	return filepath.Join(baseDir, AgentsDir, SkillsSubdir)
}

// AgentBaseDir returns the base skills directory for an agent.
// homeDir is the user's home directory (used when global is true).
func AgentBaseDir(agentType AgentType, agents map[AgentType]AgentConfig, global bool, cwd, homeDir string) string {
	if IsUniversalAgent(agents, agentType) {
		return CanonicalSkillsDir(global, cwd, homeDir)
	}
	cfg := agents[agentType]
	if global {
		if cfg.GlobalSkillsDir == "" {
			return filepath.Join(homeDir, cfg.SkillsDir)
		}
		return cfg.GlobalSkillsDir
	}
	return filepath.Join(cwd, cfg.SkillsDir)
}

func installError(msg string, mode InstallMode) InstallResult {
	return InstallResult{Error: msg, Err: fmt.Errorf("%s", msg), Mode: mode}
}

// InstallSkillForAgent installs a local skill for a single agent.
func InstallSkillForAgent(skill *Skill, agentType AgentType, opts *InstallOptions) InstallResult {
	if opts == nil {
		opts = &InstallOptions{}
	}
	agents := opts.agents()
	cfg, ok := agents[agentType]
	if !ok {
		return installError(fmt.Sprintf("unknown agent: %s", agentType), "")
	}

	if opts.Global && cfg.GlobalSkillsDir == "" {
		return installError(fmt.Sprintf("%s does not support global installation", cfg.DisplayName), opts.mode())
	}

	skillName := SanitizeName(skill.Name)
	cwd := opts.cwd()
	mode := opts.mode()

	home := opts.homeDir()
	canonicalBase := CanonicalSkillsDir(opts.Global, cwd, home)
	canonicalDir := filepath.Join(canonicalBase, skillName)
	agentBase := AgentBaseDir(agentType, agents, opts.Global, cwd, home)
	agentDir := filepath.Join(agentBase, skillName)

	if !isPathSafe(canonicalBase, canonicalDir) || !isPathSafe(agentBase, agentDir) {
		return installError("invalid skill name: potential path traversal", mode)
	}

	buildResult := func(path, canonicalPath string, mode InstallMode, symlinkFailed bool) InstallResult {
		r := InstallResult{Success: true, SkillName: skill.Name, Path: path, CanonicalPath: canonicalPath, Mode: mode, SymlinkFailed: symlinkFailed}
		r.populateLockEntries(skill.Name, skill.Path, opts)
		return r
	}

	// Choose copy function based on whether SourceFS is set
	copyDir := func(src, dest string) error {
		if opts.SourceFS != nil {
			return cleanAndCopyDirFS(opts.SourceFS, src, dest)
		}
		return cleanAndCopyDir(src, dest)
	}

	if mode == InstallCopy {
		if err := copyDir(skill.Path, agentDir); err != nil {
			return installError(err.Error(), mode)
		}
		return buildResult(agentDir, "", InstallCopy, false)
	}

	// Symlink mode
	if err := copyDir(skill.Path, canonicalDir); err != nil {
		return installError(err.Error(), mode)
	}

	// Universal agents with global install: already in canonical dir
	if opts.Global && IsUniversalAgent(agents, agentType) {
		return buildResult(canonicalDir, canonicalDir, InstallSymlink, false)
	}

	if err := createSymlink(canonicalDir, agentDir); err != nil {
		// Fallback to copy
		if err := copyDir(skill.Path, agentDir); err != nil {
			return installError(err.Error(), mode)
		}
		return buildResult(agentDir, canonicalDir, InstallSymlink, true)
	}

	return buildResult(agentDir, canonicalDir, InstallSymlink, false)
}

// InstallRemoteSkillForAgent installs a remote skill (SKILL.md content) for a single agent.
func InstallRemoteSkillForAgent(skill *RemoteSkill, agentType AgentType, opts *InstallOptions) InstallResult {
	if opts == nil {
		opts = &InstallOptions{}
	}
	agents := opts.agents()
	cfg, ok := agents[agentType]
	if !ok {
		return installError(fmt.Sprintf("unknown agent: %s", agentType), "")
	}

	if opts.Global && cfg.GlobalSkillsDir == "" {
		return installError(fmt.Sprintf("%s does not support global installation", cfg.DisplayName), opts.mode())
	}

	skillName := SanitizeName(skill.InstallName)
	cwd := opts.cwd()
	mode := opts.mode()
	home := opts.homeDir()

	canonicalBase := CanonicalSkillsDir(opts.Global, cwd, home)
	canonicalDir := filepath.Join(canonicalBase, skillName)
	agentBase := AgentBaseDir(agentType, agents, opts.Global, cwd, home)
	agentDir := filepath.Join(agentBase, skillName)

	if !isPathSafe(canonicalBase, canonicalDir) || !isPathSafe(agentBase, agentDir) {
		return installError("invalid skill name: potential path traversal", mode)
	}

	var skippedFiles []string

	writeFiles := func(targetDir string) error {
		if err := os.RemoveAll(targetDir); err != nil {
			return err
		}
		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			return err
		}
		files := skill.Files
		if files == nil {
			files = map[string]string{"SKILL.md": skill.Content}
		}
		for path, content := range files {
			fullPath := filepath.Join(targetDir, path)
			if !isPathSafe(targetDir, fullPath) {
				skippedFiles = append(skippedFiles, path)
				continue
			}
			if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
				return err
			}
		}
		return nil
	}

	buildResult := func(path, canonicalPath string, mode InstallMode, symlinkFailed bool) InstallResult {
		return InstallResult{
			Success:       true,
			SkillName:     skill.InstallName,
			Path:          path,
			CanonicalPath: canonicalPath,
			Mode:          mode,
			SymlinkFailed: symlinkFailed,
			SkippedFiles:  skippedFiles,
		}
	}

	if mode == InstallCopy {
		if err := writeFiles(agentDir); err != nil {
			return installError(err.Error(), mode)
		}
		return buildResult(agentDir, "", InstallCopy, false)
	}

	// Symlink mode
	if err := writeFiles(canonicalDir); err != nil {
		return installError(err.Error(), mode)
	}

	if opts.Global && IsUniversalAgent(agents, agentType) {
		return buildResult(canonicalDir, canonicalDir, InstallSymlink, false)
	}

	if err := createSymlink(canonicalDir, agentDir); err != nil {
		if err := writeFiles(agentDir); err != nil {
			return installError(err.Error(), mode)
		}
		return buildResult(agentDir, canonicalDir, InstallSymlink, true)
	}

	return buildResult(agentDir, canonicalDir, InstallSymlink, false)
}

// RemoveSkill removes a skill from all specified agents.
func RemoveSkill(skillName string, agentTypes []AgentType, opts *InstallOptions) error {
	if opts == nil {
		opts = &InstallOptions{}
	}
	agents := opts.agents()
	cwd := opts.cwd()
	sanitized := SanitizeName(skillName)

	home := opts.homeDir()
	canonicalPath := filepath.Join(CanonicalSkillsDir(opts.Global, cwd, home), sanitized)

	for _, agentType := range agentTypes {
		cfg, ok := agents[agentType]
		if !ok {
			return fmt.Errorf("unknown agent: %s", agentType)
		}
		var agentDir string
		if opts.Global && cfg.GlobalSkillsDir != "" {
			agentDir = filepath.Join(cfg.GlobalSkillsDir, sanitized)
		} else {
			agentDir = filepath.Join(cwd, cfg.SkillsDir, sanitized)
		}
		if agentDir == canonicalPath {
			continue
		}
		os.RemoveAll(agentDir)
	}

	return os.RemoveAll(canonicalPath)
}

// ListInstalledSkills lists all installed skills from canonical and agent directories.
// The scope can be controlled via opts.Scope (ScopeAll by default, which returns both
// project and global when opts.Global is false, or global-only when opts.Global is true).
// For explicit control, set opts.Scope to ScopeProject, ScopeGlobal, or ScopeAll.
func ListInstalledSkills(opts *InstallOptions) ([]*InstalledSkill, error) {
	if opts == nil {
		opts = &InstallOptions{}
	}
	agents := opts.agents()
	cwd := opts.cwd()
	home := opts.homeDir()

	type scanTarget struct {
		path      string
		global    bool
		agentType AgentType
	}

	var targets []scanTarget
	seenPaths := map[string]bool{}

	addTarget := func(path string, global bool, agentType AgentType) {
		key := fmt.Sprintf("%s:%v", path, global)
		if seenPaths[key] {
			return
		}
		seenPaths[key] = true
		targets = append(targets, scanTarget{path: path, global: global, agentType: agentType})
	}

	type scopeDef struct {
		global bool
	}
	var scopes []scopeDef
	scope := opts.Scope
	if scope == ScopeAll && opts.Global {
		// Legacy behavior: Global=true with default scope means global-only
		scope = ScopeGlobal
	}
	switch scope {
	case ScopeGlobal:
		scopes = []scopeDef{{global: true}}
	case ScopeProject:
		scopes = []scopeDef{{global: false}}
	default: // ScopeAll
		scopes = []scopeDef{{global: false}, {global: true}}
	}

	for _, scope := range scopes {
		addTarget(CanonicalSkillsDir(scope.global, cwd, home), scope.global, "")
		for agentType, cfg := range agents {
			if scope.global && cfg.GlobalSkillsDir == "" {
				continue
			}
			var dir string
			if scope.global {
				dir = cfg.GlobalSkillsDir
			} else {
				dir = filepath.Join(cwd, cfg.SkillsDir)
			}
			addTarget(dir, scope.global, agentType)
		}
	}

	skillsMap := map[string]*InstalledSkill{}

	for _, target := range targets {
		entries, err := os.ReadDir(target.path)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			// Follow symlinks: entry.IsDir() returns false for symlinks
			fullPath := filepath.Join(target.path, entry.Name())
			info, err := os.Stat(fullPath) // follows symlinks
			if err != nil || !info.IsDir() {
				continue
			}
			skillDir := fullPath
			skillMdPath := filepath.Join(skillDir, "SKILL.md")
			if _, err := os.Stat(skillMdPath); err != nil {
				continue
			}
			s, err := parseSkillMd(skillMdPath)
			if err != nil || s == nil {
				continue
			}

			scope := "project"
			if target.global {
				scope = "global"
			}
			key := scope + ":" + s.Name

			if existing, ok := skillsMap[key]; ok {
				if target.agentType != "" {
					existing.AddAgent(target.agentType)
				}
			} else {
				is := &InstalledSkill{
					Name:          s.Name,
					Description:   s.Description,
					Path:          skillDir,
					CanonicalPath: skillDir,
					Scope:         scope,
					DirName:       entry.Name(),
				}
				if target.agentType != "" {
					is.AddAgent(target.agentType)
				}
				skillsMap[key] = is
			}
		}
	}

	var result []*InstalledSkill
	for _, is := range skillsMap {
		result = append(result, is)
	}
	return result, nil
}

// InstalledSkill represents a skill installed on disk.
type InstalledSkill struct {
	Name          string      `json:"name"`
	Description   string      `json:"description"`
	Path          string      `json:"path"`
	CanonicalPath string      `json:"canonicalPath"`
	Scope         string      `json:"scope"`
	Agents        []AgentType `json:"agents"`
	// DirName is the actual directory name on disk. If it differs from
	// SanitizeName(Name), the install path has diverged from the frontmatter name.
	DirName string `json:"dirName,omitempty"`
}

// NameDiverged returns true if the on-disk directory name differs from what
// SanitizeName would produce for the skill's frontmatter name.
func (is *InstalledSkill) NameDiverged() bool {
	return is.DirName != "" && is.DirName != SanitizeName(is.Name)
}

// AddAgent adds an agent to the installed skill if not already present.
func (is *InstalledSkill) AddAgent(t AgentType) {
	for _, a := range is.Agents {
		if a == t {
			return
		}
	}
	is.Agents = append(is.Agents, t)
}

// populateLockEntries fills lock entry fields on a successful InstallResult based on InstallOptions.Source.
func (r *InstallResult) populateLockEntries(skillName, skillPath string, opts *InstallOptions) {
	if opts.Source == nil {
		return
	}
	ownerRepo := GetOwnerRepo(*opts.Source)
	lockSource := ownerRepo
	if lockSource == "" {
		lockSource = opts.Source.URL
	}
	sourceType := string(opts.Source.Type)

	// Compute repo-relative skill path for lock entries.
	// FetchRoot is the clone directory; skillPath is absolute within it.
	repoRelSkillPath := skillPath
	if opts.FetchRoot != "" {
		if rel, err := filepath.Rel(opts.FetchRoot, skillPath); err == nil {
			repoRelSkillPath = rel
		}
	}

	if opts.Global {
		r.GlobalLockEntry = &GlobalLockEntry{
			Source:     lockSource,
			SourceType: sourceType,
			SourceURL:  opts.Source.URL,
			SkillPath:  filepath.Join(repoRelSkillPath, "SKILL.md"),
		}
	} else {
		r.ProjectLockEntry = &ProjectLockEntry{
			Source:     lockSource,
			SourceType: sourceType,
		}
	}
}

// ResolveSkillInstallPath returns the directory where a skill would be installed
// for the given agent type and options, without actually installing anything.
func ResolveSkillInstallPath(skillName string, agentType AgentType, opts *InstallOptions) (string, error) {
	if opts == nil {
		opts = &InstallOptions{}
	}
	agents := opts.agents()
	if _, ok := agents[agentType]; !ok {
		return "", fmt.Errorf("unknown agent: %s", agentType)
	}
	sanitized := SanitizeName(skillName)
	base := AgentBaseDir(agentType, agents, opts.Global, opts.cwd(), opts.homeDir())
	return filepath.Join(base, sanitized), nil
}

func isPathSafe(basePath, targetPath string) bool {
	base, _ := filepath.Abs(basePath)
	target, _ := filepath.Abs(targetPath)
	return strings.HasPrefix(target, base+string(filepath.Separator)) || target == base
}

func createSymlink(target, linkPath string) error {
	targetAbs, _ := filepath.Abs(target)
	linkAbs, _ := filepath.Abs(linkPath)

	// Resolve real paths
	realTarget, err := filepath.EvalSymlinks(targetAbs)
	if err != nil {
		realTarget = targetAbs
	}
	realLink, err := filepath.EvalSymlinks(linkAbs)
	if err != nil {
		realLink = linkAbs
	}

	if realTarget == realLink {
		return nil
	}

	// Remove existing
	os.RemoveAll(linkPath)

	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		return err
	}

	// Compute relative path
	linkDir := filepath.Dir(linkAbs)
	relPath, err := filepath.Rel(linkDir, target)
	if err != nil {
		relPath = target
	}

	return os.Symlink(relPath, linkPath)
}

var excludeFiles = map[string]bool{"metadata.json": true}
var excludeDirs = map[string]bool{
	".git":            true,
	".svn":            true,
	".hg":             true,
	"__pycache__":     true,
	"__pypackages__":  true,
	"node_modules":    true,
}

func cleanAndCopyDir(src, dest string) error {
	// Copy to a temporary directory in the same parent, then swap in.
	// This minimizes the window where dest does not exist.
	parent := filepath.Dir(dest)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	tmpNew, err := os.MkdirTemp(parent, ".skill-install-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpNew) // clean up on any failure path

	if err := copyDirectory(src, tmpNew); err != nil {
		return err
	}

	// Move old dest aside (if it exists), rename new in, then remove old.
	tmpOld := dest + ".old"
	hasOld := false
	if _, err := os.Stat(dest); err == nil {
		if err := os.Rename(dest, tmpOld); err != nil {
			// Fallback: remove dest directly if rename fails
			os.RemoveAll(dest)
		} else {
			hasOld = true
		}
	}

	if err := os.Rename(tmpNew, dest); err != nil {
		// Restore old if rename failed
		if hasOld {
			os.Rename(tmpOld, dest)
		}
		return fmt.Errorf("rename: %w", err)
	}

	if hasOld {
		os.RemoveAll(tmpOld)
	}
	return nil
}

func copyDirectory(src, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if excludeDirs[name] {
				return filepath.SkipDir
			}
			return nil
		}
		if excludeFiles[name] {
			return nil
		}

		rel, _ := filepath.Rel(src, p)
		destPath := filepath.Join(dest, rel)
		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(destPath, data, 0o644)
	})
}

// cleanAndCopyDirFS copies a directory from an fs.FS source to an OS destination,
// using the same atomic-swap strategy as cleanAndCopyDir.
func cleanAndCopyDirFS(fsys fs.FS, src, dest string) error {
	parent := filepath.Dir(dest)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	tmpNew, err := os.MkdirTemp(parent, ".skill-install-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpNew)

	if err := copyDirectoryFS(fsys, src, tmpNew); err != nil {
		return err
	}

	tmpOld := dest + ".old"
	hasOld := false
	if _, err := os.Stat(dest); err == nil {
		if err := os.Rename(dest, tmpOld); err != nil {
			os.RemoveAll(dest)
		} else {
			hasOld = true
		}
	}

	if err := os.Rename(tmpNew, dest); err != nil {
		if hasOld {
			os.Rename(tmpOld, dest)
		}
		return fmt.Errorf("rename: %w", err)
	}

	if hasOld {
		os.RemoveAll(tmpOld)
	}
	return nil
}

// copyDirectoryFS copies files from an fs.FS source path to an OS destination.
// Source paths use forward slashes; destination paths use OS separators.
func copyDirectoryFS(fsys fs.FS, src, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	return fs.WalkDir(fsys, src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if excludeDirs[name] {
				return fs.SkipDir
			}
			return nil
		}
		if excludeFiles[name] {
			return nil
		}

		// p is a forward-slash FS path; compute relative to src
		rel := strings.TrimPrefix(p, src)
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" {
			return nil
		}
		destPath := filepath.Join(dest, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return err
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		return os.WriteFile(destPath, data, 0o644)
	})
}
