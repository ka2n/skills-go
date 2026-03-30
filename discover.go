package skills

import (
	"os"
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
	// If nil, DefaultAgents(UserHomeDir()) is used.
	Agents map[AgentType]AgentConfig
}

// DiscoverSkills finds all SKILL.md files in the given directory.
func DiscoverSkills(basePath string, subpath string, opts *DiscoverOptions) ([]*Skill, error) {
	if opts == nil {
		opts = &DiscoverOptions{}
	}

	if subpath != "" && !isSubpathSafe(basePath, subpath) {
		return nil, &PathTraversalError{Subpath: subpath}
	}

	searchPath := basePath
	if subpath != "" {
		searchPath = filepath.Join(basePath, subpath)
	}

	var skills []*Skill
	seen := map[string]bool{}

	addSkill := func(s *Skill) {
		if s == nil || seen[s.Name] {
			return
		}
		if s.Metadata.Internal && !opts.IncludeInternal && !shouldInstallInternalSkills() {
			return
		}
		seen[s.Name] = true
		skills = append(skills, s)
	}

	// If pointing directly at a skill directory
	if hasSkillMd(searchPath) {
		s, _ := parseSkillMd(filepath.Join(searchPath, "SKILL.md"))
		addSkill(s)
		if !opts.FullDepth {
			return skills, nil
		}
	}

	// Build priority search directories from agent configs + well-known locations
	priorityDirs := skillSearchDirs(searchPath, opts.Agents)

	// Add plugin manifest paths
	pluginPaths := getPluginSkillPaths(searchPath)
	priorityDirs = append(priorityDirs, pluginPaths...)

	for _, dir := range priorityDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			skillDir := filepath.Join(dir, entry.Name())
			if hasSkillMd(skillDir) {
				s, _ := parseSkillMd(filepath.Join(skillDir, "SKILL.md"))
				addSkill(s)
			}
		}
	}

	// Fallback to recursive search if nothing found, or if fullDepth
	if len(skills) == 0 || opts.FullDepth {
		allDirs := findSkillDirs(searchPath, 0, 5)
		for _, dir := range allDirs {
			s, _ := parseSkillMd(filepath.Join(dir, "SKILL.md"))
			addSkill(s)
		}
	}

	return skills, nil
}

// FilterSkills filters skills by name (case-insensitive).
func FilterSkills(skills []*Skill, names []string) []*Skill {
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
		agents = DefaultAgents(UserHomeDir())
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

func hasSkillMd(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "SKILL.md"))
	return err == nil && !info.IsDir()
}

func parseSkillMd(path string) (*Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s, err := ParseSkillBytes(data)
	if err != nil {
		return nil, err
	}
	s.Path = filepath.Dir(path)
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
