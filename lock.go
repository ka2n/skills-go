package skills

import (
	"github.com/goccy/go-json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// GlobalLockVersion is the current version of the global lock file format.
const GlobalLockVersion = 3

// ProjectLockVersion is the current version of the project lock file format.
const ProjectLockVersion = 1

// GlobalLock represents the global skill lock file (~/.agents/.skill-lock.json).
type GlobalLock struct {
	Version            int                        `json:"version"`
	Skills             map[string]GlobalLockEntry  `json:"skills"`
	Dismissed          map[string]bool             `json:"dismissed,omitempty"`
	LastSelectedAgents []string                    `json:"lastSelectedAgents,omitempty"`
}

// GlobalLockEntry represents a single skill entry in the global lock file.
type GlobalLockEntry struct {
	Source          string `json:"source"`
	SourceType      string `json:"sourceType"`
	SourceURL       string `json:"sourceUrl"`
	SkillPath       string `json:"skillPath"`
	SkillFolderHash string `json:"skillFolderHash,omitempty"`
	InstalledAt     string `json:"installedAt"`
	UpdatedAt       string `json:"updatedAt"`
	PluginName      string `json:"pluginName,omitempty"`
}

// ProjectLock represents the project-scoped lock file (skills-lock.json).
type ProjectLock struct {
	Version int                         `json:"version"`
	Skills  map[string]ProjectLockEntry `json:"skills"`
}

// ProjectLockEntry represents a single skill entry in the project lock file.
type ProjectLockEntry struct {
	Source       string `json:"source"`
	SourceType   string `json:"sourceType"`
	ComputedHash string `json:"computedHash,omitempty"`
}

// NewGlobalLock creates a new empty global lock.
func NewGlobalLock() *GlobalLock {
	return &GlobalLock{
		Version: GlobalLockVersion,
		Skills:  make(map[string]GlobalLockEntry),
	}
}

// NewProjectLock creates a new empty project lock.
func NewProjectLock() *ProjectLock {
	return &ProjectLock{
		Version: ProjectLockVersion,
		Skills:  make(map[string]ProjectLockEntry),
	}
}

// ReadGlobalLock reads the global lock file from the given path.
func ReadGlobalLock(path string) (*GlobalLock, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewGlobalLock(), nil
		}
		return nil, fmt.Errorf("reading global lock: %w", err)
	}
	var lock GlobalLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("parsing global lock: %w", err)
	}
	if lock.Skills == nil {
		lock.Skills = make(map[string]GlobalLockEntry)
	}
	return &lock, nil
}

// Write writes the global lock file to the given path.
func (l *GlobalLock) Write(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating lock directory: %w", err)
	}
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling global lock: %w", err)
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

// SetSkill adds or updates a skill entry in the global lock.
func (l *GlobalLock) SetSkill(name string, entry GlobalLockEntry) {
	now := time.Now().UTC().Format(time.RFC3339)
	if entry.InstalledAt == "" {
		if existing, ok := l.Skills[name]; ok {
			entry.InstalledAt = existing.InstalledAt
		} else {
			entry.InstalledAt = now
		}
	}
	if entry.UpdatedAt == "" {
		entry.UpdatedAt = now
	}
	l.Skills[name] = entry
}

// RemoveSkill removes a skill entry from the global lock.
func (l *GlobalLock) RemoveSkill(name string) {
	delete(l.Skills, name)
}

// ReadProjectLock reads the project lock file from the given path.
func ReadProjectLock(path string) (*ProjectLock, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewProjectLock(), nil
		}
		return nil, fmt.Errorf("reading project lock: %w", err)
	}
	var lock ProjectLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("parsing project lock: %w", err)
	}
	if lock.Skills == nil {
		lock.Skills = make(map[string]ProjectLockEntry)
	}
	return &lock, nil
}

// Write writes the project lock file to the given path.
func (l *ProjectLock) Write(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating lock directory: %w", err)
	}
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling project lock: %w", err)
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

// SetSkill adds or updates a skill entry in the project lock.
func (l *ProjectLock) SetSkill(name string, entry ProjectLockEntry) {
	l.Skills[name] = entry
}

// RemoveSkill removes a skill entry from the project lock.
func (l *ProjectLock) RemoveSkill(name string) {
	delete(l.Skills, name)
}

// GlobalLockPath returns the default path for the global lock file.
// It checks XDG_STATE_HOME first, then falls back to ~/.agents/.skill-lock.json.
// homeDir is the user's home directory.
func GlobalLockPath(homeDir string) string {
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "skills", ".skill-lock.json")
	}
	return filepath.Join(homeDir, ".agents", ".skill-lock.json")
}

// ProjectLockPath returns the path for the project lock file in the given directory.
func ProjectLockPath(projectDir string) string {
	return filepath.Join(projectDir, "skills-lock.json")
}
