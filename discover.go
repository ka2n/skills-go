package skills

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

var skipDirs = map[string]bool{
	"node_modules": true, ".git": true, "dist": true, "build": true, "__pycache__": true,
}

// DiscoverOptions controls skill discovery behavior.
type DiscoverOptions struct {
	IncludeInternal bool
	FullDepth       bool
	// Agents is used to derive priority search directories from AgentConfig.SkillsDir.
	// If nil, DefaultAgents with the current user's home directory is used.
	Agents map[AgentType]AgentConfig
	// OnParseError is called when a SKILL.md file cannot be parsed.
	// If nil, parse errors are silently ignored (legacy behavior).
	OnParseError func(path string, err error)
	// OnDuplicate is called when a skill with the same name is found in multiple directories.
	// path1 is the path of the first occurrence, path2 is the duplicate being skipped.
	// If nil, duplicates are silently ignored (legacy behavior).
	OnDuplicate func(name, path1, path2 string)
}

// Discover finds all SKILL.md files in the given directory.
func Discover(basePath string, subpath string, opts *DiscoverOptions) ([]*Skill, error) {
	if opts == nil {
		opts = &DiscoverOptions{}
	}

	if subpath != "" && !isSubpathSafe(basePath, subpath) {
		return nil, &PathTraversalError{Subpath: subpath}
	}

	root, err := os.OpenRoot(basePath)
	if err != nil {
		return nil, err
	}
	defer root.Close()

	skills, err := DiscoverFS(root.FS(), subpath, opts)
	if err != nil {
		return nil, err
	}

	// Convert FS-relative paths back to absolute OS paths.
	for _, s := range skills {
		s.Path = filepath.Join(basePath, filepath.FromSlash(s.Path))
	}
	return skills, nil
}

// DiscoverFS finds all SKILL.md files in the given fs.FS.
// Paths within the FS use forward slashes (path.Join). Skill.Path is set to
// the FS-relative path (e.g. "skills/my-skill").
func DiscoverFS(fsys fs.FS, subpath string, opts *DiscoverOptions) ([]*Skill, error) {
	if opts == nil {
		opts = &DiscoverOptions{}
	}

	searchPath := "."
	if subpath != "" {
		searchPath = subpath
	}

	var skills []*Skill
	seenPaths := map[string]string{} // name -> first path

	addSkill := func(s *Skill) {
		if s == nil {
			return
		}
		if firstPath, dup := seenPaths[s.Name]; dup {
			if opts.OnDuplicate != nil {
				opts.OnDuplicate(s.Name, firstPath, s.Path)
			}
			return
		}
		if s.Metadata.Internal && !opts.IncludeInternal && !shouldInstallInternalSkills() {
			return
		}
		seenPaths[s.Name] = s.Path
		skills = append(skills, s)
	}

	tryParse := func(skillMdPath string) *Skill {
		s, err := parseSkillMdFS(fsys, skillMdPath)
		if err != nil && opts.OnParseError != nil {
			opts.OnParseError(skillMdPath, err)
		}
		return s
	}

	// If pointing directly at a skill directory
	if hasSkillMdFS(fsys, searchPath) {
		addSkill(tryParse(path.Join(searchPath, "SKILL.md")))
		if !opts.FullDepth {
			return skills, nil
		}
	}

	// Build priority search directories from agent configs + well-known locations
	priorityDirs := skillSearchDirsFS(searchPath, opts.Agents)

	// Add plugin manifest paths (.claude-plugin/marketplace.json, plugin.json)
	pluginPaths := getPluginSkillPathsFS(fsys, searchPath)
	priorityDirs = append(priorityDirs, pluginPaths...)

	for _, dir := range priorityDirs {
		entries, err := fs.ReadDir(fsys, dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			skillDir := path.Join(dir, entry.Name())
			if hasSkillMdFS(fsys, skillDir) {
				addSkill(tryParse(path.Join(skillDir, "SKILL.md")))
			}
		}
	}

	// Fallback to recursive search if nothing found, or if fullDepth
	if len(skills) == 0 || opts.FullDepth {
		allDirs := findSkillDirsFS(fsys, searchPath, 0, 5)
		for _, dir := range allDirs {
			addSkill(tryParse(path.Join(dir, "SKILL.md")))
		}
	}

	return skills, nil
}

// Filter filters skills by name (case-insensitive).
func Filter(skills []*Skill, names []string) []*Skill {
	normalized := make(map[string]bool)
	for _, n := range names {
		normalized[strings.ToLower(n)] = true
	}
	var result []*Skill
	for _, s := range skills {
		if normalized[strings.ToLower(s.Name)] {
			result = append(result, s)
		}
	}
	return result
}

