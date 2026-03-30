package skills

import (
	"os"
	"path/filepath"
)

// AgentType is a string identifier for a supported agent.
type AgentType = string

// Known agent types.
const (
	AgentAmp           AgentType = "amp"
	AgentAntigravity   AgentType = "antigravity"
	AgentAugment       AgentType = "augment"
	AgentClaudeCode    AgentType = "claude-code"
	AgentOpenClaw      AgentType = "openclaw"
	AgentCline         AgentType = "cline"
	AgentCodeBuddy     AgentType = "codebuddy"
	AgentCodex         AgentType = "codex"
	AgentCommandCode   AgentType = "command-code"
	AgentContinue      AgentType = "continue"
	AgentCortex        AgentType = "cortex"
	AgentCrush         AgentType = "crush"
	AgentCursor        AgentType = "cursor"
	AgentDeepAgents    AgentType = "deepagents"
	AgentDroid         AgentType = "droid"
	AgentFirebender    AgentType = "firebender"
	AgentGeminiCLI     AgentType = "gemini-cli"
	AgentGitHubCopilot AgentType = "github-copilot"
	AgentGoose         AgentType = "goose"
	AgentJunie         AgentType = "junie"
	AgentIFlowCLI      AgentType = "iflow-cli"
	AgentKilo          AgentType = "kilo"
	AgentKimiCLI       AgentType = "kimi-cli"
	AgentKiroCLI       AgentType = "kiro-cli"
	AgentKode          AgentType = "kode"
	AgentMCPJam        AgentType = "mcpjam"
	AgentMistralVibe   AgentType = "mistral-vibe"
	AgentMux           AgentType = "mux"
	AgentNeovate       AgentType = "neovate"
	AgentOpenCode      AgentType = "opencode"
	AgentOpenHands     AgentType = "openhands"
	AgentPi            AgentType = "pi"
	AgentQoder         AgentType = "qoder"
	AgentQwenCode      AgentType = "qwen-code"
	AgentReplit        AgentType = "replit"
	AgentRoo           AgentType = "roo"
	AgentTrae          AgentType = "trae"
	AgentTraeCN        AgentType = "trae-cn"
	AgentWarp          AgentType = "warp"
	AgentWindsurf      AgentType = "windsurf"
	AgentZencoder      AgentType = "zencoder"
	AgentPochi         AgentType = "pochi"
	AgentAdal          AgentType = "adal"
	AgentUniversal     AgentType = "universal"
)

// AgentConfig describes where an agent stores skills.
type AgentConfig struct {
	Name            string
	DisplayName     string
	SkillsDir       string // relative to project root
	GlobalSkillsDir string // absolute path, empty if global not supported
	ShowInUniversalList bool
	DetectInstalled func() bool
}

// UserHomeDir returns the current user's home directory.
// This is the canonical way to get the home directory in the library.
func UserHomeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

func resolveXDGConfigHome(home string) string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v
	}
	return filepath.Join(home, ".config")
}

func resolveCodexHome(home string) string {
	if v := os.Getenv("CODEX_HOME"); v != "" {
		return v
	}
	return filepath.Join(home, ".codex")
}

