package skills

import (
	"github.com/goccy/go-json"
	"os"
	"path/filepath"
	"strings"
)

type pluginManifestEntry struct {
	Source interface{} `json:"source"` // string or object
	Skills []string    `json:"skills"`
	Name   string      `json:"name"`
}

type marketplaceManifest struct {
	Metadata struct {
		PluginRoot string `json:"pluginRoot"`
	} `json:"metadata"`
	Plugins []pluginManifestEntry `json:"plugins"`
}

type pluginManifest struct {
	Skills []string `json:"skills"`
	Name   string   `json:"name"`
}

func isValidRelativePath(p string) bool {
	return strings.HasPrefix(p, "./")
}

func isContainedIn(targetPath, basePath string) bool {
	base, _ := filepath.Abs(basePath)
	target, _ := filepath.Abs(targetPath)
	return strings.HasPrefix(target, base+string(filepath.Separator)) || target == base
}

func getPluginSkillPaths(basePath string) []string {
	var dirs []string

	addPaths := func(pluginBase string, skills []string) {
		if !isContainedIn(pluginBase, basePath) {
			return
		}
		if len(skills) > 0 {
			for _, sp := range skills {
				if !isValidRelativePath(sp) {
					continue
				}
				skillDir := filepath.Dir(filepath.Join(pluginBase, sp))
				if isContainedIn(skillDir, basePath) {
					dirs = append(dirs, skillDir)
				}
			}
		}
		dirs = append(dirs, filepath.Join(pluginBase, "skills"))
	}

	// marketplace.json
	if data, err := os.ReadFile(filepath.Join(basePath, ".claude-plugin/marketplace.json")); err == nil {
		var m marketplaceManifest
		if json.Unmarshal(data, &m) == nil {
			root := m.Metadata.PluginRoot
			if root == "" || isValidRelativePath(root) {
				for _, plugin := range m.Plugins {
					src, ok := plugin.Source.(string)
					if !ok && plugin.Source != nil {
						continue
					}
					if src != "" && !isValidRelativePath(src) {
						continue
					}
					pluginBase := filepath.Join(basePath, root, src)
					addPaths(pluginBase, plugin.Skills)
				}
			}
		}
	}

	// plugin.json
	if data, err := os.ReadFile(filepath.Join(basePath, ".claude-plugin/plugin.json")); err == nil {
		var m pluginManifest
		if json.Unmarshal(data, &m) == nil {
			addPaths(basePath, m.Skills)
		}
	}

	return dirs
}

// GetPluginGroupings returns a map of absolute skill directory path to plugin name.
func GetPluginGroupings(basePath string) map[string]string {
	groupings := map[string]string{}

	if data, err := os.ReadFile(filepath.Join(basePath, ".claude-plugin/marketplace.json")); err == nil {
		var m marketplaceManifest
		if json.Unmarshal(data, &m) == nil {
			root := m.Metadata.PluginRoot
			if root == "" || isValidRelativePath(root) {
				for _, plugin := range m.Plugins {
					if plugin.Name == "" {
						continue
					}
					src, ok := plugin.Source.(string)
					if !ok && plugin.Source != nil {
						continue
					}
					if src != "" && !isValidRelativePath(src) {
						continue
					}
					pluginBase := filepath.Join(basePath, root, src)
					if !isContainedIn(pluginBase, basePath) {
						continue
					}
					for _, sp := range plugin.Skills {
						if !isValidRelativePath(sp) {
							continue
						}
						skillDir := filepath.Join(pluginBase, sp)
						if isContainedIn(skillDir, basePath) {
							abs, _ := filepath.Abs(skillDir)
							groupings[abs] = plugin.Name
						}
					}
				}
			}
		}
	}

	if data, err := os.ReadFile(filepath.Join(basePath, ".claude-plugin/plugin.json")); err == nil {
		var m pluginManifest
		if json.Unmarshal(data, &m) == nil && m.Name != "" {
			for _, sp := range m.Skills {
				if !isValidRelativePath(sp) {
					continue
				}
				skillDir := filepath.Join(basePath, sp)
				if isContainedIn(skillDir, basePath) {
					abs, _ := filepath.Abs(skillDir)
					groupings[abs] = m.Name
				}
			}
		}
	}

	return groupings
}
