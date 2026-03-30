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
	"github.com/ka2n/skills-go/provider/local"
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
	case "add", "a", "install", "i":
		cmdAdd(rest)
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
  remove [skills]      Remove installed skills
  list, ls             List installed skills
  init [name]          Create a new SKILL.md template

Updates:
  check                Check for available skill updates
  update               Update all skills to latest versions

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

	// Lock files
	var globalLock *skills.GlobalLock
	var projectLock *skills.ProjectLock
	if installOpts.Global {
		globalLock, _ = skills.ReadGlobalLock(skills.GlobalLockPath(homeDir()))
	} else {
		projectLock, _ = skills.ReadProjectLock(skills.ProjectLockPath(cwd))
	}

	ownerRepo := skills.GetOwnerRepo(parsed)
	sourceType := string(parsed.Type)

	for _, skill := range toInstall {
		fmt.Printf("Installing %s...\n", skill.Name)
		for _, agent := range targetAgents {
			result := skills.InstallSkillForAgent(skill, agent, installOpts)
			if result.Success {
				fmt.Printf("  ✓ %s → %s\n", agent, result.Path)
			} else if result.Error != "" {
				fmt.Printf("  ✗ %s: %s\n", agent, result.Error)
			}
		}

		// Update lock files
		if installOpts.Global && globalLock != nil {
			lockSource := ownerRepo
			if lockSource == "" {
				lockSource = source
			}
			globalLock.SetSkill(skill.Name, skills.GlobalLockEntry{
				Source:     lockSource,
				SourceType: sourceType,
				SourceURL:  parsed.URL,
				SkillPath:  filepath.Join(skill.Path, "SKILL.md"),
			})
		}
		if !installOpts.Global && projectLock != nil {
			lockSource := ownerRepo
			if lockSource == "" {
				lockSource = source
			}
			hash := ""
			if skill.Path != "" {
				hash, _ = local.ComputeFolderHash(skill.Path)
			}
			projectLock.SetSkill(skill.Name, skills.ProjectLockEntry{
				Source:       lockSource,
				SourceType:   sourceType,
				ComputedHash: hash,
			})
		}
	}

	// Write lock files
	if installOpts.Global && globalLock != nil {
		if err := globalLock.Write(skills.GlobalLockPath(homeDir())); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not write global lock: %v\n", err)
		}
	}
	if !installOpts.Global && projectLock != nil {
		if err := projectLock.Write(skills.ProjectLockPath(cwd)); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not write project lock: %v\n", err)
		}
	}

	fmt.Println()
	fmt.Printf("✓ Installed %d skill(s)\n", len(toInstall))
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

	// Update lock file
	if global {
		lock, _ := skills.ReadGlobalLock(skills.GlobalLockPath(homeDir()))
		for _, name := range toRemove {
			lock.RemoveSkill(name)
		}
		lock.Write(skills.GlobalLockPath(homeDir()))
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

func cmdCheck(_ []string) {
	lock, err := skills.ReadGlobalLock(skills.GlobalLockPath(homeDir()))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading lock: %v\n", err)
		os.Exit(1)
	}

	if len(lock.Skills) == 0 {
		fmt.Println("No skills tracked in lock file.")
		return
	}

	fmt.Println("Checking for updates...")
	ctx := context.Background()
	hp := &github.HashProvider{Token: github.AutoToken()}

	var updates []string
	for name, entry := range lock.Skills {
		if entry.SkillFolderHash == "" || entry.SkillPath == "" {
			continue
		}
		ownerRepo := entry.Source
		hash, err := hp.FetchFolderHash(ctx, ownerRepo, entry.SkillPath)
		if err != nil {
			continue
		}
		if hash != entry.SkillFolderHash {
			updates = append(updates, name)
		}
	}

	if len(updates) == 0 {
		fmt.Println("✓ All skills are up to date")
	} else {
		fmt.Printf("%d update(s) available:\n", len(updates))
		for _, name := range updates {
			fmt.Printf("  ↑ %s\n", name)
		}
		fmt.Println("\nRun skills update to update all skills")
	}
}

// --- Update command ---

func cmdUpdate(_ []string) {
	lock, err := skills.ReadGlobalLock(skills.GlobalLockPath(homeDir()))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading lock: %v\n", err)
		os.Exit(1)
	}

	if len(lock.Skills) == 0 {
		fmt.Println("No skills tracked in lock file.")
		return
	}

	fmt.Println("Checking for updates...")
	ctx := context.Background()
	hp := &github.HashProvider{Token: github.AutoToken()}

	type updateEntry struct {
		name  string
		entry skills.GlobalLockEntry
	}
	var updates []updateEntry

	for name, entry := range lock.Skills {
		if entry.SkillFolderHash == "" || entry.SkillPath == "" {
			continue
		}
		hash, err := hp.FetchFolderHash(ctx, entry.Source, entry.SkillPath)
		if err != nil {
			continue
		}
		if hash != entry.SkillFolderHash {
			updates = append(updates, updateEntry{name: name, entry: entry})
		}
	}

	if len(updates) == 0 {
		fmt.Println("✓ All skills are up to date")
		return
	}

	fmt.Printf("Found %d update(s)\n", len(updates))
	allAgents := skills.DefaultAgents(homeDir())
	targetAgents := skills.DetectInstalledAgents(allAgents)
	if len(targetAgents) == 0 {
		targetAgents = []skills.AgentType{skills.AgentClaudeCode}
	}

	successCount := 0
	for _, u := range updates {
		fmt.Printf("Updating %s...\n", u.name)
		// Re-install from source
		cmdAddArgs := []string{u.entry.SourceURL, "-g", "-y", "--skill", u.name}
		cmdAdd(cmdAddArgs)
		successCount++
	}

	fmt.Printf("\n✓ Updated %d skill(s)\n", successCount)
}
