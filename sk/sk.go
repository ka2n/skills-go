// Package sk provides high-level orchestration functions for skill management.
// It builds on top of the low-level primitives in the [github.com/ka2n/skills-go]
// package to provide batch install, update, restore, and reconciliation workflows.
//
// For low-level control (e.g. custom storage backends), use the skills package directly.
//
// # Install skills from a GitHub repository
//
// When opts is nil, [DefaultProviders] is used automatically
// (git fetcher, GitHub hash provider, well-known endpoint support).
//
//	results, err := sk.Install(ctx,
//	    skills.SourceFrom("vercel-labs/agent-skills"),
//	    []skills.AgentType{skills.AgentClaudeCode},
//	    nil,
//	)
//
// # Install skills from a local directory
//
//	results, err := sk.Install(ctx,
//	    skills.SourceFromLocal("./my-skills"),
//	    []skills.AgentType{skills.AgentClaudeCode},
//	    nil,
//	)
//
// # Install skills from an embedded filesystem
//
//	//go:embed skills
//	var skillsFS embed.FS
//
//	results, err := sk.Install(ctx,
//	    skills.SourceFromFS(skillsFS),
//	    []skills.AgentType{skills.AgentClaudeCode},
//	    nil,
//	)
//
// # Install and update project lock file
//
//	results, _ := sk.Install(ctx,
//	    skills.SourceFrom("owner/repo"),
//	    []skills.AgentType{skills.AgentClaudeCode},
//	    nil,
//	)
//	lock, _ := sk.ProjectLock(".")
//	lock.ApplyResults(results)
//	sk.WriteProjectLock(lock, ".")
//
// # Check for updates and apply them
//
//	lock, _ := sk.GlobalLock()
//	updates, _ := sk.CheckUpdates(ctx, lock, nil)
//	if len(updates) > 0 {
//	    sk.UpdateGlobal(ctx, lock, agents, nil)
//	    sk.WriteGlobalLock(lock)
//	}
//
// # Restore project skills from lock file
//
//	lock, _ := sk.ProjectLock(".")
//	sk.RestoreFromProjectLock(ctx, lock, agents, nil)
package sk

import (
	"context"
	"fmt"
	"os"

	skills "github.com/ka2n/skills-go"
	"github.com/ka2n/skills-go/provider/git"
	"github.com/ka2n/skills-go/provider/github"
	"github.com/ka2n/skills-go/provider/wellknown"
)

// DefaultProviders returns the built-in provider set.
// Includes git and well-known fetchers, and GitHub hash provider (with auto token).
func DefaultProviders() *skills.Providers {
	return &skills.Providers{
		Fetcher:      skills.MultiFetcher(&wellknown.Fetcher{}, &git.Fetcher{}),
		HashProvider: &github.HashProvider{Token: github.AutoToken()},
	}
}

// GlobalLock reads the global lock file from the standard path.
// Returns an empty lock if the file does not exist.
func GlobalLock() (*skills.GlobalLock, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolving home directory: %w", err)
	}
	return skills.ReadGlobalLockFile(skills.GlobalLockPath(home))
}

// ProjectLock reads the project lock file from the given project directory.
// Returns an empty lock if the file does not exist.
func ProjectLock(projectDir string) (*skills.ProjectLock, error) {
	return skills.ReadProjectLockFile(skills.ProjectLockPath(projectDir))
}

