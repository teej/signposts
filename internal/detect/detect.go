package detect

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"mvdan.cc/sh/v3/syntax"
)

var (
	diffPathPattern   = regexp.MustCompile(`(?m)^diff --git a/(.+?) b/(.+)$|^--- (?:a/)?(.+)$|^\+\+\+ (?:b/)?(.+)$`)
	outputPathPattern = regexp.MustCompile(`(?m)^([^:\n]+?):(?:[0-9]+:)?`)
)

type Detector struct {
	CWD      string
	RepoRoot string
}

func (detector Detector) Paths(command string, toolResponse json.RawMessage) ([]string, error) {
	if strings.TrimSpace(command) == "" {
		return nil, nil
	}

	candidates := make(map[string]struct{})
	contentRead, inspectOutput, err := detector.pathsFromCommand(command, candidates)
	if err != nil {
		return nil, err
	}
	if !contentRead {
		return nil, nil
	}

	if inspectOutput {
		response := flattenJSONStrings(toolResponse)
		detector.pathsFromOutput(response, candidates)
	}

	paths := make([]string, 0, len(candidates))
	for path := range candidates {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

func (detector Detector) pathsFromCommand(command string, candidates map[string]struct{}) (bool, bool, error) {
	parser := syntax.NewParser(syntax.KeepComments(false))
	program, err := parser.Parse(strings.NewReader(command), "")
	if err != nil {
		return false, false, fmt.Errorf("parse shell command: %w", err)
	}

	contentRead := false
	inspectOutput := false
	syntax.Walk(program, func(node syntax.Node) bool {
		statement, ok := node.(*syntax.Stmt)
		if !ok {
			return true
		}

		for _, redirect := range statement.Redirs {
			if redirect.Op != syntax.RdrIn && redirect.Op != syntax.RdrInOut {
				continue
			}
			if path, static := staticWord(redirect.Word); static {
				detector.addCandidate(path, candidates)
				contentRead = true
			}
		}

		call, ok := statement.Cmd.(*syntax.CallExpr)
		if !ok {
			return true
		}
		words := staticWords(call.Args)
		if len(words) == 0 || words[0] == "" {
			return true
		}
		callRead, callOutput := detector.pathsFromCall(words, candidates)
		if callRead {
			contentRead = true
		}
		if callOutput {
			inspectOutput = true
		}
		return true
	})
	return contentRead, inspectOutput, nil
}

func (detector Detector) pathsFromCall(words []string, candidates map[string]struct{}) (bool, bool) {
	words = unwrapCommand(words)
	if len(words) == 0 {
		return false, false
	}

	command := filepath.Base(words[0])
	args := words[1:]
	switch command {
	case "cat", "nl", "bat", "less", "more":
		detector.addFileArguments(args, candidates)
		return true, false
	case "head", "tail":
		detector.addFileArguments(args, candidates)
		return true, false
	case "sed":
		for _, path := range sedFiles(args) {
			detector.addCandidate(path, candidates)
		}
		return true, false
	case "awk", "nawk", "gawk":
		for _, path := range awkFiles(args) {
			detector.addCandidate(path, candidates)
		}
		return true, false
	case "grep", "egrep", "fgrep":
		for _, path := range grepFiles(args) {
			detector.addCandidate(path, candidates)
		}
		return true, true
	case "rg", "ripgrep":
		if hasArgument(args, "--files") {
			return false, false
		}
		for _, path := range ripgrepFiles(args) {
			detector.addCandidate(path, candidates)
		}
		return true, true
	case "git":
		contentRead := detector.pathsFromGit(args, candidates)
		return contentRead, contentRead
	case "sh", "bash", "zsh", "dash", "ksh":
		if script := shellCommandString(args); script != "" {
			contentRead, inspectOutput, _ := detector.pathsFromCommand(script, candidates)
			return contentRead, inspectOutput
		}
	}
	return false, false
}

func (detector Detector) pathsFromGit(args []string, candidates map[string]struct{}) bool {
	subcommandIndex := -1
	for index, arg := range args {
		if arg == "diff" || arg == "show" || arg == "blame" || arg == "log" {
			subcommandIndex = index
			break
		}
	}
	if subcommandIndex < 0 {
		return false
	}

	for _, arg := range args[subcommandIndex+1:] {
		if strings.HasPrefix(arg, "-") || arg == "--" {
			continue
		}
		detector.addCandidate(arg, candidates)
		if colon := strings.IndexByte(arg, ':'); colon > 0 && colon < len(arg)-1 {
			detector.addCandidate(arg[colon+1:], candidates)
		}
	}
	return true
}

func (detector Detector) pathsFromOutput(output string, candidates map[string]struct{}) {
	for _, match := range diffPathPattern.FindAllStringSubmatch(output, -1) {
		for _, path := range match[1:] {
			if path != "" && path != "/dev/null" {
				detector.addCandidate(path, candidates)
			}
		}
	}
	for _, match := range outputPathPattern.FindAllStringSubmatch(output, -1) {
		detector.addCandidate(match[1], candidates)
	}
	for _, line := range strings.Split(output, "\n") {
		detector.addCandidate(line, candidates)
	}
}

func (detector Detector) addFileArguments(args []string, candidates map[string]struct{}) {
	for _, arg := range args {
		if arg == "-" || strings.HasPrefix(arg, "-") {
			continue
		}
		detector.addCandidate(arg, candidates)
	}
}

func (detector Detector) addCandidate(rawPath string, candidates map[string]struct{}) {
	path := strings.TrimSpace(strings.Trim(rawPath, `"'`))
	if path == "" || path == "-" || strings.HasPrefix(path, "-") {
		return
	}

	paths := []string{path}
	if hasMeta(path) {
		pattern := path
		if !filepath.IsAbs(pattern) {
			pattern = filepath.Join(detector.CWD, pattern)
		}
		matches, err := doublestar.FilepathGlob(pattern)
		if err != nil {
			return
		}
		paths = matches
	}

	for _, path := range paths {
		absolute := path
		if !filepath.IsAbs(absolute) {
			absolute = filepath.Join(detector.CWD, absolute)
		}
		absolute, err := filepath.Abs(absolute)
		if err != nil {
			continue
		}
		info, err := os.Stat(absolute)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}

		resolved, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			continue
		}
		if !within(detector.RepoRoot, resolved) {
			continue
		}
		relative, err := filepath.Rel(detector.RepoRoot, absolute)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			continue
		}
		candidates[filepath.ToSlash(relative)] = struct{}{}
	}
}

