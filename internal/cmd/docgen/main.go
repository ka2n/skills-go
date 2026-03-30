// Command docgen regenerates marker sections in README.md from Go source.
//
// Usage (via go generate):
//
//	//go:generate go run ./internal/cmd/docgen
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	skills "github.com/ka2n/skills-go"
)

var (
	beginRe = regexp.MustCompile(`<!--\s*BEGIN:(\w+)\s*-->`)
	endRe   = regexp.MustCompile(`<!--\s*END:(\w+)\s*-->`)
)

func main() {
	path := "README.md"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}

	sections := map[string]string{
		"agents": agentTable(),
	}

	if err := renderInPlace(path, sections); err != nil {
		fmt.Fprintf(os.Stderr, "docgen: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "docgen: updated %s\n", path)
}

func renderInPlace(path string, sections map[string]string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 256*1024), 256*1024)

	var inSection string
	for scanner.Scan() {
		line := scanner.Text()

		if m := beginRe.FindStringSubmatch(line); m != nil {
			name := m[1]
			buf.WriteString(line + "\n")
			if content, ok := sections[name]; ok {
				buf.WriteString(content)
				if !strings.HasSuffix(content, "\n") {
					buf.WriteByte('\n')
				}
			}
			inSection = name
			continue
		}

		if m := endRe.FindStringSubmatch(line); m != nil {
			if m[1] == inSection {
				inSection = ""
			}
			buf.WriteString(line + "\n")
			continue
		}

		if inSection != "" {
			continue // skip old generated content
		}

		buf.WriteString(line + "\n")
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if inSection != "" {
		return fmt.Errorf("unclosed marker: BEGIN:%s", inSection)
	}

	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func agentTable() string {
	agents := skills.DefaultAgents("/home/user") // dummy home; only SkillsDir matters

	type row struct {
		displayName string
		projectDir  string
		universal   bool
	}

	var rows []row
	for _, cfg := range agents {
		if cfg.Name == "universal" || (!cfg.ShowInUniversalList && cfg.Name != "universal") {
			continue
		}
		rows = append(rows, row{
			displayName: cfg.DisplayName,
			projectDir:  cfg.SkillsDir,
			universal:   cfg.SkillsDir == ".agents/skills",
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].displayName < rows[j].displayName
	})

	var b strings.Builder
	b.WriteString("| Agent | Project Dir | Universal |\n")
	b.WriteString("|-------|------------|:---------:|\n")
	for _, r := range rows {
		uni := ""
		if r.universal {
			uni = "yes"
		}
		fmt.Fprintf(&b, "| %s | `%s` | %s |\n", r.displayName, r.projectDir, uni)
	}
	return b.String()
}
