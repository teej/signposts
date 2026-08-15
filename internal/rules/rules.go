package rules

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"
)

const maxRuleBytes = 1 << 20

type Rule struct {
	ID     string
	Source string
	Paths  []string
	Text   string
}

type LoadResult struct {
	Rules    []Rule
	Warnings []error
}

type sourceRoot struct {
	path        string
	displayRoot string
}

type frontMatter struct {
	Paths pathList `yaml:"paths"`
}

type pathList []string

func (paths *pathList) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		var path string
		if err := node.Decode(&path); err != nil {
			return err
		}
		*paths = []string{path}
		return nil
	case yaml.SequenceNode:
		var values []string
		if err := node.Decode(&values); err != nil {
			return err
		}
		*paths = values
		return nil
	case 0:
		return nil
	default:
		return errors.New("paths must be a string or a list of strings")
	}
}

func Load(repoRoot string) LoadResult {
	result := LoadResult{}
	home := os.Getenv("SIGNPOSTS_USER_HOME")
	var homeErr error
	if home == "" {
		home, homeErr = os.UserHomeDir()
	}
	if homeErr != nil {
		result.Warnings = append(result.Warnings, fmt.Errorf("resolve home directory: %w", homeErr))
	}

	roots := make([]sourceRoot, 0, 4)
	if homeErr == nil {
		roots = append(roots,
			sourceRoot{path: filepath.Join(home, ".claude", "rules"), displayRoot: "~/.claude/rules"},
			sourceRoot{path: filepath.Join(home, ".codex", "signposts"), displayRoot: "~/.codex/signposts"},
		)
	}
	roots = append(roots,
		sourceRoot{path: filepath.Join(repoRoot, ".claude", "rules"), displayRoot: ".claude/rules"},
		sourceRoot{path: filepath.Join(repoRoot, ".codex", "signposts"), displayRoot: ".codex/signposts"},
	)

	seenFiles := make(map[string]struct{})
	for _, root := range roots {
		paths, warnings := markdownFiles(root.path)
		result.Warnings = append(result.Warnings, warnings...)
		for _, path := range paths {
			canonical, err := filepath.EvalSymlinks(path)
			if err != nil {
				result.Warnings = append(result.Warnings, fmt.Errorf("resolve rule %s: %w", path, err))
				continue
			}
			canonical, err = filepath.Abs(canonical)
			if err != nil {
				result.Warnings = append(result.Warnings, fmt.Errorf("resolve rule %s: %w", path, err))
				continue
			}
			if _, exists := seenFiles[canonical]; exists {
				continue
			}

			relative, err := filepath.Rel(root.path, path)
			if err != nil {
				result.Warnings = append(result.Warnings, fmt.Errorf("name rule %s: %w", path, err))
				continue
			}
			source := filepath.ToSlash(filepath.Join(root.displayRoot, relative))
			rule, err := parse(path, canonical, source)
			if err != nil {
				result.Warnings = append(result.Warnings, err)
				continue
			}

			seenFiles[canonical] = struct{}{}
			result.Rules = append(result.Rules, rule)
		}
	}

	sort.Slice(result.Rules, func(i, j int) bool {
		return result.Rules[i].Source < result.Rules[j].Source
	})
	return result
}

func (rule Rule) Conditional() bool {
	return len(rule.Paths) > 0
}

func (rule Rule) Matches(repoRelativePath string) bool {
	path := normalizePath(repoRelativePath)
	for _, rawPattern := range rule.Paths {
		for _, pattern := range expandBraces(normalizePattern(rawPattern)) {
			matched, err := doublestar.Match(pattern, path)
			if err == nil && matched {
				return true
			}
		}
	}
	return false
}

