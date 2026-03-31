package main

import (
	"context"
	"github.com/goccy/go-json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	skills "github.com/ka2n/skills-go"
	"github.com/ka2n/skills-go/provider/git"
	"github.com/ka2n/skills-go/provider/github"
	"github.com/ka2n/skills-go/provider/wellknown"
)

const version = "0.1.0"

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		showBanner()
		return
	}

	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "add", "a":
		cmdAdd(rest)
	case "install", "i":
		// With source arg: same as add. Without: restore from lock.
		if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
			cmdAdd(rest)
		} else {
			cmdInstall(rest)
		}
	case "remove", "rm", "r":
		cmdRemove(rest)
	case "list", "ls":
		cmdList(rest)
	case "init":
		cmdInit(rest)
	case "check":
		cmdCheck(rest)
	case "update", "upgrade":
		cmdUpdate(rest)
	case "--version", "-v":
		fmt.Println(version)
	case "--help", "-h":
		showHelp()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\nRun skills --help for usage.\n", cmd)
		os.Exit(1)
	}
}

func showBanner() {
	fmt.Println()
	fmt.Println("  skills-go — Agent skills manager")
	fmt.Println()
	dim := "\033[2m"
	reset := "\033[0m"
	fmt.Printf("  %s$%s skills add <source>       %sAdd skills%s\n", dim, reset, dim, reset)
	fmt.Printf("  %s$%s skills install             %sRestore from skills-lock.json%s\n", dim, reset, dim, reset)
	fmt.Printf("  %s$%s skills remove              %sRemove skills%s\n", dim, reset, dim, reset)
	fmt.Printf("  %s$%s skills list                %sList installed skills%s\n", dim, reset, dim, reset)
	fmt.Printf("  %s$%s skills init [name]         %sCreate a new skill%s\n", dim, reset, dim, reset)
	fmt.Printf("  %s$%s skills check               %sCheck for updates%s\n", dim, reset, dim, reset)
	fmt.Printf("  %s$%s skills update              %sUpdate all skills%s\n", dim, reset, dim, reset)
	fmt.Println()
}

func showHelp() {
	fmt.Print(`Usage: skills <command> [options]

Manage Skills:
  add <source>         Add skills from a source (GitHub, GitLab, local path, URL)
  install              Restore skills from skills-lock.json (or add with source)
  remove [skills]      Remove installed skills
  list, ls             List installed skills
  init [name]          Create a new SKILL.md template

Updates:
  check                Check for available skill updates (project by default, -g for global)
  update               Update skills to latest versions (project by default, -g for global)

Add Options:
  -g, --global           Install globally (~/  ) instead of project-level
  -a, --agent <agents>   Target specific agents (use '*' for all)
  -s, --skill <skills>   Install specific skills (use '*' for all)
  -y, --yes              Skip confirmation prompts
  --copy                 Copy files instead of symlinking
  --all                  Shorthand for --skill '*' --agent '*' -y
  --full-depth           Search all subdirectories

Remove Options:
  -g, --global           Remove from global scope
  -y, --yes              Skip confirmation
  --all                  Remove all skills

List Options:
  -g, --global           List global skills
  --json                 Output as JSON

Common Options:
  --help, -h             Show help
  --version, -v          Show version

Sources:
  owner/repo             GitHub shorthand
  owner/repo@skill       Specific skill from repo
  https://github.com/... GitHub URL
  https://gitlab.com/... GitLab URL
  ./local/path           Local directory
  https://example.com    Well-known endpoint
`)
}

// --- Add command ---

type addOptions struct {
	global    bool
	agents    []string
	skills    []string
	yes       bool
	copy      bool
	all       bool
	fullDepth bool
	list      bool
}

func parseAddOptions(args []string) (string, addOptions) {
	var opts addOptions
	var source string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-g", "--global":
			opts.global = true
		case "-y", "--yes":
			opts.yes = true
		case "--copy":
			opts.copy = true
		case "--all":
			opts.all = true
		case "--full-depth":
			opts.fullDepth = true
		case "-l", "--list":
			opts.list = true
		case "-a", "--agent":
			for i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				opts.agents = append(opts.agents, args[i])
			}
		case "-s", "--skill":
			for i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				opts.skills = append(opts.skills, args[i])
			}
		default:
			if !strings.HasPrefix(args[i], "-") && source == "" {
				source = args[i]
			}
		}
	}

	if opts.all {
		opts.skills = []string{"*"}
		opts.agents = []string{"*"}
		opts.yes = true
	}

	return source, opts
}