// skillSearchDirs returns directories to search for skills, derived from
// agent SkillsDir values plus well-known conventional locations.
func skillSearchDirs(searchPath string, agents map[AgentType]AgentConfig) []string {
	if agents == nil {
		agents = DefaultAgents(userHomeDir())
	}

	seen := map[string]bool{}
	var dirs []string
	add := func(d string) {
		if seen[d] {
			return
		}
		seen[d] = true
		dirs = append(dirs, d)
	}

	// The search path itself
	add(searchPath)

	// Conventional sub-directories not tied to any agent
	add(filepath.Join(searchPath, "skills"))
	add(filepath.Join(searchPath, "skills/.curated"))
	add(filepath.Join(searchPath, "skills/.experimental"))
	add(filepath.Join(searchPath, "skills/.system"))
	add(filepath.Join(searchPath, ".github/skills"))

	// Agent-derived directories
	for _, cfg := range agents {
		add(filepath.Join(searchPath, cfg.SkillsDir))
	}

	return dirs
}

// skillSearchDirsFS returns directories to search for skills within an fs.FS,
// using forward-slash paths (path.Join).
func skillSearchDirsFS(searchPath string, agents map[AgentType]AgentConfig) []string {
	if agents == nil {
		agents = DefaultAgents(userHomeDir())
	}

	seen := map[string]bool{}
	var dirs []string
	add := func(d string) {
		if seen[d] {
			return
		}
		seen[d] = true
		dirs = append(dirs, d)
	}

	add(searchPath)

	add(path.Join(searchPath, "skills"))
	add(path.Join(searchPath, "skills/.curated"))
	add(path.Join(searchPath, "skills/.experimental"))
	add(path.Join(searchPath, "skills/.system"))
	add(path.Join(searchPath, ".github/skills"))

	for _, cfg := range agents {
		// cfg.SkillsDir uses OS separators; convert to forward slashes for FS
		add(path.Join(searchPath, filepath.ToSlash(cfg.SkillsDir)))
	}

	return dirs
}

func hasSkillMd(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "SKILL.md"))
	return err == nil && !info.IsDir()
}

func hasSkillMdFS(fsys fs.FS, dir string) bool {
	p := path.Join(dir, "SKILL.md")
	info, err := fs.Stat(fsys, p)
	return err == nil && !info.IsDir()
}

func parseSkillMd(fpath string) (*Skill, error) {
	data, err := os.ReadFile(fpath)
	if err != nil {
		return nil, err
	}
	s, err := ParseSkillBytes(data)
	if err != nil {
		return nil, err
	}
	s.Path = filepath.Dir(fpath)
	s.RawContent = string(data)
	return s, nil
}

func parseSkillMdFS(fsys fs.FS, skillMdPath string) (*Skill, error) {
	data, err := fs.ReadFile(fsys, skillMdPath)
	if err != nil {
		return nil, err
	}
	s, err := ParseSkillBytes(data)
	if err != nil {
		return nil, err
	}
	s.Path = path.Dir(skillMdPath)
	s.RawContent = string(data)
	return s, nil
}

func findSkillDirs(dir string, depth, maxDepth int) []string {
	if depth > maxDepth {
		return nil
	}
	var result []string
	if hasSkillMd(dir) {
		result = append(result, dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return result
	}
	for _, entry := range entries {
		if !entry.IsDir() || skipDirs[entry.Name()] {
			continue
		}
		result = append(result, findSkillDirs(filepath.Join(dir, entry.Name()), depth+1, maxDepth)...)
	}
	return result
}

func findSkillDirsFS(fsys fs.FS, dir string, depth, maxDepth int) []string {
	if depth > maxDepth {
		return nil
	}
	var result []string
	if hasSkillMdFS(fsys, dir) {
		result = append(result, dir)
	}
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return result
	}
	for _, entry := range entries {
		if !entry.IsDir() || skipDirs[entry.Name()] {
			continue
		}
		result = append(result, findSkillDirsFS(fsys, path.Join(dir, entry.Name()), depth+1, maxDepth)...)
	}
	return result
}

func isSubpathSafe(basePath, subpath string) bool {
	base, _ := filepath.Abs(basePath)
	target, _ := filepath.Abs(filepath.Join(basePath, subpath))
	return strings.HasPrefix(target, base+string(filepath.Separator)) || target == base
}

func shouldInstallInternalSkills() bool {
	v := os.Getenv("INSTALL_INTERNAL_SKILLS")
	return v == "1" || v == "true"
}

// PathTraversalError is returned when a subpath attempts to escape the base directory.
type PathTraversalError struct {
	Subpath string
}

func (e *PathTraversalError) Error() string {
	return "invalid subpath: " + e.Subpath + " resolves outside the repository directory"
}