// InstallOptions configures high-level operations.
type InstallOptions struct {
	skills.DestOptions
	Providers       *skills.Providers
	DiscoverOptions *skills.DiscoverOptions
	SkillFilter     []string
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

// UpdateInfo describes a single skill that has an available update.
type UpdateInfo struct {
	Name    string
	Source  string
	OldHash string
	NewHash string
}

// Install discovers and installs skills from the given source for the specified agents.
//
// The source can be created with [skills.SourceFrom] (GitHub shorthand, URLs, local paths),
// [skills.SourceFromFS] (embed.FS), or [skills.SourceFromLocal] (explicit local path).
//
// For remote sources (GitHub, GitLab, well-known endpoints), a Fetcher is required.
// When opts is nil, [DefaultProviders] is used which includes git and well-known fetchers.
func Install(ctx context.Context, source skills.Source, agents []skills.AgentType, opts *InstallOptions) ([]skills.InstallResult, error) {
	if opts == nil {
		opts = &InstallOptions{}
	}

	discoverOpts := opts.DiscoverOptions
	if discoverOpts == nil {
		discoverOpts = &skills.DiscoverOptions{
			Agents: opts.Agents,
		}
	}

	// FS-backed source
	if source.IsFS() {
		discovered, err := skills.DiscoverFS(source.FS(), "", discoverOpts)
		if err != nil {
			return nil, fmt.Errorf("discovering skills: %w", err)
		}
		if len(opts.SkillFilter) > 0 {
			discovered = skills.Filter(discovered, opts.SkillFilter)
		}
		if len(discovered) == 0 {
			return nil, nil
		}
		src := &skills.SourceRef{FS: source.FS()}
		return installAll(discovered, agents, src, &opts.DestOptions), nil
	}

	// Parse the source string
	var parsed skills.ParsedSource
	var err error
	if source.Raw() != "" {
		parsed, err = parseSource(opts, source.Raw())
		if err != nil {
			return nil, fmt.Errorf("parsing source: %w", err)
		}
	}

	// Local path
	if parsed.Type == skills.SourceLocal {
		return installLocal(parsed, agents, opts, discoverOpts)
	}

	// Remote (git, well-known, etc.): fetch → discover → install
	f := fetcher(opts)
	if f == nil {
		return nil, fmt.Errorf("Fetcher is required for remote source %s", parsed.URL)
	}

	localDir, cleanup, err := f.Fetch(ctx, parsed)
	if err != nil {
		return nil, fmt.Errorf("fetching source: %w", err)
	}
	defer cleanup()

	discovered, err := skills.Discover(localDir, parsed.Subpath, discoverOpts)
	if err != nil {
		return nil, fmt.Errorf("discovering skills: %w", err)
	}

	if parsed.SkillFilter != "" {
		discovered = skills.Filter(discovered, []string{parsed.SkillFilter})
	}
	if len(opts.SkillFilter) > 0 {
		discovered = skills.Filter(discovered, opts.SkillFilter)
	}
	if len(discovered) == 0 {
		return nil, nil
	}

	src := &skills.SourceRef{Parsed: &parsed, FetchRoot: localDir}
	return installAll(discovered, agents, src, &opts.DestOptions), nil
}

func installAll(discovered []*skills.Skill, agents []skills.AgentType, src *skills.SourceRef, dest *skills.DestOptions) []skills.InstallResult {
	var results []skills.InstallResult
	for _, skill := range discovered {
		for _, agent := range agents {
			r := skills.Install(skill, agent, src, dest)
			results = append(results, r)
		}
	}
	return results
}

func installLocal(parsed skills.ParsedSource, agents []skills.AgentType, opts *InstallOptions, discoverOpts *skills.DiscoverOptions) ([]skills.InstallResult, error) {
	discovered, err := skills.Discover(parsed.URL, parsed.Subpath, discoverOpts)
	if err != nil {
		return nil, fmt.Errorf("discovering skills: %w", err)
	}
	if parsed.SkillFilter != "" {
		discovered = skills.Filter(discovered, []string{parsed.SkillFilter})
	}
	if len(opts.SkillFilter) > 0 {
		discovered = skills.Filter(discovered, opts.SkillFilter)
	}
	if len(discovered) == 0 {
		return nil, nil
	}
	src := &skills.SourceRef{Parsed: &parsed}
	return installAll(discovered, agents, src, &opts.DestOptions), nil
}

// CheckUpdates checks the global lock for skills with outdated hashes.
func CheckUpdates(ctx context.Context, lock *skills.GlobalLock, opts *InstallOptions) ([]UpdateInfo, error) {
	if opts == nil {
		opts = &InstallOptions{}
	}
	if lock == nil || len(lock.Skills) == 0 {
		return nil, nil
	}
	hp := hashProvider(opts)
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

// UpdateGlobal fetches and reinstalls all global skills that have available updates.
// It modifies the lock in-place; the caller is responsible for writing it to disk.
func UpdateGlobal(ctx context.Context, lock *skills.GlobalLock, agents []skills.AgentType, opts *InstallOptions) ([]skills.InstallResult, error) {
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

	f := fetcher(opts)
	if f == nil {
		return nil, fmt.Errorf("Fetcher is required for UpdateGlobal")
	}

	var allResults []skills.InstallResult
	for _, u := range updates {
		entry := lock.Skills[u.Name]
		source, err := parseSource(opts, entry.SourceURL)
		if err != nil {
			source, err = parseSource(opts, entry.Source)
			if err != nil {
				continue
			}
		}

		localDir, cleanup, err := f.Fetch(ctx, source)
		if err != nil {
			continue
		}

		discovered, err := skills.Discover(localDir, "", &skills.DiscoverOptions{IncludeInternal: true})
		if err != nil {
			cleanup()
			continue
		}

		matched := skills.Filter(discovered, []string{u.Name})
		if len(matched) == 0 {
			cleanup()
			continue
		}

		src := &skills.SourceRef{Parsed: &source, FetchRoot: localDir}
		for _, agent := range agents {
			r := skills.Install(matched[0], agent, src, &opts.DestOptions)
			allResults = append(allResults, r)

			if r.Success {
				newHash, _ := skills.ComputeFolderHash(r.Path)
				updated := entry
				updated.SkillFolderHash = newHash
				lock.SetSkill(u.Name, updated)
			}
		}
		cleanup()
	}

	return allResults, nil
}

// Reconcile compares discovered skills against installed state.
func Reconcile(discovered []*skills.Skill, agentType skills.AgentType, src *skills.SourceRef, dest *skills.DestOptions) ([]SkillStatus, error) {
	if dest == nil {
		dest = &skills.DestOptions{}
	}

	installed, err := skills.ListInstalled(dest)
	if err != nil {
		return nil, err
	}

	installedMap := make(map[string]*skills.InstalledSkill, len(installed))
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
				if src != nil && src.FS != nil {
					st.SourceHash, _ = skills.ComputeFolderHashFS(src.FS, skill.Path)
				} else {
					st.SourceHash, _ = skills.ComputeFolderHash(skill.Path)
				}
			}
			installedHash, _ := skills.ComputeFolderHash(is.Path)
			st.InstalledHash = installedHash

			if st.SourceHash != "" && st.InstalledHash != "" && st.SourceHash != st.InstalledHash {
				st.UpdateAvailable = true
			}
		} else {
			p, err := skills.ResolveInstallPath(skill.Name, agentType, dest)
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
			hash, _ := skills.ComputeFolderHash(is.Path)
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
			p, _ := skills.ResolveInstallPath(statuses[i].Name, agentType, dest)
			statuses[i].InstallPath = p
		}
	}

	return statuses, nil
}

