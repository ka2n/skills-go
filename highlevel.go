package skills

import (
	"context"
	"fmt"
	"path/filepath"
)

// InstallFromLocalOptions configures a batch local install.
type InstallFromLocalOptions struct {
	InstallOptions
	FullDepth       bool
	IncludeInternal bool
	SkillFilter     []string // if non-empty, only install skills matching these names
	DiscoverOptions *DiscoverOptions
}

// InstallFromLocal discovers skills in srcDir and installs them for the given agents.
func InstallFromLocal(srcDir string, agents []AgentType, opts *InstallFromLocalOptions) ([]InstallResult, error) {
	if opts == nil {
		opts = &InstallFromLocalOptions{}
	}

	discoverOpts := opts.DiscoverOptions
	if discoverOpts == nil {
		discoverOpts = &DiscoverOptions{
			FullDepth:       opts.FullDepth,
			IncludeInternal: opts.IncludeInternal,
			Agents:          opts.Agents,
		}
	}

	discovered, err := DiscoverSkills(srcDir, "", discoverOpts)
	if err != nil {
		return nil, fmt.Errorf("discovering skills: %w", err)
	}

	if len(opts.SkillFilter) > 0 {
		discovered = FilterSkills(discovered, opts.SkillFilter)
	}

	if len(discovered) == 0 {
		return nil, nil
	}

	var results []InstallResult
	for _, skill := range discovered {
		for _, agent := range agents {
			r := InstallSkillForAgent(skill, agent, &opts.InstallOptions)
			results = append(results, r)
		}
	}
	return results, nil
}

// InstallSkillsForAgent installs multiple skills for a single agent (batch convenience).
func InstallSkillsForAgent(skills []*Skill, agentType AgentType, opts *InstallOptions) []InstallResult {
	results := make([]InstallResult, len(skills))
	for i, skill := range skills {
		results[i] = InstallSkillForAgent(skill, agentType, opts)
	}
	return results
}

// UpdateInfo describes a single skill that has an available update.
type UpdateInfo struct {
	Name    string
	Source  string
	OldHash string
	NewHash string
}

// CheckUpdates checks the global lock for skills with outdated hashes.
// Uses opts.Providers.HashProvider to fetch remote hashes.
func CheckUpdates(ctx context.Context, lock *GlobalLock, opts *InstallOptions) ([]UpdateInfo, error) {
	if opts == nil {
		opts = &InstallOptions{}
	}
	if lock == nil || len(lock.Skills) == 0 {
		return nil, nil
	}
	hp := opts.Providers.hashProvider()
	if hp == nil {
		return nil, fmt.Errorf("HashProvider is required for CheckUpdates")
	}

	var updates []UpdateInfo
	for name, entry := range lock.Skills {
		if entry.SkillFolderHash == "" || entry.SkillPath == "" {
			continue
		}
		hash, err := hp.FetchFolderHash(ctx, entry.Source, entry.SkillPath)
		if err != nil {
			continue
		}
		if hash != entry.SkillFolderHash {
			updates = append(updates, UpdateInfo{
				Name:    name,
				Source:  entry.Source,
				OldHash: entry.SkillFolderHash,
				NewHash: hash,
			})
		}
	}
	return updates, nil
}

// UpdateAll fetches and reinstalls all global skills that have available updates.
// It modifies the lock in-place; the caller is responsible for writing it to disk.
// Uses opts.Providers.Fetcher and opts.Providers.HashProvider.
func UpdateAll(ctx context.Context, lock *GlobalLock, agents []AgentType, opts *InstallOptions) ([]InstallResult, error) {
	if opts == nil {
		opts = &InstallOptions{}
	}
	updates, err := CheckUpdates(ctx, lock, opts)
	if err != nil {
		return nil, err
	}
	if len(updates) == 0 {
		return nil, nil
	}

	p := opts.Providers
	fetcher := p.fetcher()
	if fetcher == nil {
		return nil, fmt.Errorf("Fetcher is required for UpdateAll")
	}

	var allResults []InstallResult
	for _, u := range updates {
		entry := lock.Skills[u.Name]
		source, err := p.parseSource(entry.SourceURL)
		if err != nil {
			source, err = p.parseSource(entry.Source)
			if err != nil {
				continue
			}
		}

		localDir, cleanup, err := fetcher.Fetch(ctx, source)
		if err != nil {
			continue
		}

		discovered, err := DiscoverSkills(localDir, "", &DiscoverOptions{IncludeInternal: true})
		if err != nil {
			cleanup()
			continue
		}

		matched := FilterSkills(discovered, []string{u.Name})
		if len(matched) == 0 {
			cleanup()
			continue
		}

		installOpts := *opts
		installOpts.Source = &source
		installOpts.FetchRoot = localDir

		for _, agent := range agents {
			r := InstallSkillForAgent(matched[0], agent, &installOpts)
			allResults = append(allResults, r)

			if r.Success {
				newHash, _ := ComputeFolderHash(r.Path)
				updated := entry
				updated.SkillFolderHash = newHash
				lock.SetSkill(u.Name, updated)
			}
		}
		cleanup()
	}

	return allResults, nil
}

