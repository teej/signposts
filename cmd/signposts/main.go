package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/teej/signposts/internal/hook"
	"github.com/teej/signposts/internal/rules"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "hook":
		runHook()
	case "check":
		runCheck(os.Args[2:])
	case "version", "--version", "-version":
		fmt.Println(version)
	default:
		usage()
		os.Exit(2)
	}
}

func runHook() {
	event := hook.Event{}
	if err := json.NewDecoder(os.Stdin).Decode(&event); err != nil {
		debugError(fmt.Errorf("decode hook event: %w", err))
		return
	}
	output, err := hook.NewHandler().Handle(event)
	if err != nil {
		debugError(err)
		return
	}
	if output == nil {
		return
	}
	if err := json.NewEncoder(os.Stdout).Encode(output); err != nil {
		debugError(fmt.Errorf("encode hook output: %w", err))
	}
}

func runCheck(args []string) {
	cwd, err := os.Getwd()
	if err != nil {
		fatal(err)
	}
	repoRoot, err := hook.FindRepoRoot(cwd)
	if err != nil {
		fatal(err)
	}
	loaded := rules.Load(repoRoot)
	if len(loaded.Warnings) > 0 {
		for _, warning := range loaded.Warnings {
			fmt.Fprintf(os.Stderr, "warning: %v\n", warning)
		}
	}

	path := ""
	if len(args) > 0 {
		path = args[0]
		if filepath.IsAbs(path) {
			path, err = filepath.Rel(repoRoot, path)
			if err != nil {
				fatal(err)
			}
		}
		path = filepath.ToSlash(path)
	}

	matched := []string{}
	for _, rule := range loaded.Rules {
		if path == "" || !rule.Conditional() || rule.Matches(path) {
			matched = append(matched, rule.Source)
		}
	}
	sort.Strings(matched)
	if path == "" {
		fmt.Printf("Repository: %s\n", repoRoot)
		fmt.Printf("Loaded rules: %d\n", len(loaded.Rules))
	} else {
		fmt.Printf("Repository: %s\n", repoRoot)
		fmt.Printf("Path: %s\n", path)
		fmt.Printf("Applicable rules: %d\n", len(matched))
	}
	for _, source := range matched {
		fmt.Printf("- %s\n", source)
	}
	if len(loaded.Warnings) > 0 {
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: signposts <hook|check|version>")
}

func debugError(err error) {
	if os.Getenv("SIGNPOSTS_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "signposts: %v\n", err)
	}
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "signposts: %v\n", err)
	os.Exit(1)
}