// fetchOrLocal resolves a parsed source to a local directory.
func fetchOrLocal(ctx context.Context, f skills.Fetcher, parsed skills.ParsedSource) (localDir string, cleanup func(), err error) {
	if parsed.Type == skills.SourceLocal {
		return parsed.URL, func() {}, nil
	}
	if f == nil {
		return "", nil, fmt.Errorf("Fetcher is required for remote source %s", parsed.URL)
	}
	return f.Fetch(ctx, parsed)
}

// RestoreFromProjectLock reinstalls all tracked skills from a project lock file.
func RestoreFromProjectLock(ctx context.Context, lock *skills.ProjectLock, agents []skills.AgentType, opts *InstallOptions) ([]skills.InstallResult, error) {
	if opts == nil {
		opts = &InstallOptions{}
	}
	if lock == nil || len(lock.Skills) == 0 {
		return nil, nil
	}

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

	var allResults []skills.InstallResult
	for source, group := range groups {
		parsed, err := parseSource(opts, source)
		if err != nil {
			continue
		}

		localDir, cleanup, err := fetchOrLocal(ctx, fetcher(opts), parsed)
		if err != nil {
			continue
		}

		discovered, err := skills.Discover(localDir, "", &skills.DiscoverOptions{IncludeInternal: true})
		if err != nil {
			cleanup()
			continue
		}

		matched := skills.Filter(discovered, group.skills)
		src := &skills.SourceRef{Parsed: &parsed, FetchRoot: localDir}
		for _, skill := range matched {
			for _, agent := range agents {
				r := skills.Install(skill, agent, src, &opts.DestOptions)
				allResults = append(allResults, r)
			}
		}
		cleanup()
	}

	return allResults, nil
}