func staticWords(words []*syntax.Word) []string {
	values := make([]string, 0, len(words))
	for _, word := range words {
		value, static := staticWord(word)
		if !static {
			value = ""
		}
		values = append(values, value)
	}
	return values
}

func staticWord(word *syntax.Word) (string, bool) {
	if word == nil {
		return "", false
	}
	var builder strings.Builder
	for _, part := range word.Parts {
		switch part := part.(type) {
		case *syntax.Lit:
			builder.WriteString(part.Value)
		case *syntax.SglQuoted:
			builder.WriteString(part.Value)
		case *syntax.DblQuoted:
			value, static := staticWord(&syntax.Word{Parts: part.Parts})
			if !static {
				return "", false
			}
			builder.WriteString(value)
		default:
			return "", false
		}
	}
	return builder.String(), true
}

func unwrapCommand(words []string) []string {
	for len(words) > 0 {
		switch filepath.Base(words[0]) {
		case "command", "builtin", "nohup", "sudo":
			words = words[1:]
		case "env":
			words = words[1:]
			for len(words) > 0 && (strings.HasPrefix(words[0], "-") || strings.Contains(words[0], "=")) {
				words = words[1:]
			}
		default:
			return words
		}
	}
	return words
}

func sedFiles(args []string) []string {
	files := []string{}
	hasProgramOption := false
	programConsumed := false
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "-e" || arg == "--expression" {
			hasProgramOption = true
			index++
			continue
		}
		if strings.HasPrefix(arg, "-e") && arg != "-e" {
			hasProgramOption = true
			continue
		}
		if arg == "-f" || arg == "--file" {
			hasProgramOption = true
			if index+1 < len(args) {
				files = append(files, args[index+1])
				index++
			}
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if !hasProgramOption && !programConsumed {
			programConsumed = true
			continue
		}
		files = append(files, arg)
	}
	return files
}