func cmdAdd(args []string) {
	source, opts := parseAddOptions(args)
	if source == "" {
		fmt.Fprintln(os.Stderr, "Usage: skills add <source> [options]")
		os.Exit(1)
	}

	parsed, err := skills.ParseSource(source)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing source: %v\n", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	allAgents := skills.DefaultAgents(homeDir())

	// Determine target agents
	var targetAgents []skills.AgentType
	if len(opts.agents) > 0 && opts.agents[0] == "*" {
		for t := range allAgents {
			targetAgents = append(targetAgents, t)
		}
	} else if len(opts.agents) > 0 {
		targetAgents = opts.agents
	} else {
		// Default: detected agents
		targetAgents = skills.DetectInstalledAgents(allAgents)
		if len(targetAgents) == 0 {
			// Fallback to claude-code
			targetAgents = []skills.AgentType{skills.AgentClaudeCode}
		}
	}

	var mode skills.InstallMode
	if opts.copy {
		mode = skills.InstallCopy
	} else {
		mode = skills.InstallSymlink
	}

	installOpts := &skills.InstallOptions{
		Global: opts.global,
		Mode:   mode,
		Agents: allAgents,
	}

	// Handle well-known sources
	if parsed.Type == skills.SourceWellKnown {
		installWellKnown(ctx, parsed, targetAgents, installOpts, opts)
		return
	}

	// Handle local sources
	if parsed.Type == skills.SourceLocal {
		installLocal(ctx, parsed, targetAgents, installOpts, opts)
		return
	}

	// Git-based sources
	installGit(ctx, parsed, source, targetAgents, installOpts, opts)
}

