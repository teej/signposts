package hook

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/teej/signposts/internal/detect"
	"github.com/teej/signposts/internal/rules"
	"github.com/teej/signposts/internal/state"
)

type Event struct {
	SessionID     string          `json:"session_id"`
	TurnID        string          `json:"turn_id"`
	CWD           string          `json:"cwd"`
	HookEventName string          `json:"hook_event_name"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
	ToolResponse  json.RawMessage `json:"tool_response"`
	Source        string          `json:"source"`
}

type Output struct {
	SystemMessage      string              `json:"systemMessage,omitempty"`
	HookSpecificOutput *hookSpecificOutput `json:"hookSpecificOutput,omitempty"`
}

type hookSpecificOutput struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext,omitempty"`
}

type commandInput struct {
	Command string `json:"command"`
	Cmd     string `json:"cmd"`
}

type matchedRule struct {
	Rule        rules.Rule
	MatchedPath string
}

type Handler struct {
	Store state.Store
}

func NewHandler() Handler {
	return Handler{Store: state.New(state.DefaultRoot())}
}

func (handler Handler) Handle(event Event) (*Output, error) {
	if event.CWD == "" || event.SessionID == "" {
		return nil, errors.New("hook event is missing cwd or session_id")
	}
	repoRoot, err := FindRepoRoot(event.CWD)
	if err != nil {
		return nil, err
	}

	switch event.HookEventName {
	case "SessionStart":
		return handler.sessionStart(event, repoRoot)
	case "PostToolUse":
		return handler.postToolUse(event, repoRoot)
	case "SessionEnd":
		return nil, handler.Store.Clear(repoRoot, event.SessionID)
	default:
		return nil, nil
	}
}

func (handler Handler) sessionStart(event Event, repoRoot string) (*Output, error) {
	if event.Source == "clear" {
		if err := handler.Store.Clear(repoRoot, event.SessionID); err != nil {
			return nil, err
		}
	}

	loaded := rules.Load(repoRoot)
	if event.Source == "compact" {
		emitted, err := handler.Store.Emitted(repoRoot, event.SessionID)
		if err != nil {
			return nil, err
		}
		emittedSet := make(map[string]struct{}, len(emitted))
		for _, id := range emitted {
			emittedSet[id] = struct{}{}
		}

		matches := []matchedRule{}
		for _, rule := range loaded.Rules {
			_, wasEmitted := emittedSet[rule.ID]
			if !rule.Conditional() || wasEmitted {
				matches = append(matches, matchedRule{Rule: rule})
			}
		}
		return makeOutput("SessionStart", formatRestoredRules(matches), loaded.Warnings), nil
	}

	matches := []matchedRule{}
	for _, rule := range loaded.Rules {
		if rule.Conditional() {
			continue
		}
		reserved, err := handler.Store.Reserve(repoRoot, event.SessionID, rule.ID)
		if err != nil {
			return nil, err
		}
		if reserved {
			matches = append(matches, matchedRule{Rule: rule})
		}
	}
	return makeOutput("SessionStart", formatLoadedRules(matches), loaded.Warnings), nil
}

func (handler Handler) postToolUse(event Event, repoRoot string) (*Output, error) {
	if event.ToolName != "Bash" {
		return nil, nil
	}
	input := commandInput{}
	if err := json.Unmarshal(event.ToolInput, &input); err != nil {
		return nil, fmt.Errorf("parse Bash tool input: %w", err)
	}
	command := input.Command
	if command == "" {
		command = input.Cmd
	}

	detector := detect.Detector{CWD: event.CWD, RepoRoot: repoRoot}
	paths, err := detector.Paths(command, event.ToolResponse)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, nil
	}

	loaded := rules.Load(repoRoot)
	matches := []matchedRule{}
	for _, rule := range loaded.Rules {
		if !rule.Conditional() {
			continue
		}
		matchedPath := firstMatch(rule, paths)
		if matchedPath == "" {
			continue
		}
		reserved, err := handler.Store.Reserve(repoRoot, event.SessionID, rule.ID)
		if err != nil {
			return nil, err
		}
		if reserved {
			matches = append(matches, matchedRule{Rule: rule, MatchedPath: matchedPath})
		}
	}
	return makeOutput("PostToolUse", formatMatchedRules(matches), nil), nil
}

func FindRepoRoot(cwd string) (string, error) {
	directory, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("resolve cwd: %w", err)
	}
	info, err := os.Stat(directory)
	if err != nil {
		return "", fmt.Errorf("read cwd: %w", err)
	}
	if !info.IsDir() {
		directory = filepath.Dir(directory)
	}
	startDirectory := directory

	for {
		if _, err := os.Stat(filepath.Join(directory, ".git")); err == nil {
			return directory, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return startDirectory, nil
		}
		directory = parent
	}
}

func firstMatch(rule rules.Rule, paths []string) string {
	for _, path := range paths {
		if rule.Matches(path) {
			return path
		}
	}
	return ""
}

func makeOutput(eventName, context string, warnings []error) *Output {
	if context == "" && len(warnings) == 0 {
		return nil
	}
	output := &Output{}
	if context != "" {
		output.HookSpecificOutput = &hookSpecificOutput{
			HookEventName:     eventName,
			AdditionalContext: context,
		}
	}
	if len(warnings) > 0 {
		messages := make([]string, 0, len(warnings))
		for _, warning := range warnings {
			messages = append(messages, warning.Error())
		}
		sort.Strings(messages)
		output.SystemMessage = "Some repository rules could not be loaded:\n- " + strings.Join(messages, "\n- ")
	}
	return output
}

func formatLoadedRules(matches []matchedRule) string {
	parts := make([]string, 0, len(matches))
	for _, match := range matches {
		parts = append(parts, fmt.Sprintf(
			"Repository rule loaded for this project.\nRule source: %s\n\n%s",
			match.Rule.Source,
			match.Rule.Text,
		))
	}
	return strings.Join(parts, "\n\n---\n\n")
}

func formatMatchedRules(matches []matchedRule) string {
	parts := make([]string, 0, len(matches))
	for _, match := range matches {
		parts = append(parts, fmt.Sprintf(
			"A repository rule now applies because the preceding tool read `%s`.\nRule source: %s\n\n%s",
			match.MatchedPath,
			match.Rule.Source,
			match.Rule.Text,
		))
	}
	return strings.Join(parts, "\n\n---\n\n")
}

func formatRestoredRules(matches []matchedRule) string {
	parts := make([]string, 0, len(matches))
	for _, match := range matches {
		parts = append(parts, fmt.Sprintf(
			"Repository rule restored after context compaction.\nRule source: %s\n\n%s",
			match.Rule.Source,
			match.Rule.Text,
		))
	}
	return strings.Join(parts, "\n\n---\n\n")
}
