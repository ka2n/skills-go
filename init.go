package skills

import (
	"fmt"
	"os"
	"path/filepath"
)

// InitSkill creates a SKILL.md template in the given directory.
func InitSkill(dir string, name string) (string, error) {
	if name == "" {
		name = filepath.Base(dir)
	}

	skillDir := dir
	if name != filepath.Base(dir) {
		skillDir = filepath.Join(dir, name)
	}
	skillFile := filepath.Join(skillDir, "SKILL.md")

	if _, err := os.Stat(skillFile); err == nil {
		return "", fmt.Errorf("skill already exists at %s", skillFile)
	}

	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return "", err
	}

	content := fmt.Sprintf(`---
name: %s
description: A brief description of what this skill does
---

# %s

Instructions for the agent to follow when this skill is activated.

## When to use

Describe when this skill should be used.

## Instructions

1. First step
2. Second step
3. Additional steps as needed
`, name, name)

	if err := os.WriteFile(skillFile, []byte(content), 0o644); err != nil {
		return "", err
	}

	return skillFile, nil
}
