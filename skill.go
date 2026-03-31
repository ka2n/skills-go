// Package skills provides a library for managing agent skills -
// reusable instruction sets that extend AI coding agent capabilities.
//
// Skills are defined as SKILL.md files with YAML frontmatter containing
// name and description fields. They can be installed from various sources
// (GitHub, GitLab, well-known endpoints, local paths, or custom providers)
// into multiple agent directories.
package skills

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/goccy/go-yaml"
)

// Skill represents a parsed SKILL.md file.
type Skill struct {
	// Name is the unique identifier for the skill (lowercase, hyphens allowed, 1-64 chars).
	Name string `yaml:"name" json:"name"`
	// Description is a brief explanation of the skill's functionality.
	Description string `yaml:"description" json:"description"`
	// Metadata contains optional fields.
	Metadata SkillMetadata `yaml:"metadata,omitempty" json:"metadata,omitempty"`
	// RawFrontmatter preserves all frontmatter fields (including unknown ones like
	// allowed-tools, disable-model-invocation) for round-trip fidelity in Marshal().
	RawFrontmatter map[string]any `yaml:"-" json:"-"`
	// Body is the markdown content after the frontmatter.
	Body string `yaml:"-" json:"-"`
	// RawContent is the original SKILL.md content for hashing.
	RawContent string `yaml:"-" json:"-"`
	// Path is the directory containing the SKILL.md file (absolute on disk).
	Path string `yaml:"-" json:"path,omitempty"`
	// PluginName is the name of the plugin this skill belongs to (if any).
	PluginName string `yaml:"-" json:"pluginName,omitempty"`
}

// SkillMetadata contains optional metadata fields for a skill.
type SkillMetadata struct {
	Internal bool `yaml:"internal,omitempty" json:"internal,omitempty"`
}

// RemoteSkill represents a skill fetched from a remote host provider.
type RemoteSkill struct {
	Name             string            `json:"name"`
	Description      string            `json:"description"`
	Content          string            `json:"content"`
	InstallName      string            `json:"installName"`
	SourceURL        string            `json:"sourceUrl"`
	ProviderID       string            `json:"providerId"`
	SourceIdentifier string            `json:"sourceIdentifier"`
	Metadata         map[string]any    `json:"metadata,omitempty"`
	Files            map[string]string `json:"files,omitempty"`
}

// ParseSkill parses a SKILL.md file from a reader.
func ParseSkill(r io.Reader) (*Skill, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading skill: %w", err)
	}
	return ParseSkillBytes(data)
}

// ParseSkillBytes parses a SKILL.md file from bytes.
func ParseSkillBytes(data []byte) (*Skill, error) {
	frontmatter, body, err := splitFrontmatter(data)
	if err != nil {
		return nil, fmt.Errorf("parsing frontmatter: %w", err)
	}

	var s Skill
	if err := yaml.Unmarshal(frontmatter, &s); err != nil {
		return nil, fmt.Errorf("parsing skill frontmatter YAML: %w", err)
	}

	// Capture all frontmatter fields (including unknown ones) for round-trip fidelity.
	var raw map[string]any
	if err := yaml.Unmarshal(frontmatter, &raw); err == nil {
		s.RawFrontmatter = raw
	}

	if s.Name == "" {
		return nil, fmt.Errorf("skill name is required")
	}
	if s.Description == "" {
		return nil, fmt.Errorf("skill description is required")
	}

	s.Body = string(body)
	s.RawContent = string(data)
	return &s, nil
}

// ParseSkillFile parses a SKILL.md file from the filesystem.
func ParseSkillFile(path string) (*Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseSkillBytes(data)
}

// Marshal serializes a Skill back to SKILL.md format (YAML frontmatter + markdown body).
// Unknown frontmatter fields from the original parse are preserved.
func (s *Skill) Marshal() ([]byte, error) {
	// Start with raw frontmatter to preserve unknown fields.
	merged := make(map[string]any)
	for k, v := range s.RawFrontmatter {
		merged[k] = v
	}
	// Overwrite known fields with current values.
	merged["name"] = s.Name
	merged["description"] = s.Description
	if m := omitEmptyMetadata(&s.Metadata); m != nil {
		merged["metadata"] = m
	} else {
		delete(merged, "metadata")
	}

	fm, err := yaml.Marshal(merged)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	buf.WriteString("---\n")
	buf.Write(fm)
	buf.WriteString("---\n")
	if s.Body != "" {
		buf.WriteString(s.Body)
	}
	return buf.Bytes(), nil
}

func omitEmptyMetadata(m *SkillMetadata) *SkillMetadata {
	if m == nil || *m == (SkillMetadata{}) {
		return nil
	}
	return m
}

// splitFrontmatter splits YAML frontmatter from markdown body.
func splitFrontmatter(data []byte) (frontmatter, body []byte, err error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))

	if !scanner.Scan() {
		return nil, nil, fmt.Errorf("empty file")
	}
	if strings.TrimSpace(scanner.Text()) != "---" {
		return nil, nil, fmt.Errorf("file must start with ---")
	}

	var fmBuf bytes.Buffer
	closed := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "---" {
			closed = true
			break
		}
		fmBuf.WriteString(line)
		fmBuf.WriteByte('\n')
	}
	if !closed {
		return nil, nil, fmt.Errorf("unclosed frontmatter (missing closing ---)")
	}

	var bodyBuf bytes.Buffer
	for scanner.Scan() {
		bodyBuf.WriteString(scanner.Text())
		bodyBuf.WriteByte('\n')
	}

	return fmBuf.Bytes(), bodyBuf.Bytes(), scanner.Err()
}
