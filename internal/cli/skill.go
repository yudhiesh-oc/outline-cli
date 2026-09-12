package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	urfave "github.com/urfave/cli/v3"

	"github.com/yudhiesh-oc/outline-cli/internal/skill"
)

// skillName is the skill directory name every harness discovers.
const skillName = "outline"

// skillHarnesses maps each supported harness to its user-level skills
// directory, relative to the home directory.
var skillHarnesses = map[string]string{
	"claude-code": filepath.Join(".claude", "skills"),
	"codex":       filepath.Join(".codex", "skills"),
	"cursor":      filepath.Join(".cursor", "skills"),
	"gemini":      filepath.Join(".gemini", "skills"),
	"pi":          filepath.Join(".pi", "agent", "skills"),
}

// harnessNames lists the supported harnesses in a stable order.
func harnessNames() []string {
	names := make([]string, 0, len(skillHarnesses))
	for name := range skillHarnesses {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func cmdSkillInstall(_ context.Context, c *urfave.Command) error {
	harness := c.Args().First()
	dir, ok := skillHarnesses[harness]
	if !ok {
		return usageError("unknown harness %q; supported: %s", harness, strings.Join(harnessNames(), ", "))
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot locate the home directory: %w", err)
	}
	path := filepath.Join(home, dir, skillName, "SKILL.md")
	if _, err := os.Stat(path); err == nil && !c.Bool("force") {
		return fmt.Errorf("%s already exists; pass --force to overwrite it", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, skill.Outline, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	receipt, err := json.Marshal(map[string]any{"harness": harness, "path": path, "bytes": len(skill.Outline)})
	if err != nil {
		return err
	}
	return emit(c.Root().Writer, commandSkill, true, receipt)
}