func parse(path, canonical, source string) (Rule, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Rule{}, fmt.Errorf("stat rule %s: %w", source, err)
	}
	if info.Size() > maxRuleBytes {
		return Rule{}, fmt.Errorf("rule %s exceeds %d bytes", source, maxRuleBytes)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		return Rule{}, fmt.Errorf("read rule %s: %w", source, err)
	}
	contents = bytes.TrimPrefix(contents, []byte{0xef, 0xbb, 0xbf})
	metadata, body, err := splitFrontMatter(contents)
	if err != nil {
		return Rule{}, fmt.Errorf("parse rule %s: %w", source, err)
	}

	front := frontMatter{}
	if len(metadata) > 0 {
		if err := yaml.Unmarshal(metadata, &front); err != nil {
			return Rule{}, fmt.Errorf("parse rule %s frontmatter: %w", source, err)
		}
	}

	text := strings.TrimSpace(string(body))
	if text == "" {
		return Rule{}, fmt.Errorf("rule %s has no instructions", source)
	}

	paths := make([]string, 0, len(front.Paths))
	for _, pattern := range front.Paths {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			return Rule{}, fmt.Errorf("rule %s has an empty path pattern", source)
		}
		paths = append(paths, pattern)
	}

	idHash := sha256.Sum256([]byte(canonical))
	return Rule{
		ID:     fmt.Sprintf("%x", idHash[:]),
		Source: source,
		Paths:  paths,
		Text:   text,
	}, nil
}

func splitFrontMatter(contents []byte) ([]byte, []byte, error) {
	normalized := bytes.ReplaceAll(contents, []byte("\r\n"), []byte("\n"))
	if !bytes.HasPrefix(normalized, []byte("---\n")) {
		return nil, normalized, nil
	}

	rest := normalized[len("---\n"):]
	lines := bytes.Split(rest, []byte("\n"))
	for index, line := range lines {
		if bytes.Equal(line, []byte("---")) || bytes.Equal(line, []byte("...")) {
			metadata := bytes.Join(lines[:index], []byte("\n"))
			body := bytes.Join(lines[index+1:], []byte("\n"))
			return metadata, body, nil
		}
	}
	return nil, nil, errors.New("unterminated YAML frontmatter")
}

func markdownFiles(root string) ([]string, []error) {
	if _, err := os.Stat(root); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, []error{fmt.Errorf("scan rules at %s: %w", root, err)}
	}

	files := []string{}
	warnings := []error{}
	seenDirectories := make(map[string]struct{})

	var walk func(string)
	walk = func(directory string) {
		canonical, err := filepath.EvalSymlinks(directory)
		if err != nil {
			warnings = append(warnings, fmt.Errorf("scan rules at %s: %w", directory, err))
			return
		}
		canonical, err = filepath.Abs(canonical)
		if err != nil {
			warnings = append(warnings, fmt.Errorf("scan rules at %s: %w", directory, err))
			return
		}
		if _, exists := seenDirectories[canonical]; exists {
			return
		}
		seenDirectories[canonical] = struct{}{}

		entries, err := os.ReadDir(directory)
		if err != nil {
			warnings = append(warnings, fmt.Errorf("scan rules at %s: %w", directory, err))
			return
		}
		for _, entry := range entries {
			path := filepath.Join(directory, entry.Name())
			info, err := os.Stat(path)
			if err != nil {
				warnings = append(warnings, fmt.Errorf("scan rule %s: %w", path, err))
				continue
			}
			if info.IsDir() {
				walk(path)
				continue
			}
			if info.Mode().IsRegular() && strings.EqualFold(filepath.Ext(path), ".md") {
				files = append(files, path)
			}
		}
	}

	walk(root)
	sort.Strings(files)
	return files, warnings
}

func normalizePath(path string) string {
	return strings.TrimPrefix(filepath.ToSlash(filepath.Clean(path)), "./")
}

func normalizePattern(pattern string) string {
	pattern = strings.TrimSpace(filepath.ToSlash(pattern))
	pattern = strings.TrimPrefix(pattern, "./")
	pattern = strings.TrimPrefix(pattern, "/")
	return pattern
}

func expandBraces(pattern string) []string {
	open := strings.IndexByte(pattern, '{')
	if open < 0 {
		return []string{pattern}
	}
	closeOffset := strings.IndexByte(pattern[open+1:], '}')
	if closeOffset < 0 {
		return []string{pattern}
	}
	close := open + 1 + closeOffset
	options := strings.Split(pattern[open+1:close], ",")
	if len(options) < 2 {
		return []string{pattern}
	}

	result := []string{}
	for _, option := range options {
		expanded := pattern[:open] + option + pattern[close+1:]
		result = append(result, expandBraces(expanded)...)
	}
	return result
}
