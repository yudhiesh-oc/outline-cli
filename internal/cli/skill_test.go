package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yudhiesh-oc/outline-cli/internal/skill"
)

// Every supported harness receives the embedded skill in its own skills directory.
func TestSkillInstallWritesHarnessTargets(t *testing.T) {
	targets := map[string]string{
		"claude-code": ".claude/skills/outline/SKILL.md",
		"codex":       ".codex/skills/outline/SKILL.md",
		"cursor":      ".cursor/skills/outline/SKILL.md",
		"gemini":      ".gemini/skills/outline/SKILL.md",
		"pi":          ".pi/agent/skills/outline/SKILL.md",
	}
	for harness, relative := range targets {
		t.Run(harness, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			code, out, errOut := runCLI(t, "skill", "install", harness)
			if code != 0 || errOut != "" {
				t.Fatalf("install %s: exit=%d stderr=%s", harness, code, errOut)
			}
			var receipt struct{ Harness, Path string }
			if err := json.Unmarshal([]byte(out), &receipt); err != nil {
				t.Fatalf("receipt %q: %v", out, err)
			}
			want := filepath.Join(home, filepath.FromSlash(relative))
			if receipt.Harness != harness || receipt.Path != want {
				t.Fatalf("receipt = %+v, want harness %s at %s", receipt, harness, want)
			}
			content, err := os.ReadFile(want)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(content, skill.Outline) {
				t.Fatalf("installed %d bytes, want the embedded skill", len(content))
			}
			if !strings.HasPrefix(string(content), "---\nname: outline\n") {
				t.Fatalf("skill is missing its name frontmatter: %.40q", content)
			}
		})
	}
}

// An existing skill file is preserved unless --force is passed.
func TestSkillInstallOverwritesOnlyWithForce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".cursor", "skills", "outline", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("hand-edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := runCLI(t, "skill", "install", "cursor")
	if code != 1 || out != "" || !strings.Contains(errOut, "--force") {
		t.Fatalf("existing skill: exit=%d out=%q stderr=%q", code, out, errOut)
	}
	if content, err := os.ReadFile(path); err != nil || string(content) != "hand-edited" {
		t.Fatalf("existing skill was modified: %q err=%v", content, err)
	}
	if code, _, errOut := runCLI(t, "skill", "install", "cursor", "--force"); code != 0 {
		t.Fatalf("force install: exit=%d stderr=%s", code, errOut)
	}
	if content, err := os.ReadFile(path); err != nil || !bytes.Equal(content, skill.Outline) {
		t.Fatalf("force install did not replace the skill: %v", err)
	}
}

// Unknown harnesses are usage errors and write nothing.
func TestSkillInstallRejectsUnknownHarness(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	code, out, errOut := runCLI(t, "skill", "install", "vscode")
	if code != 2 || out != "" || !strings.Contains(errOut, "claude-code") {
		t.Fatalf("unknown harness: exit=%d out=%q stderr=%q", code, out, errOut)
	}
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 0 {
		t.Fatalf("unknown harness wrote into the home directory: %v %v", entries, err)
	}
}
