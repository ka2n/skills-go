# skills-go

Go library and CLI for managing [agent skills](https://github.com/vercel-labs/skills) — reusable instruction sets that extend AI coding agent capabilities.

Format-compatible with [vercel-labs/skills](https://github.com/vercel-labs/skills) (`npx skills`). Designed to be embedded into your own tooling for managing private skill repositories.

## Use cases

- Embed skill install/discovery into your own CLI or internal platform
- Manage private skill registries (GitHub, GitLab, local, or custom backends like S3, Google Drive, Confluence, etc.)
- Automate skill deployment across teams and agents

## Install

### As a library

```go
go get github.com/ka2n/skills-go
```

### As a CLI

```sh
go install github.com/ka2n/skills-go/cmd/skills@latest
```

## Library usage

### High-level API (`sk` package)

The `sk` package provides one-step workflows with built-in providers.
When `opts` is nil, [DefaultProviders](https://pkg.go.dev/github.com/ka2n/skills-go/sk#DefaultProviders) (git fetcher, GitHub hash provider, well-known endpoint support) is used automatically.

```go
import (
    skills "github.com/ka2n/skills-go"
    "github.com/ka2n/skills-go/sk"
)

// Install from GitHub
results, _ := sk.Install(ctx,
    skills.SourceFrom("vercel-labs/agent-skills"),
    []skills.AgentType{skills.AgentClaudeCode},
    nil,
)

// Install from a local directory
results, _ = sk.Install(ctx,
    skills.SourceFromLocal("./my-skills"),
    []skills.AgentType{skills.AgentClaudeCode},
    nil,
)

// Install from an embedded filesystem
//go:embed skills
var skillsFS embed.FS

results, _ = sk.Install(ctx,
    skills.SourceFromFS(skillsFS),
    []skills.AgentType{skills.AgentClaudeCode},
    nil,
)

// Install and update project lock file
results, _ = sk.Install(ctx,
    skills.SourceFrom("owner/repo"),
    []skills.AgentType{skills.AgentClaudeCode},
    nil,
)
lock, _ := sk.ProjectLock(".")
lock.ApplyResults(results)
sk.WriteProjectLock(lock, ".")

// Restore from skills-lock.json (like npm install)
lock, _ := sk.ProjectLock(".")
sk.RestoreFromProjectLock(ctx, lock, agents, nil)

// Check for updates and apply them
globalLock, _ := sk.GlobalLock()
updates, _ := sk.CheckUpdates(ctx, globalLock, nil)
if len(updates) > 0 {
    sk.UpdateGlobal(ctx, globalLock, agents, nil)
    sk.WriteGlobalLock(globalLock)
}
```

### Low-level API (`skills` package)

The `skills` package provides composable primitives for custom workflows
(e.g. custom storage backends for lock files, non-standard directory layouts).

```go
import (
    skills "github.com/ka2n/skills-go"
    "github.com/ka2n/skills-go/provider/git"
)

// 1. Parse source — "owner/repo" is resolved to a GitHub git URL
source, _ := skills.ParseSource("your-org/private-skills")

// 2. Clone — Fetcher downloads the repo to a temp directory
fetcher := &git.Fetcher{}
dir, cleanup, _ := fetcher.Fetch(ctx, source)
defer cleanup()

// 3. Discover — find all SKILL.md files in the cloned repo
found, _ := skills.Discover(dir, "", nil)

// 4. Install — copy/symlink into the agent's skills directory
home, _ := os.UserHomeDir()
dest := &skills.DestOptions{
    Cwd:     ".",
    HomeDir: home,
    Agents:  skills.DefaultAgents(home),
}
for _, s := range found {
    result := skills.Install(s, skills.AgentClaudeCode, nil, dest)
    fmt.Println(result.Path)
}

// 5. Update lock — write to any io.Writer
lock, _ := skills.ReadGlobalLockFile(skills.GlobalLockPath(home))
lock.ApplyResults(results)
lock.WriteTo(myCustomWriter) // S3, database, etc.
```

### Custom providers

```go
opts := &sk.InstallOptions{
    Providers: &skills.Providers{
        Fetcher:      skills.MultiFetcher(&azdo.Fetcher{}, &wellknown.Fetcher{}, &git.Fetcher{}),
        HashProvider: skills.MultiHashProvider(&azdo.HashProvider{}, &github.HashProvider{}),
        SourceParser: func(input string) (skills.ParsedSource, bool, error) {
            if strings.HasPrefix(input, "azdo:") {
                return skills.ParsedSource{
                    Type: "azdo",
                    URL:  "https://dev.azure.com/...",
                }, true, nil
            }
            return skills.ParsedSource{}, false, nil
        },
    },
}
results, _ := sk.Install(ctx, skills.SourceFrom("azdo:my-project/repo"), agents, opts)
```

## Example CLI usage

```sh
skills add owner/repo                     # GitHub shorthand
skills add owner/repo@skill-name          # specific skill
skills add https://github.com/owner/repo  # full URL
skills add ./local/path                   # local directory
skills add https://example.com            # well-known endpoint

skills install                            # restore from skills-lock.json
skills install owner/repo                 # same as add

skills list                               # list project skills
skills list -g                            # list global skills
skills list --json                        # JSON output

skills remove skill-name                  # remove by name

skills init my-skill                      # create SKILL.md template

skills check                              # check project skills for updates
skills check -g                           # check global skills for updates
skills update                             # update project skills
skills update -g                          # update global skills
```

### Add options

```
-g, --global         Install globally (~/) instead of project-level
-a, --agent <name>   Target specific agents (use '*' for all)
-s, --skill <name>   Install specific skills (use '*' for all)
-y, --yes            Skip confirmation prompts
--copy               Copy files instead of symlinking
--all                Shorthand for --skill '*' --agent '*' -y
--full-depth         Search all subdirectories
```

## Source formats

| Format | Example |
|--------|---------|
| GitHub shorthand | `owner/repo` |
| GitHub with skill | `owner/repo@skill-name` |
| GitHub with subpath | `owner/repo/path/to/skills` |
| GitHub URL | `https://github.com/owner/repo` |
| GitHub tree URL | `https://github.com/owner/repo/tree/main/skills/x` |
| GitLab URL | `https://gitlab.com/group/repo` |
| GitLab shorthand | `gitlab:group/repo` |
| Local path | `./path` or `/absolute/path` |
| Well-known endpoint | `https://example.com` (RFC 8615) |
| Git URL | `git@github.com:owner/repo.git` |

## SKILL.md format

```markdown
---
name: my-skill
description: Brief description of what this skill does
---

# My Skill

Instructions for the agent.
```

Fully compatible with the [vercel-labs/skills](https://github.com/vercel-labs/skills) format.

## Directory structure

Skills are installed using the same layout as `npx skills`:

```
project/
├── .agents/skills/my-skill/SKILL.md   # canonical copy
├── .claude/skills/my-skill            # symlink → ../../.agents/skills/my-skill
├── .cursor/skills/my-skill            # symlink (if selected)
└── skills-lock.json                   # project lock (version 1)
```

Global installs go to `~/.agents/skills/` with a lock at `~/.agents/.skill-lock.json` (version 3).

## Supported agents

<!-- BEGIN:agents -->
| Agent | Project Dir | Universal |
|-------|------------|:---------:|
| AdaL | `.adal/skills` |  |
| Amp | `.agents/skills` | yes |
| Antigravity | `.agents/skills` | yes |
| Augment | `.augment/skills` |  |
| Claude Code | `.claude/skills` |  |
| Cline | `.agents/skills` | yes |
| CodeBuddy | `.codebuddy/skills` |  |
| Codex | `.agents/skills` | yes |
| Command Code | `.commandcode/skills` |  |
| Continue | `.continue/skills` |  |
| Cortex Code | `.cortex/skills` |  |
| Crush | `.crush/skills` |  |
| Cursor | `.agents/skills` | yes |
| Deep Agents | `.agents/skills` | yes |
| Droid | `.factory/skills` |  |
| Firebender | `.agents/skills` | yes |
| Gemini CLI | `.agents/skills` | yes |
| GitHub Copilot | `.agents/skills` | yes |
| Goose | `.goose/skills` |  |
| Junie | `.junie/skills` |  |
| Kilo Code | `.kilocode/skills` |  |
| Kimi Code CLI | `.agents/skills` | yes |
| Kiro CLI | `.kiro/skills` |  |
| Kode | `.kode/skills` |  |
| MCPJam | `.mcpjam/skills` |  |
| Mistral Vibe | `.vibe/skills` |  |
| Mux | `.mux/skills` |  |
| Neovate | `.neovate/skills` |  |
| OpenClaw | `skills` |  |
| OpenCode | `.agents/skills` | yes |
| OpenHands | `.openhands/skills` |  |
| Pi | `.pi/skills` |  |
| Pochi | `.pochi/skills` |  |
| Qoder | `.qoder/skills` |  |
| Qwen Code | `.qwen/skills` |  |
| Roo Code | `.roo/skills` |  |
| Trae | `.trae/skills` |  |
| Trae CN | `.trae/skills` |  |
| Warp | `.agents/skills` | yes |
| Windsurf | `.windsurf/skills` |  |
| Zencoder | `.zencoder/skills` |  |
| iFlow CLI | `.iflow/skills` |  |
<!-- END:agents -->

Agents marked "Universal" share the `.agents/skills/` directory; others get symlinks from their own directory.

## Architecture

```
github.com/ka2n/skills-go
├── skill.go           # SKILL.md parsing, Skill type
├── source.go          # Source type + source string parsing
├── agent.go           # Agent definitions (45+ agents)
├── discover.go        # Skill discovery (priority dirs + recursive)
├── installer.go       # Install (symlink/copy), list, uninstall
├── hash.go            # Folder/content hashing (SHA-256)
├── lock.go            # Lock file read/write + ApplyResults
├── provider.go        # Fetcher/HashProvider/Providers + Multi* combinators
├── plugin.go          # .claude-plugin manifest support
├── init.go            # SKILL.md template generation
├── sk/                # High-level orchestration (Install, Update, Restore, Reconcile)
├── provider/
│   ├── git/           # Fetcher — git clone (exec)
│   ├── go-git/        # Fetcher — go-git v6 (pure Go, no git CLI)
│   ├── github/        # HashProvider — GitHub Trees API + token resolution
│   ├── local/         # Fetcher — local path + folder hashing
│   └── wellknown/     # Fetcher — RFC 8615 well-known endpoint
└── cmd/
    └── skills/        # CLI
```

### Provider abstraction

Git is not a hard dependency. Core interfaces allow any backend:

```go
type Providers struct {
    Fetcher      Fetcher        // retrieves skills from remote sources
    HashProvider HashProvider   // checks remote hashes for update detection
    SourceParser SourceParser   // custom source format parser
}

// Combine multiple implementations with Multi* constructors:
// skills.MultiFetcher(&wellknown.Fetcher{}, &git.Fetcher{})
// skills.MultiHashProvider(hp1, hp2)
// skills.MultiSourceParser(parser1, parser2)

type Fetcher interface {
    Fetch(ctx context.Context, source ParsedSource) (localDir string, cleanup func(), err error)
}

type HashProvider interface {
    FetchFolderHash(ctx context.Context, ownerRepo, skillPath string) (string, error)
}

type SourceParser func(input string) (ParsedSource, bool, error)
```

Built-in providers:

| Package | Interface | Description | Requires |
|---------|-----------|-------------|----------|
| `provider/git` | `Fetcher` | `git clone` via exec | git CLI |
| `provider/go-git` | `Fetcher` | Pure Go clone (go-git v6) | nothing |
| `provider/local` | `Fetcher` | Local filesystem path | nothing |
| `provider/github` | `HashProvider` | GitHub Trees API (update check) | nothing |
| `provider/wellknown` | `Fetcher` | RFC 8615 well-known endpoint | nothing |

## Lock file format

Fully compatible with `npx skills`.

**Project** (`skills-lock.json`):
```json
{
  "version": 1,
  "skills": {
    "skill-name": {
      "source": "owner/repo",
      "sourceType": "github",
      "computedHash": "sha256..."
    }
  }
}
```

**Global** (`~/.agents/.skill-lock.json`):
```json
{
  "version": 3,
  "skills": {
    "skill-name": {
      "source": "owner/repo",
      "sourceType": "github",
      "sourceUrl": "https://github.com/owner/repo.git",
      "skillPath": "skills/skill-name/SKILL.md",
      "skillFolderHash": "gitTreeSHA",
      "installedAt": "2024-01-15T10:30:00Z",
      "updatedAt": "2024-01-15T10:30:00Z"
    }
  }
}
```

## Compatibility

Cross-validated against `npx skills` — identical SKILL.md content, file trees, and lockfile structure. See `crossval_test.go`.

## License

MIT