func resolveClaudeHome(home string) string {
	if v := os.Getenv("CLAUDE_CONFIG_DIR"); v != "" {
		return v
	}
	return filepath.Join(home, ".claude")
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func resolveOpenClawGlobalSkillsDir(home string) string {
	for _, name := range []string{".openclaw", ".clawdbot", ".moltbot"} {
		if pathExists(filepath.Join(home, name)) {
			return filepath.Join(home, name, "skills")
		}
	}
	return filepath.Join(home, ".openclaw", "skills")
}

// DefaultAgents returns all known agent configurations.
// homeDir is the user's home directory (e.g. from os.UserHomeDir()).
func DefaultAgents(homeDir string) map[AgentType]AgentConfig {
	configHome := resolveXDGConfigHome(homeDir)
	codex := resolveCodexHome(homeDir)
	claude := resolveClaudeHome(homeDir)

	m := map[AgentType]AgentConfig{
		AgentAmp:           {Name: "amp", DisplayName: "Amp", SkillsDir: ".agents/skills", GlobalSkillsDir: filepath.Join(configHome, "agents/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(configHome, "amp")) }},
		AgentAntigravity:   {Name: "antigravity", DisplayName: "Antigravity", SkillsDir: ".agents/skills", GlobalSkillsDir: filepath.Join(homeDir, ".gemini/antigravity/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".gemini/antigravity")) }},
		AgentAugment:       {Name: "augment", DisplayName: "Augment", SkillsDir: ".augment/skills", GlobalSkillsDir: filepath.Join(homeDir, ".augment/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".augment")) }},
		AgentClaudeCode:    {Name: "claude-code", DisplayName: "Claude Code", SkillsDir: ".claude/skills", GlobalSkillsDir: filepath.Join(claude, "skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(claude) }},
		AgentOpenClaw:      {Name: "openclaw", DisplayName: "OpenClaw", SkillsDir: "skills", GlobalSkillsDir: resolveOpenClawGlobalSkillsDir(homeDir), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".openclaw")) || pathExists(filepath.Join(homeDir, ".clawdbot")) || pathExists(filepath.Join(homeDir, ".moltbot")) }},
		AgentCline:         {Name: "cline", DisplayName: "Cline", SkillsDir: ".agents/skills", GlobalSkillsDir: filepath.Join(homeDir, ".agents", "skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".cline")) }},
		AgentCodeBuddy:     {Name: "codebuddy", DisplayName: "CodeBuddy", SkillsDir: ".codebuddy/skills", GlobalSkillsDir: filepath.Join(homeDir, ".codebuddy/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".codebuddy")) }},
		AgentCodex:         {Name: "codex", DisplayName: "Codex", SkillsDir: ".agents/skills", GlobalSkillsDir: filepath.Join(codex, "skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(codex) || pathExists("/etc/codex") }},
		AgentCommandCode:   {Name: "command-code", DisplayName: "Command Code", SkillsDir: ".commandcode/skills", GlobalSkillsDir: filepath.Join(homeDir, ".commandcode/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".commandcode")) }},
		AgentContinue:      {Name: "continue", DisplayName: "Continue", SkillsDir: ".continue/skills", GlobalSkillsDir: filepath.Join(homeDir, ".continue/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".continue")) }},
		AgentCortex:        {Name: "cortex", DisplayName: "Cortex Code", SkillsDir: ".cortex/skills", GlobalSkillsDir: filepath.Join(homeDir, ".snowflake/cortex/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".snowflake/cortex")) }},
		AgentCrush:         {Name: "crush", DisplayName: "Crush", SkillsDir: ".crush/skills", GlobalSkillsDir: filepath.Join(homeDir, ".config/crush/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".config/crush")) }},
		AgentCursor:        {Name: "cursor", DisplayName: "Cursor", SkillsDir: ".agents/skills", GlobalSkillsDir: filepath.Join(homeDir, ".cursor/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".cursor")) }},
		AgentDeepAgents:    {Name: "deepagents", DisplayName: "Deep Agents", SkillsDir: ".agents/skills", GlobalSkillsDir: filepath.Join(homeDir, ".deepagents/agent/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".deepagents")) }},
		AgentDroid:         {Name: "droid", DisplayName: "Droid", SkillsDir: ".factory/skills", GlobalSkillsDir: filepath.Join(homeDir, ".factory/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".factory")) }},
		AgentFirebender:    {Name: "firebender", DisplayName: "Firebender", SkillsDir: ".agents/skills", GlobalSkillsDir: filepath.Join(homeDir, ".firebender/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".firebender")) }},
		AgentGeminiCLI:     {Name: "gemini-cli", DisplayName: "Gemini CLI", SkillsDir: ".agents/skills", GlobalSkillsDir: filepath.Join(homeDir, ".gemini/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".gemini")) }},
		AgentGitHubCopilot: {Name: "github-copilot", DisplayName: "GitHub Copilot", SkillsDir: ".agents/skills", GlobalSkillsDir: filepath.Join(homeDir, ".copilot/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".copilot")) }},
		AgentGoose:         {Name: "goose", DisplayName: "Goose", SkillsDir: ".goose/skills", GlobalSkillsDir: filepath.Join(configHome, "goose/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(configHome, "goose")) }},
		AgentJunie:         {Name: "junie", DisplayName: "Junie", SkillsDir: ".junie/skills", GlobalSkillsDir: filepath.Join(homeDir, ".junie/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".junie")) }},
		AgentIFlowCLI:      {Name: "iflow-cli", DisplayName: "iFlow CLI", SkillsDir: ".iflow/skills", GlobalSkillsDir: filepath.Join(homeDir, ".iflow/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".iflow")) }},
		AgentKilo:          {Name: "kilo", DisplayName: "Kilo Code", SkillsDir: ".kilocode/skills", GlobalSkillsDir: filepath.Join(homeDir, ".kilocode/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".kilocode")) }},
		AgentKimiCLI:       {Name: "kimi-cli", DisplayName: "Kimi Code CLI", SkillsDir: ".agents/skills", GlobalSkillsDir: filepath.Join(homeDir, ".config/agents/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".kimi")) }},
		AgentKiroCLI:       {Name: "kiro-cli", DisplayName: "Kiro CLI", SkillsDir: ".kiro/skills", GlobalSkillsDir: filepath.Join(homeDir, ".kiro/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".kiro")) }},
		AgentKode:          {Name: "kode", DisplayName: "Kode", SkillsDir: ".kode/skills", GlobalSkillsDir: filepath.Join(homeDir, ".kode/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".kode")) }},
		AgentMCPJam:        {Name: "mcpjam", DisplayName: "MCPJam", SkillsDir: ".mcpjam/skills", GlobalSkillsDir: filepath.Join(homeDir, ".mcpjam/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".mcpjam")) }},
		AgentMistralVibe:   {Name: "mistral-vibe", DisplayName: "Mistral Vibe", SkillsDir: ".vibe/skills", GlobalSkillsDir: filepath.Join(homeDir, ".vibe/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".vibe")) }},
		AgentMux:           {Name: "mux", DisplayName: "Mux", SkillsDir: ".mux/skills", GlobalSkillsDir: filepath.Join(homeDir, ".mux/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".mux")) }},
		AgentNeovate:       {Name: "neovate", DisplayName: "Neovate", SkillsDir: ".neovate/skills", GlobalSkillsDir: filepath.Join(homeDir, ".neovate/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".neovate")) }},
		AgentOpenCode:      {Name: "opencode", DisplayName: "OpenCode", SkillsDir: ".agents/skills", GlobalSkillsDir: filepath.Join(configHome, "opencode/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(configHome, "opencode")) }},
		AgentOpenHands:     {Name: "openhands", DisplayName: "OpenHands", SkillsDir: ".openhands/skills", GlobalSkillsDir: filepath.Join(homeDir, ".openhands/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".openhands")) }},
		AgentPi:            {Name: "pi", DisplayName: "Pi", SkillsDir: ".pi/skills", GlobalSkillsDir: filepath.Join(homeDir, ".pi/agent/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".pi/agent")) }},
		AgentQoder:         {Name: "qoder", DisplayName: "Qoder", SkillsDir: ".qoder/skills", GlobalSkillsDir: filepath.Join(homeDir, ".qoder/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".qoder")) }},
		AgentQwenCode:      {Name: "qwen-code", DisplayName: "Qwen Code", SkillsDir: ".qwen/skills", GlobalSkillsDir: filepath.Join(homeDir, ".qwen/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".qwen")) }},
		AgentReplit:        {Name: "replit", DisplayName: "Replit", SkillsDir: ".agents/skills", GlobalSkillsDir: filepath.Join(configHome, "agents/skills"), ShowInUniversalList: false, DetectInstalled: func() bool { return false }},
		AgentRoo:           {Name: "roo", DisplayName: "Roo Code", SkillsDir: ".roo/skills", GlobalSkillsDir: filepath.Join(homeDir, ".roo/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".roo")) }},
		AgentTrae:          {Name: "trae", DisplayName: "Trae", SkillsDir: ".trae/skills", GlobalSkillsDir: filepath.Join(homeDir, ".trae/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".trae")) }},
		AgentTraeCN:        {Name: "trae-cn", DisplayName: "Trae CN", SkillsDir: ".trae/skills", GlobalSkillsDir: filepath.Join(homeDir, ".trae-cn/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".trae-cn")) }},
		AgentWarp:          {Name: "warp", DisplayName: "Warp", SkillsDir: ".agents/skills", GlobalSkillsDir: filepath.Join(homeDir, ".agents/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".warp")) }},
		AgentWindsurf:      {Name: "windsurf", DisplayName: "Windsurf", SkillsDir: ".windsurf/skills", GlobalSkillsDir: filepath.Join(homeDir, ".codeium/windsurf/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".codeium/windsurf")) }},
		AgentZencoder:      {Name: "zencoder", DisplayName: "Zencoder", SkillsDir: ".zencoder/skills", GlobalSkillsDir: filepath.Join(homeDir, ".zencoder/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".zencoder")) }},
		AgentPochi:         {Name: "pochi", DisplayName: "Pochi", SkillsDir: ".pochi/skills", GlobalSkillsDir: filepath.Join(homeDir, ".pochi/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".pochi")) }},
		AgentAdal:          {Name: "adal", DisplayName: "AdaL", SkillsDir: ".adal/skills", GlobalSkillsDir: filepath.Join(homeDir, ".adal/skills"), ShowInUniversalList: true, DetectInstalled: func() bool { return pathExists(filepath.Join(homeDir, ".adal")) }},
		AgentUniversal:     {Name: "universal", DisplayName: "Universal", SkillsDir: ".agents/skills", GlobalSkillsDir: filepath.Join(configHome, "agents/skills"), ShowInUniversalList: false, DetectInstalled: func() bool { return false }},
	}
	return m
}

// IsUniversalAgent returns true if the agent uses the universal .agents/skills directory.
func IsUniversalAgent(agents map[AgentType]AgentConfig, agentType AgentType) bool {
	cfg, ok := agents[agentType]
	if !ok {
		return false
	}
	return cfg.SkillsDir == ".agents/skills"
}

// UniversalAgents returns agent types that use the universal .agents/skills directory.
func UniversalAgents(agents map[AgentType]AgentConfig) []AgentType {
	var result []AgentType
	for t, cfg := range agents {
		if cfg.SkillsDir == ".agents/skills" && cfg.ShowInUniversalList {
			result = append(result, t)
		}
	}
	return result
}

// NonUniversalAgents returns agent types that use agent-specific directories.
func NonUniversalAgents(agents map[AgentType]AgentConfig) []AgentType {
	var result []AgentType
	for t, cfg := range agents {
		if cfg.SkillsDir != ".agents/skills" {
			result = append(result, t)
		}
	}
	return result
}

// DetectInstalledAgents returns agent types that appear to be installed.
func DetectInstalledAgents(agents map[AgentType]AgentConfig) []AgentType {
	var result []AgentType
	for t, cfg := range agents {
		if cfg.DetectInstalled != nil && cfg.DetectInstalled() {
			result = append(result, t)
		}
	}
	return result
}