func awkFiles(args []string) []string {
	files := []string{}
	programConsumed := false
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "-f" || arg == "--file" {
			if index+1 < len(args) {
				files = append(files, args[index+1])
				index++
			}
			programConsumed = true
			continue
		}
		if arg == "-v" {
			index++
			continue
		}
		if strings.HasPrefix(arg, "-") || strings.Contains(arg, "=") {
			continue
		}
		if !programConsumed {
			programConsumed = true
			continue
		}
		files = append(files, arg)
	}
	return files
}

func grepFiles(args []string) []string {
	return searchFiles(args, map[string]bool{
		"-e": true, "--regexp": true, "-f": true, "--file": true,
		"-A": true, "--after-context": true, "-B": true, "--before-context": true,
		"-C": true, "--context": true, "-m": true, "--max-count": true,
	})
}

func ripgrepFiles(args []string) []string {
	return searchFiles(args, map[string]bool{
		"-e": true, "--regexp": true, "-f": true, "--file": true,
		"-g": true, "--glob": true, "-t": true, "--type": true,
		"-T": true, "--type-not": true, "-A": true, "--after-context": true,
		"-B": true, "--before-context": true, "-C": true, "--context": true,
		"-m": true, "--max-count": true, "--max-depth": true,
	})
}

func searchFiles(args []string, optionsWithValues map[string]bool) []string {
	files := []string{}
	patternConsumed := false
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			if !patternConsumed && index+1 < len(args) {
				patternConsumed = true
				index++
			}
			files = append(files, args[index+1:]...)
			break
		}
		if optionsWithValues[arg] {
			if arg == "-e" || arg == "--regexp" {
				patternConsumed = true
			}
			index++
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if !patternConsumed {
			patternConsumed = true
			continue
		}
		files = append(files, arg)
	}
	return files
}

func shellCommandString(args []string) string {
	for index, arg := range args {
		if arg == "-c" && index+1 < len(args) {
			return args[index+1]
		}
	}
	return ""
}

func hasArgument(args []string, expected string) bool {
	for _, arg := range args {
		if arg == expected {
			return true
		}
	}
	return false
}

func hasMeta(path string) bool {
	return strings.ContainsAny(path, "*?[")
}

func within(root, path string) bool {
	root, rootErr := filepath.Abs(root)
	path, pathErr := filepath.Abs(path)
	if rootErr != nil || pathErr != nil {
		return false
	}
	if resolvedRoot, err := filepath.EvalSymlinks(root); err == nil {
		root = resolvedRoot
	}
	if resolvedPath, err := filepath.EvalSymlinks(path); err == nil {
		path = resolvedPath
	}
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func flattenJSONStrings(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	strings := []string{}
	flatten(value, &strings)
	return joinNonempty(strings)
}

func flatten(value any, values *[]string) {
	switch value := value.(type) {
	case string:
		*values = append(*values, value)
	case []any:
		for _, item := range value {
			flatten(item, values)
		}
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			flatten(value[key], values)
		}
	}
}

func joinNonempty(values []string) string {
	filtered := values[:0]
	for _, value := range values {
		if value != "" {
			filtered = append(filtered, value)
		}
	}
	return strings.Join(filtered, "\n")
}