// CheckProjectUpdates checks project-scoped skills for updates.
func CheckProjectUpdates(ctx context.Context, lock *skills.ProjectLock, opts *InstallOptions) ([]UpdateInfo, error) {
	if opts == nil {
		opts = &InstallOptions{}
	}
	if lock == nil || len(lock.Skills) == 0 {
		return nil, nil
	}

	type sourceGroup struct {
		skills map[string]string
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

	var updates []UpdateInfo
	for source, group := range groups {
		parsed, err := parseSource(opts, source)
		if err != nil {
			continue
		}

		localDir, cleanup, err := fetchOrLocal(ctx, fetcher(opts), parsed)
		if err != nil {
			continue
		}

		discovered, err := skills.Discover(localDir, "", &skills.DiscoverOptions{IncludeInternal: true})
		if err != nil {
			cleanup()
			continue
		}

		for _, skill := range discovered {
			oldHash, tracked := group.skills[skill.Name]
			if !tracked {
				continue
			}
			newHash, err := skills.ComputeFolderHash(skill.Path)
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
func UpdateProject(ctx context.Context, lock *skills.ProjectLock, agents []skills.AgentType, opts *InstallOptions) ([]skills.InstallResult, error) {
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

	var allResults []skills.InstallResult
	for source, group := range groups {
		parsed, err := parseSource(opts, source)
		if err != nil {
			continue
		}

		localDir, cleanup, err := fetchOrLocal(ctx, fetcher(opts), parsed)
		if err != nil {
			continue
		}

		discovered, err := skills.Discover(localDir, "", &skills.DiscoverOptions{IncludeInternal: true})
		if err != nil {
			cleanup()
			continue
		}

		matched := skills.Filter(discovered, group.names)
		src := &skills.SourceRef{Parsed: &parsed, FetchRoot: localDir}

		for _, skill := range matched {
			for _, agent := range agents {
				r := skills.Install(skill, agent, src, &opts.DestOptions)
				allResults = append(allResults, r)

				if r.Success {
					newHash, _ := skills.ComputeFolderHash(r.Path)
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

// Provider helpers

func providers(opts *InstallOptions) *skills.Providers {
	if opts != nil && opts.Providers != nil {
		return opts.Providers
	}
	return DefaultProviders()
}

func fetcher(opts *InstallOptions) skills.Fetcher {
	return providers(opts).Fetcher
}

func hashProvider(opts *InstallOptions) skills.HashProvider {
	return providers(opts).HashProvider
}

// WriteGlobalLock writes the global lock file to the standard path.
func WriteGlobalLock(lock *skills.GlobalLock) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolving home directory: %w", err)
	}
	return lock.WriteFile(skills.GlobalLockPath(home))
}

// WriteProjectLock writes the project lock file to the given project directory.
func WriteProjectLock(lock *skills.ProjectLock, projectDir string) error {
	return lock.WriteFile(skills.ProjectLockPath(projectDir))
}

func parseSource(opts *InstallOptions, input string) (skills.ParsedSource, error) {
	p := providers(opts)
	if p.SourceParser != nil {
		ps, ok, err := p.SourceParser(input)
		if err != nil {
			return ps, err
		}
		if ok {
			return ps, nil
		}
	}
	return skills.ParseSource(input)
}