// SkillStatus describes the reconciled state of a skill.
type SkillStatus struct {
	Name            string
	Installed       bool
	InstallPath     string
	UpdateAvailable bool
	SourceHash      string
	InstalledHash   string
}

// ReconcileSkills compares discovered skills against installed state.
func ReconcileSkills(discovered []*Skill, agentType AgentType, opts *InstallOptions) ([]SkillStatus, error) {
	if opts == nil {
		opts = &InstallOptions{}
	}

	installed, err := ListInstalledSkills(opts)
	if err != nil {
		return nil, err
	}

	installedMap := make(map[string]*InstalledSkill, len(installed))
	for _, is := range installed {
		installedMap[is.Name] = is
	}

	var statuses []SkillStatus
	for _, skill := range discovered {
		st := SkillStatus{Name: skill.Name}

		if is, ok := installedMap[skill.Name]; ok {
			st.Installed = true
			st.InstallPath = is.Path

			if skill.Path != "" {
				sourceHash, _ := ComputeFolderHash(skill.Path)
				st.SourceHash = sourceHash
			}
			installedHash, _ := ComputeFolderHash(is.Path)
			st.InstalledHash = installedHash

			if st.SourceHash != "" && st.InstalledHash != "" && st.SourceHash != st.InstalledHash {
				st.UpdateAvailable = true
			}
		} else {
			p, err := ResolveSkillInstallPath(skill.Name, agentType, opts)
			if err == nil {
				st.InstallPath = p
			}
		}

		statuses = append(statuses, st)
	}

	discoveredNames := make(map[string]bool, len(discovered))
	for _, s := range discovered {
		discoveredNames[s.Name] = true
	}
	for _, is := range installed {
		if !discoveredNames[is.Name] {
			hash, _ := ComputeFolderHash(is.Path)
			statuses = append(statuses, SkillStatus{
				Name:          is.Name,
				Installed:     true,
				InstallPath:   is.Path,
				InstalledHash: hash,
			})
		}
	}

	for i := range statuses {
		if statuses[i].InstallPath == "" {
			p, _ := ResolveSkillInstallPath(statuses[i].Name, agentType, opts)
			statuses[i].InstallPath = p
		}
	}

	return statuses, nil
}

// fetchOrLocal resolves a parsed source to a local directory.
// For local sources, returns the path directly. For remote sources, uses the fetcher.
func fetchOrLocal(ctx context.Context, fetcher Fetcher, parsed ParsedSource) (localDir string, cleanup func(), err error) {
	if parsed.Type == SourceLocal {
		return parsed.URL, func() {}, nil
	}
	if fetcher == nil {
		return "", nil, fmt.Errorf("Fetcher is required for remote source %s", parsed.URL)
	}
	return fetcher.Fetch(ctx, parsed)
}

// RestoreFromProjectLock reads a project lock file and reinstalls all tracked skills,
// similar to `npm install` restoring from package-lock.json.
// Uses opts.Providers.Fetcher for git-based sources.
func RestoreFromProjectLock(ctx context.Context, lock *ProjectLock, agents []AgentType, opts *InstallOptions) ([]InstallResult, error) {
	if opts == nil {
		opts = &InstallOptions{}
	}
	if lock == nil || len(lock.Skills) == 0 {
		return nil, nil
	}

	// Group skills by source
	type sourceGroup struct {
		sourceType string
		skills     []string
	}
	groups := map[string]*sourceGroup{}
	for name, entry := range lock.Skills {
		g, ok := groups[entry.Source]
		if !ok {
			g = &sourceGroup{sourceType: entry.SourceType}
			groups[entry.Source] = g
		}
		g.skills = append(g.skills, name)
	}

	p := opts.Providers
	var allResults []InstallResult
	for source, group := range groups {
		parsed, err := p.parseSource(source)
		if err != nil {
			continue
		}

		localDir, cleanup, err := fetchOrLocal(ctx, p.fetcher(), parsed)
		if err != nil {
			continue
		}

		discovered, err := DiscoverSkills(localDir, "", &DiscoverOptions{IncludeInternal: true})
		if err != nil {
			cleanup()
			continue
		}

		matched := FilterSkills(discovered, group.skills)
		installOpts := *opts
		installOpts.Source = &parsed
		installOpts.FetchRoot = localDir
		for _, skill := range matched {
			for _, agent := range agents {
				r := InstallSkillForAgent(skill, agent, &installOpts)
				allResults = append(allResults, r)
			}
		}
		cleanup()
	}

	return allResults, nil
}