func installWellKnown(ctx context.Context, parsed skills.ParsedSource, targetAgents []skills.AgentType, installOpts *skills.InstallOptions, opts addOptions) {
	provider := &wellknown.Provider{}
	remoteSkills, err := provider.FetchAllSkills(ctx, parsed.URL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching skills: %v\n", err)
		os.Exit(1)
	}

	if len(remoteSkills) == 0 {
		fmt.Fprintln(os.Stderr, "No skills found at", parsed.URL)
		os.Exit(1)
	}

	if opts.list {
		printRemoteSkills(remoteSkills)
		return
	}

	for _, rs := range remoteSkills {
		if len(opts.skills) > 0 && opts.skills[0] != "*" {
			found := false
			for _, name := range opts.skills {
				if strings.EqualFold(name, rs.Name) || strings.EqualFold(name, rs.InstallName) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		fmt.Printf("Installing %s...\n", rs.Name)
		for _, agent := range targetAgents {
			result := skills.InstallRemoteSkillForAgent(rs, agent, installOpts)
			if result.Success {
				fmt.Printf("  ✓ %s → %s\n", agent, result.Path)
			} else if result.Error != "" {
				fmt.Printf("  ✗ %s: %s\n", agent, result.Error)
			}
		}
	}
}

func installLocal(_ context.Context, parsed skills.ParsedSource, targetAgents []skills.AgentType, installOpts *skills.InstallOptions, opts addOptions) {
	discovered, err := skills.DiscoverSkills(parsed.URL, parsed.Subpath, &skills.DiscoverOptions{
		FullDepth:       opts.fullDepth,
		IncludeInternal: len(opts.skills) > 0,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error discovering skills: %v\n", err)
		os.Exit(1)
	}

	if len(discovered) == 0 {
		fmt.Fprintln(os.Stderr, "No skills found at", parsed.URL)
		os.Exit(1)
	}

	if opts.list {
		printSkills(discovered)
		return
	}

	toInstall := filterByOpts(discovered, opts)
	installSkills(toInstall, targetAgents, installOpts, parsed, "local")
}

func installGit(ctx context.Context, parsed skills.ParsedSource, source string, targetAgents []skills.AgentType, installOpts *skills.InstallOptions, opts addOptions) {
	fmt.Printf("Cloning %s...\n", parsed.URL)
	fetcher := &git.Fetcher{}
	localDir, cleanup, err := fetcher.Fetch(ctx, parsed)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer cleanup()
	installOpts.FetchRoot = localDir

	discovered, err := skills.DiscoverSkills(localDir, parsed.Subpath, &skills.DiscoverOptions{
		FullDepth:       opts.fullDepth,
		IncludeInternal: len(opts.skills) > 0 || parsed.SkillFilter != "",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error discovering skills: %v\n", err)
		os.Exit(1)
	}

	// Apply @skill filter
	if parsed.SkillFilter != "" {
		discovered = skills.FilterSkills(discovered, []string{parsed.SkillFilter})
	}

	if len(discovered) == 0 {
		fmt.Fprintln(os.Stderr, "No skills found in", source)
		os.Exit(1)
	}

	if opts.list {
		printSkills(discovered)
		return
	}

	toInstall := filterByOpts(discovered, opts)
	installSkills(toInstall, targetAgents, installOpts, parsed, source)
}

func filterByOpts(discovered []*skills.Skill, opts addOptions) []*skills.Skill {
	if len(opts.skills) > 0 && opts.skills[0] != "*" {
		return skills.FilterSkills(discovered, opts.skills)
	}
	return discovered
}

func installSkills(toInstall []*skills.Skill, targetAgents []skills.AgentType, installOpts *skills.InstallOptions, parsed skills.ParsedSource, source string) {
	cwd, _ := os.Getwd()

	// Set source on install options for lock entry population
	installOpts.Source = &parsed

	var allResults []skills.InstallResult

	for _, skill := range toInstall {
		fmt.Printf("Installing %s...\n", skill.Name)
		for _, agent := range targetAgents {
			result := skills.InstallSkillForAgent(skill, agent, installOpts)
			allResults = append(allResults, result)
			if result.Success {
				fmt.Printf("  ✓ %s → %s\n", agent, result.Path)
			} else if result.Error != "" {
				fmt.Printf("  ✗ %s: %s\n", agent, result.Error)
			}
		}
	}

	// Write lock files using high-level API
	if err := skills.WriteLockEntries(allResults, homeDir(), cwd); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write lock: %v\n", err)
	}

	fmt.Println()
	fmt.Printf("✓ Installed %d skill(s)\n", len(toInstall))
}

// --- Install command (no source = restore from lock) ---

func cmdInstall(_ []string) {
	cwd, _ := os.Getwd()
	lockPath := skills.ProjectLockPath(cwd)
	lock, err := skills.ReadProjectLock(lockPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading %s: %v\n", lockPath, err)
		os.Exit(1)
	}

	if len(lock.Skills) == 0 {
		fmt.Println("No skills found in skills-lock.json")
		fmt.Println("Add skills with: skills add <source>")
		return
	}

	fmt.Printf("Restoring %d skill(s) from skills-lock.json...\n", len(lock.Skills))

	allAgents := skills.DefaultAgents(homeDir())
	targetAgents := skills.DetectInstalledAgents(allAgents)
	if len(targetAgents) == 0 {
		targetAgents = skills.UniversalAgents(allAgents)
	}

	installOpts := &skills.InstallOptions{
		Cwd:    cwd,
		Agents: allAgents,
		Providers: &skills.Providers{Fetcher: &git.Fetcher{}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	results, err := skills.RestoreFromProjectLock(ctx, lock, targetAgents, installOpts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	successCount := 0
	for _, r := range results {
		if r.Success {
			successCount++
			fmt.Printf("  ✓ %s → %s\n", r.SkillName, r.Path)
		} else if r.Error != "" {
			fmt.Printf("  ✗ %s: %s\n", r.SkillName, r.Error)
		}
	}

	// Update lock with new hashes
	if err := skills.WriteLockEntries(results, homeDir(), cwd); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write lock: %v\n", err)
	}

	fmt.Printf("\n✓ Restored %d skill(s)\n", successCount)
}

func printSkills(discovered []*skills.Skill) {
	for _, s := range discovered {
		fmt.Printf("  %s — %s\n", s.Name, s.Description)
	}
}

func printRemoteSkills(discovered []*skills.RemoteSkill) {
	for _, s := range discovered {
		fmt.Printf("  %s — %s\n", s.Name, s.Description)
	}
}

// --- Remove command ---

func cmdRemove(args []string) {
	var global, yes, all bool
	var skillNames []string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-g", "--global":
			global = true
		case "-y", "--yes":
			yes = true
		case "--all":
			all = true
		default:
			if !strings.HasPrefix(args[i], "-") {
				skillNames = append(skillNames, args[i])
			}
		}
	}
	_ = yes

	allAgents := skills.DefaultAgents(homeDir())
	cwd, _ := os.Getwd()
	installOpts := &skills.InstallOptions{Global: global, Cwd: cwd, Agents: allAgents}

	installed, err := skills.ListInstalledSkills(installOpts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing skills: %v\n", err)
		os.Exit(1)
	}

	if len(installed) == 0 {
		fmt.Println("No skills installed.")
		return
	}

	var toRemove []string
	if all {
		for _, s := range installed {
			toRemove = append(toRemove, s.Name)
		}
	} else if len(skillNames) > 0 {
		for _, s := range installed {
			for _, name := range skillNames {
				if strings.EqualFold(s.Name, name) {
					toRemove = append(toRemove, s.Name)
				}
			}
		}
	} else {
		// List skills for manual removal
		fmt.Println("Installed skills:")
		for _, s := range installed {
			fmt.Printf("  %s (%s)\n", s.Name, s.Scope)
		}
		fmt.Println("\nSpecify skill names to remove: skills remove <name> [name...]")
		return
	}

	var agentTypes []skills.AgentType
	for t := range allAgents {
		agentTypes = append(agentTypes, t)
	}

	for _, name := range toRemove {
		if err := skills.RemoveSkill(name, agentTypes, installOpts); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to remove %s: %v\n", name, err)
		} else {
			fmt.Printf("✓ Removed %s\n", name)
		}
	}

	// Update lock files
	if global {
		lock, _ := skills.ReadGlobalLock(skills.GlobalLockPath(homeDir()))
		for _, name := range toRemove {
			lock.RemoveSkill(name)
		}
		lock.Write(skills.GlobalLockPath(homeDir()))
	} else {
		lock, _ := skills.ReadProjectLock(skills.ProjectLockPath(cwd))
		for _, name := range toRemove {
			lock.RemoveSkill(name)
		}
		lock.Write(skills.ProjectLockPath(cwd))
	}
}

// --- List command ---

func cmdList(args []string) {
	var global, jsonOutput bool
	for _, arg := range args {
		switch arg {
		case "-g", "--global":
			global = true
		case "--json":
			jsonOutput = true
		}
	}

	allAgents := skills.DefaultAgents(homeDir())
	cwd, _ := os.Getwd()
	installOpts := &skills.InstallOptions{Global: global, Cwd: cwd, Agents: allAgents}

	installed, err := skills.ListInstalledSkills(installOpts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	sort.Slice(installed, func(i, j int) bool {
		return installed[i].Name < installed[j].Name
	})

	if jsonOutput {
		type jsonSkill struct {
			Name   string   `json:"name"`
			Path   string   `json:"path"`
			Scope  string   `json:"scope"`
			Agents []string `json:"agents"`
		}
		var out []jsonSkill
		for _, s := range installed {
			agentNames := make([]string, len(s.Agents))
			for i, a := range s.Agents {
				if cfg, ok := allAgents[a]; ok {
					agentNames[i] = cfg.DisplayName
				} else {
					agentNames[i] = a
				}
			}
			out = append(out, jsonSkill{Name: s.Name, Path: s.CanonicalPath, Scope: s.Scope, Agents: agentNames})
		}
		data, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(data))
		return
	}

	if len(installed) == 0 {
		scope := "project"
		if global {
			scope = "global"
		}
		fmt.Printf("No %s skills found.\n", scope)
		return
	}

	home := homeDir()
	for _, s := range installed {
		path := s.CanonicalPath
		if strings.HasPrefix(path, home) {
			path = "~" + path[len(home):]
		} else if strings.HasPrefix(path, cwd) {
			path = "." + path[len(cwd):]
		}

		agentNames := make([]string, len(s.Agents))
		for i, a := range s.Agents {
			if cfg, ok := allAgents[a]; ok {
				agentNames[i] = cfg.DisplayName
			} else {
				agentNames[i] = a
			}
		}

		fmt.Printf("  %s  \033[2m%s\033[0m\n", s.Name, path)
		if len(agentNames) > 0 {
			fmt.Printf("    Agents: %s\n", strings.Join(agentNames, ", "))
		}
	}
}

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}


// --- Init command ---

func cmdInit(args []string) {
	cwd, _ := os.Getwd()
	name := ""
	if len(args) > 0 {
		name = args[0]
	}

	dir := cwd
	if name != "" {
		dir = cwd // InitSkill handles subdirectory creation
	}

	path, err := skills.InitSkill(dir, name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	rel, _ := filepath.Rel(cwd, path)
	fmt.Printf("✓ Created %s\n", rel)
	fmt.Println("\nNext steps:")
	fmt.Println("  1. Edit the SKILL.md to define your skill instructions")
	fmt.Println("  2. Update the name and description in the frontmatter")
}

// --- Check command ---

func cmdCheck(args []string) {
	var global bool
	for _, arg := range args {
		if arg == "-g" || arg == "--global" {
			global = true
		}
	}

	ctx := context.Background()

	if global {
		lock, err := skills.ReadGlobalLock(skills.GlobalLockPath(homeDir()))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading global lock: %v\n", err)
			os.Exit(1)
		}
		if len(lock.Skills) == 0 {
			fmt.Println("No global skills tracked.")
			return
		}
		fmt.Println("Checking global skills for updates...")
		updates, err := skills.CheckUpdates(ctx, lock, &skills.InstallOptions{
			Providers: &skills.Providers{HashProvider: &github.HashProvider{Token: github.AutoToken()}},
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if len(updates) == 0 {
			fmt.Println("✓ All global skills are up to date")
		} else {
			fmt.Printf("%d update(s) available:\n", len(updates))
			for _, u := range updates {
				fmt.Printf("  ↑ %s\n", u.Name)
			}
			fmt.Println("\nRun skills update -g to update")
		}
		return
	}

	// Project scope (default)
	cwd, _ := os.Getwd()
	lock, err := skills.ReadProjectLock(skills.ProjectLockPath(cwd))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading project lock: %v\n", err)
		os.Exit(1)
	}
	if len(lock.Skills) == 0 {
		fmt.Println("No project skills tracked in skills-lock.json")
		return
	}
	fmt.Println("Checking project skills for updates...")
	updates, err := skills.CheckProjectUpdates(ctx, lock, &skills.InstallOptions{
		Cwd:       cwd,
		Providers: &skills.Providers{Fetcher: &git.Fetcher{}},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if len(updates) == 0 {
		fmt.Println("✓ All project skills are up to date")
	} else {
		fmt.Printf("%d update(s) available:\n", len(updates))
		for _, u := range updates {
			fmt.Printf("  ↑ %s\n", u.Name)
		}
		fmt.Println("\nRun skills update to update")
	}
}

// --- Update command ---

func cmdUpdate(args []string) {
	var global bool
	for _, arg := range args {
		if arg == "-g" || arg == "--global" {
			global = true
		}
	}

	ctx := context.Background()
	allAgents := skills.DefaultAgents(homeDir())
	targetAgents := skills.DetectInstalledAgents(allAgents)
	if len(targetAgents) == 0 {
		targetAgents = []skills.AgentType{skills.AgentClaudeCode}
	}

	fmt.Println("Checking for updates...")

	if global {
		lock, err := skills.ReadGlobalLock(skills.GlobalLockPath(homeDir()))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading global lock: %v\n", err)
			os.Exit(1)
		}
		if len(lock.Skills) == 0 {
			fmt.Println("No global skills tracked.")
			return
		}
		results, err := skills.UpdateAll(ctx, lock, targetAgents, &skills.InstallOptions{
			Global: true,
			Agents: allAgents,
			Providers: &skills.Providers{
				Fetcher:      &git.Fetcher{},
				HashProvider: &github.HashProvider{Token: github.AutoToken()},
			},
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if len(results) == 0 {
			fmt.Println("✓ All global skills are up to date")
			return
		}
		successCount := 0
		for _, r := range results {
			if r.Success {
				successCount++
				fmt.Printf("  ✓ %s\n", r.SkillName)
			}
		}
		if err := lock.Write(skills.GlobalLockPath(homeDir())); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not write lock: %v\n", err)
		}
		fmt.Printf("\n✓ Updated %d skill(s)\n", successCount)
		return
	}

	// Project scope (default)
	cwd, _ := os.Getwd()
	lock, err := skills.ReadProjectLock(skills.ProjectLockPath(cwd))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading project lock: %v\n", err)
		os.Exit(1)
	}
	if len(lock.Skills) == 0 {
		fmt.Println("No project skills tracked in skills-lock.json")
		return
	}
	results, err := skills.UpdateProject(ctx, lock, targetAgents, &skills.InstallOptions{
		Cwd:       cwd,
		Agents:    allAgents,
		Providers: &skills.Providers{Fetcher: &git.Fetcher{}},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if len(results) == 0 {
		fmt.Println("✓ All project skills are up to date")
		return
	}
	successCount := 0
	for _, r := range results {
		if r.Success {
			successCount++
			fmt.Printf("  ✓ %s\n", r.SkillName)
		}
	}
	if err := lock.Write(skills.ProjectLockPath(cwd)); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write lock: %v\n", err)
	}
	fmt.Printf("\n✓ Updated %d skill(s)\n", successCount)
}
