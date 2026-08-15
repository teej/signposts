package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teej/signposts/internal/state"
)

func TestSessionAndPostToolUseLifecycle(t *testing.T) {
	repo := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTestFile(t, filepath.Join(repo, ".git", "keep"), "")
	writeTestFile(t, filepath.Join(repo, "src", "sync.go"), "package src\n")
	writeTestFile(t, filepath.Join(repo, ".claude", "rules", "always.md"), "Always preserve behavior.\n")
	writeTestFile(t, filepath.Join(repo, ".codex", "signposts", "sync.md"), `---
paths:
  - "src/**"
---
Preserve transaction IDs.
`)

	handler := Handler{Store: state.New(t.TempDir())}
	start := Event{SessionID: "session", CWD: repo, HookEventName: "SessionStart", Source: "startup"}
	startOutput, err := handler.Handle(start)
	if err != nil {
		t.Fatal(err)
	}
	assertContextContains(t, startOutput, "Always preserve behavior.")
	if repeated, err := handler.Handle(start); err != nil || repeated != nil {
		t.Fatalf("repeated start = %#v, %v", repeated, err)
	}

	post := Event{
		SessionID:     "session",
		TurnID:        "turn",
		CWD:           repo,
		HookEventName: "PostToolUse",
		ToolName:      "Bash",
		ToolInput:     json.RawMessage(`{"command":"sed -n '1p' src/sync.go"}`),
		ToolResponse:  json.RawMessage(`{"output":"package src"}`),
	}
	postOutput, err := handler.Handle(post)
	if err != nil {
		t.Fatal(err)
	}
	assertContextContains(t, postOutput, "Preserve transaction IDs.")
	assertContextContains(t, postOutput, "preceding tool read `src/sync.go`")
	if repeated, err := handler.Handle(post); err != nil || repeated != nil {
		t.Fatalf("repeated post = %#v, %v", repeated, err)
	}

	compact := Event{SessionID: "session", CWD: repo, HookEventName: "SessionStart", Source: "compact"}
	compactOutput, err := handler.Handle(compact)
	if err != nil {
		t.Fatal(err)
	}
	assertContextContains(t, compactOutput, "Always preserve behavior.")
	assertContextContains(t, compactOutput, "Preserve transaction IDs.")
	assertContextContains(t, compactOutput, "restored after context compaction")
}

func TestPostToolUseIgnoresBuildOutput(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	writeTestFile(t, filepath.Join(repo, ".git", "keep"), "")
	writeTestFile(t, filepath.Join(repo, "src", "sync.go"), "package src\n")
	writeTestFile(t, filepath.Join(repo, ".codex", "signposts", "sync.md"), "---\npaths: [\"src/**\"]\n---\nRule text.\n")

	handler := Handler{Store: state.New(t.TempDir())}
	post := Event{
		SessionID:     "session",
		CWD:           repo,
		HookEventName: "PostToolUse",
		ToolName:      "Bash",
		ToolInput:     json.RawMessage(`{"command":"pnpm test"}`),
		ToolResponse:  json.RawMessage(`{"output":"src/sync.go:12: test failed"}`),
	}
	output, err := handler.Handle(post)
	if err != nil {
		t.Fatal(err)
	}
	if output != nil {
		t.Fatalf("build output unexpectedly loaded a rule: %#v", output)
	}
}

func TestClearResetsDeduplication(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	writeTestFile(t, filepath.Join(repo, ".git", "keep"), "")
	writeTestFile(t, filepath.Join(repo, ".claude", "rules", "always.md"), "Always loaded.\n")
	handler := Handler{Store: state.New(t.TempDir())}

	start := Event{SessionID: "session", CWD: repo, HookEventName: "SessionStart", Source: "startup"}
	if output, err := handler.Handle(start); err != nil || output == nil {
		t.Fatalf("initial start = %#v, %v", output, err)
	}
	clear := Event{SessionID: "session", CWD: repo, HookEventName: "SessionStart", Source: "clear"}
	if output, err := handler.Handle(clear); err != nil || output == nil {
		t.Fatalf("clear start = %#v, %v", output, err)
	}
}

func TestFindRepoRootFallsBackToStartingDirectory(t *testing.T) {
	directory := t.TempDir()
	root, err := FindRepoRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	if root != directory {
		t.Fatalf("got %q, want %q", root, directory)
	}
}

func assertContextContains(t *testing.T, output *Output, expected string) {
	t.Helper()
	if output == nil || output.HookSpecificOutput == nil {
		t.Fatalf("missing hook context; wanted %q", expected)
	}
	if !strings.Contains(output.HookSpecificOutput.AdditionalContext, expected) {
		t.Fatalf("context %q does not contain %q", output.HookSpecificOutput.AdditionalContext, expected)
	}
}

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