// CheckProjectUpdates checks project-scoped skills for updates by re-fetching
// sources and comparing folder hashes. Works with any git source, not just GitHub.
// Uses opts.Providers.Fetcher for remote sources.
func CheckProjectUpdates(ctx context.Context, lock *ProjectLock, opts *InstallOptions) ([]UpdateInfo, error) {
	if opts == nil {
		opts = &InstallOptions{}
	}
	if lock == nil || len(lock.Skills) == 0 {
		return nil, nil
	}

	// Group by source
	type sourceGroup struct {
		skills map[string]string // name -> computedHash
	}
	groups := map[string]*sourceGroup{}
	for name, entry := range lock.Skills {
		if entry.ComputedHash == "" {
			continue
		}
		g, ok := groups[entry.Source]
		if !ok {
			g = &sourceGroup{skills: map[string]string{}}
			groups[entry.Source] = g
		}
		g.skills[name] = entry.ComputedHash
	}

	p := opts.Providers
	var updates []UpdateInfo
	for source, group := range groups {
		parsed, err := p.parseSource(source)
		if err != nil {
			continue
		}

		localDir, cleanup, err := fetchOrLocal(ctx, p.fetcher(), parsed)
		if err != nil {
			continue
		}

		discovered, err := DiscoverSkills(localDir, "", &DiscoverOptions{IncludeInternal: true})
		if err != nil {
			cleanup()
			continue
		}

		for _, skill := range discovered {
			oldHash, tracked := group.skills[skill.Name]
			if !tracked {
				continue
			}
			newHash, err := ComputeFolderHash(skill.Path)
			if err != nil {
				continue
			}
			if newHash != oldHash {
				updates = append(updates, UpdateInfo{
					Name:    skill.Name,
					Source:  source,
					OldHash: oldHash,
					NewHash: newHash,
				})
			}
		}
		cleanup()
	}

	return updates, nil
}

// UpdateProject fetches and reinstalls project-scoped skills that have updates.
// It modifies the lock in-place; the caller is responsible for writing it to disk.
// Uses opts.Providers.Fetcher for remote sources.
func UpdateProject(ctx context.Context, lock *ProjectLock, agents []AgentType, opts *InstallOptions) ([]InstallResult, error) {
	if opts == nil {
		opts = &InstallOptions{}
	}

	updates, err := CheckProjectUpdates(ctx, lock, opts)
	if err != nil {
		return nil, err
	}
	if len(updates) == 0 {
		return nil, nil
	}

	// Group updates by source
	type sourceGroup struct {
		names []string
	}
	groups := map[string]*sourceGroup{}
	for _, u := range updates {
		g, ok := groups[u.Source]
		if !ok {
			g = &sourceGroup{}
			groups[u.Source] = g
		}
		g.names = append(g.names, u.Name)
	}

	p := opts.Providers
	var allResults []InstallResult
	for source, group := range groups {
		parsed, err := p.parseSource(source)
		if err != nil {
			continue
		}

		localDir, cleanup, err := fetchOrLocal(ctx, p.fetcher(), parsed)
		if err != nil {
			continue
		}

		discovered, err := DiscoverSkills(localDir, "", &DiscoverOptions{IncludeInternal: true})
		if err != nil {
			cleanup()
			continue
		}

		matched := FilterSkills(discovered, group.names)
		installOpts := *opts
		installOpts.Source = &parsed
		installOpts.FetchRoot = localDir

		for _, skill := range matched {
			for _, agent := range agents {
				r := InstallSkillForAgent(skill, agent, &installOpts)
				allResults = append(allResults, r)

				if r.Success {
					newHash, _ := ComputeFolderHash(r.Path)
					entry := lock.Skills[skill.Name]
					entry.ComputedHash = newHash
					lock.SetSkill(skill.Name, entry)
				}
			}
		}
		cleanup()
	}

	return allResults, nil
}

// WriteLockEntries writes install results to the appropriate lock files.
func WriteLockEntries(results []InstallResult, homeDir, projectDir string) error {
	var hasGlobal, hasProject bool
	for _, r := range results {
		if r.GlobalLockEntry != nil {
			hasGlobal = true
		}
		if r.ProjectLockEntry != nil {
			hasProject = true
		}
	}

	if hasGlobal {
		lock, err := ReadGlobalLockFile(GlobalLockPath(homeDir))
		if err != nil {
			return fmt.Errorf("reading global lock: %w", err)
		}
		for _, r := range results {
			if r.GlobalLockEntry != nil && r.Success {
				if r.Path != "" {
					hash, _ := ComputeFolderHash(r.Path)
					r.GlobalLockEntry.SkillFolderHash = hash
				}
				name := r.SkillName
				if name == "" {
					name = filepath.Base(r.Path)
				}
				lock.SetSkill(name, *r.GlobalLockEntry)
			}
		}
		if err := lock.WriteFile(GlobalLockPath(homeDir)); err != nil {
			return fmt.Errorf("writing global lock: %w", err)
		}
	}

	if hasProject {
		lock, err := ReadProjectLockFile(ProjectLockPath(projectDir))
		if err != nil {
			return fmt.Errorf("reading project lock: %w", err)
		}
		for _, r := range results {
			if r.ProjectLockEntry != nil && r.Success {
				if r.Path != "" {
					hash, _ := ComputeFolderHash(r.Path)
					r.ProjectLockEntry.ComputedHash = hash
				}
				name := r.SkillName
				if name == "" {
					name = filepath.Base(r.Path)
				}
				lock.SetSkill(name, *r.ProjectLockEntry)
			}
		}
		if err := lock.WriteFile(ProjectLockPath(projectDir)); err != nil {
			return fmt.Errorf("writing project lock: %w", err)
		}
	}

	return nil
}
