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
}

// InstallOptions configures an install operation.
type InstallOptions struct {
	Global  bool
	Cwd     string
	HomeDir string
	Mode    InstallMode
	Agents  map[AgentType]AgentConfig
}

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

// InstallSkillForAgent installs a local skill for a single agent.
func InstallSkillForAgent(skill *Skill, agentType AgentType, opts *InstallOptions) InstallResult {
	if opts == nil {
		opts = &InstallOptions{}
	}
	agents := opts.agents()
	cfg, ok := agents[agentType]
	if !ok {
		return InstallResult{Error: fmt.Sprintf("unknown agent: %s", agentType)}
	}

	if opts.Global && cfg.GlobalSkillsDir == "" {
		return InstallResult{Error: fmt.Sprintf("%s does not support global installation", cfg.DisplayName), Mode: opts.mode()}
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
		return InstallResult{Error: "invalid skill name: potential path traversal", Mode: mode}
	}

	if mode == InstallCopy {
		if err := cleanAndCopyDir(skill.Path, agentDir); err != nil {
			return InstallResult{Error: err.Error(), Mode: mode}
		}
		return InstallResult{Success: true, Path: agentDir, Mode: InstallCopy}
	}

	// Symlink mode
	if err := cleanAndCopyDir(skill.Path, canonicalDir); err != nil {
		return InstallResult{Error: err.Error(), Mode: mode}
	}

	// Universal agents with global install: already in canonical dir
	if opts.Global && IsUniversalAgent(agents, agentType) {
		return InstallResult{Success: true, Path: canonicalDir, CanonicalPath: canonicalDir, Mode: InstallSymlink}
	}

	if err := createSymlink(canonicalDir, agentDir); err != nil {
		// Fallback to copy
		if err := cleanAndCopyDir(skill.Path, agentDir); err != nil {
			return InstallResult{Error: err.Error(), Mode: mode}
		}
		return InstallResult{Success: true, Path: agentDir, CanonicalPath: canonicalDir, Mode: InstallSymlink, SymlinkFailed: true}
	}

	return InstallResult{Success: true, Path: agentDir, CanonicalPath: canonicalDir, Mode: InstallSymlink}
}

// InstallRemoteSkillForAgent installs a remote skill (SKILL.md content) for a single agent.
func InstallRemoteSkillForAgent(skill *RemoteSkill, agentType AgentType, opts *InstallOptions) InstallResult {
	if opts == nil {
		opts = &InstallOptions{}
	}
	agents := opts.agents()
	cfg, ok := agents[agentType]
	if !ok {
		return InstallResult{Error: fmt.Sprintf("unknown agent: %s", agentType)}
	}

	if opts.Global && cfg.GlobalSkillsDir == "" {
		return InstallResult{Error: fmt.Sprintf("%s does not support global installation", cfg.DisplayName), Mode: opts.mode()}
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
		return InstallResult{Error: "invalid skill name: potential path traversal", Mode: mode}
	}

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

	if mode == InstallCopy {
		if err := writeFiles(agentDir); err != nil {
			return InstallResult{Error: err.Error(), Mode: mode}
		}
		return InstallResult{Success: true, Path: agentDir, Mode: InstallCopy}
	}

	// Symlink mode
	if err := writeFiles(canonicalDir); err != nil {
		return InstallResult{Error: err.Error(), Mode: mode}
	}

	if opts.Global && IsUniversalAgent(agents, agentType) {
		return InstallResult{Success: true, Path: canonicalDir, CanonicalPath: canonicalDir, Mode: InstallSymlink}
	}

	if err := createSymlink(canonicalDir, agentDir); err != nil {
		if err := writeFiles(agentDir); err != nil {
			return InstallResult{Error: err.Error(), Mode: mode}
		}
		return InstallResult{Success: true, Path: agentDir, CanonicalPath: canonicalDir, Mode: InstallSymlink, SymlinkFailed: true}
	}

	return InstallResult{Success: true, Path: agentDir, CanonicalPath: canonicalDir, Mode: InstallSymlink}
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
		cfg := agents[agentType]
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
	if opts.Global {
		scopes = []scopeDef{{global: true}}
	} else {
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
var excludeDirs = map[string]bool{".git": true, "__pycache__": true, "__pypackages__": true}

func cleanAndCopyDir(src, dest string) error {
	os.RemoveAll(dest)
	return copyDirectory(src, dest)
}

func copyDirectory(src, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if excludeDirs[name] || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if excludeFiles[name] || strings.HasPrefix(name, ".") {
			return nil
		}

		rel, _ := filepath.Rel(src, path)
		destPath := filepath.Join(dest, rel)
		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(destPath, data, 0o644)
	})
}
