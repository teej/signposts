package detect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDetectsIntentionalFileReads(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "src", "a.go"), "package src\n")
	writeFile(t, filepath.Join(repo, "src", "nested", "b.go"), "package nested\n")
	detector := Detector{CWD: repo, RepoRoot: repo}

	tests := []struct {
		name     string
		command  string
		response any
		want     []string
	}{
		{name: "sed", command: `sed -n '1,80p' src/a.go`, want: []string{"src/a.go"}},
		{name: "quoted cat", command: `cat "src/a.go"`, want: []string{"src/a.go"}},
		{name: "glob", command: `cat src/**/*.go`, want: []string{"src/a.go", "src/nested/b.go"}},
		{name: "dynamic pattern", command: `rg "$PATTERN" src/a.go`, want: []string{"src/a.go"}},
		{name: "grep", command: `grep package src/a.go`, want: []string{"src/a.go"}},
		{name: "nested shell", command: `bash -c 'head -10 src/a.go'`, want: []string{"src/a.go"}},
		{name: "input redirect", command: `wc -l < src/a.go`, want: []string{"src/a.go"}},
		{name: "rg output", command: `rg package src`, response: map[string]any{"output": "src/a.go:1:package src"}, want: []string{"src/a.go"}},
		{name: "rg files with matches", command: `rg -l package src`, response: map[string]any{"output": "src/a.go"}, want: []string{"src/a.go"}},
		{name: "git diff output", command: `git diff HEAD^`, response: map[string]any{"output": "diff --git a/src/a.go b/src/a.go\n--- a/src/a.go\n+++ b/src/a.go"}, want: []string{"src/a.go"}},
		{name: "git name only output", command: `git diff --name-only HEAD^`, response: map[string]any{"output": "src/a.go"}, want: []string{"src/a.go"}},
		{name: "git show object path", command: `git show HEAD:src/a.go`, want: []string{"src/a.go"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response, err := json.Marshal(test.response)
			if err != nil {
				t.Fatal(err)
			}
			got, err := detector.Paths(test.command, response)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("got %v, want %v", got, test.want)
			}
		})
	}
}

func TestIgnoresListingsBuildsAndRuntimes(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "src", "a.go"), "package src\n")
	detector := Detector{CWD: repo, RepoRoot: repo}
	response, err := json.Marshal(map[string]any{"output": "src/a.go:12: build failed"})
	if err != nil {
		t.Fatal(err)
	}

	for _, command := range []string{
		"ls src",
		"rg --files src",
		"git status",
		"pnpm test",
		"go test ./...",
		"python3 scripts/check.py",
	} {
		t.Run(command, func(t *testing.T) {
			got, err := detector.Paths(command, response)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 0 {
				t.Fatalf("got paths from a non-read command: %v", got)
			}
		})
	}
}

func TestDoesNotTreatReadContentsAsMorePaths(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "src", "a.go"), "package src\n")
	writeFile(t, filepath.Join(repo, "src", "nested", "b.go"), "package nested\n")
	detector := Detector{CWD: repo, RepoRoot: repo}
	response, err := json.Marshal(map[string]any{"output": "src/nested/b.go:12"})
	if err != nil {
		t.Fatal(err)
	}

	got, err := detector.Paths("cat src/a.go", response)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"src/a.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
