package rules

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLoadClaudeAndCodexRules(t *testing.T) {
	repo := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeRule(t, filepath.Join(repo, ".claude", "rules", "api.md"), `---
paths:
  - "src/api/**/*.{go,ts}"
---
Validate every API input.
`)
	writeRule(t, filepath.Join(repo, ".codex", "signposts", "testing.md"), `---
paths: "tests/**"
---
Use table-driven tests.
`)
	writeRule(t, filepath.Join(home, ".claude", "rules", "personal.md"), "Prefer concise explanations.\n")

	loaded := Load(repo)
	if len(loaded.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", loaded.Warnings)
	}
	if len(loaded.Rules) != 3 {
		t.Fatalf("loaded %d rules, want 3", len(loaded.Rules))
	}

	bySource := map[string]Rule{}
	for _, rule := range loaded.Rules {
		bySource[rule.Source] = rule
	}
	if !bySource[".claude/rules/api.md"].Matches("src/api/users/create.go") {
		t.Fatal("Claude rule did not match a nested Go file")
	}
	if !bySource[".claude/rules/api.md"].Matches("src/api/root.ts") {
		t.Fatal("brace-expanded Claude rule did not match a TypeScript file")
	}
	if bySource[".claude/rules/api.md"].Matches("src/client/root.ts") {
		t.Fatal("Claude rule matched an unrelated path")
	}
	if !bySource[".codex/signposts/testing.md"].Matches("tests/unit/parser_test.go") {
		t.Fatal("Codex rule did not match")
	}
	if bySource["~/.claude/rules/personal.md"].Conditional() {
		t.Fatal("rule without paths should be unconditional")
	}
}

func TestSingleStarDoesNotCrossDirectories(t *testing.T) {
	rule := Rule{Paths: []string{"src/*.go"}}
	if !rule.Matches("src/root.go") {
		t.Fatal("single star did not match a file in the same directory")
	}
	if rule.Matches("src/nested/file.go") {
		t.Fatal("single star crossed a directory boundary")
	}
}

func TestLoadFollowsRuleSymlinksOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation can require additional Windows privileges")
	}
	repo := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)

	shared := filepath.Join(t.TempDir(), "shared")
	writeRule(t, filepath.Join(shared, "shared.md"), "Shared instructions.\n")
	if err := os.MkdirAll(filepath.Join(repo, ".claude", "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(shared, filepath.Join(repo, ".claude", "rules", "shared")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".codex", "signposts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(shared, "shared.md"), filepath.Join(repo, ".codex", "signposts", "duplicate.md")); err != nil {
		t.Fatal(err)
	}

	loaded := Load(repo)
	if len(loaded.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", loaded.Warnings)
	}
	if len(loaded.Rules) != 1 {
		t.Fatalf("loaded %d rules, want one canonical symlink target", len(loaded.Rules))
	}
	if !strings.Contains(loaded.Rules[0].Source, "shared/shared.md") {
		t.Fatalf("unexpected source: %s", loaded.Rules[0].Source)
	}
}

func TestMalformedFrontMatterReturnsWarning(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	writeRule(t, filepath.Join(repo, ".claude", "rules", "bad.md"), "---\npaths:\n  nested: nope\n---\nText\n")

	loaded := Load(repo)
	if len(loaded.Rules) != 0 || len(loaded.Warnings) != 1 {
		t.Fatalf("got %d rules and %d warnings", len(loaded.Rules), len(loaded.Warnings))
	}
}

func writeRule(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
