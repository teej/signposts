# signposts

Signposts adds lazy, path-scoped repository rules to Codex.

Rules load when Codex reads a matching file. This keeps specialized guidance out of the initial context while making its activation deterministic. Existing Claude Code rules work without modification.

## Installation

Add the Signposts marketplace from GitHub, then install the plugin:

```sh
codex plugin marketplace add teej/signposts
codex plugin add signposts@signposts
```

Start a new Codex task after installation. The plugin ships prebuilt binaries for macOS, Linux, and Windows; Go is not required to use it.

To pin the marketplace checkout to a release or commit, add `--ref <tag-or-sha>` to the first command.

## Rule locations

Signposts scans these directories recursively:

| Scope | Claude-compatible | Codex-specific |
| --- | --- | --- |
| Project | `.claude/rules/` | `.codex/signposts/` |
| User | `~/.claude/rules/` | `~/.codex/signposts/` |

`.codex/rules/` is not used because Codex reserves it for command execution policies.

## Rule format

Rules are Markdown files with optional YAML frontmatter:

```md
---
paths:
  - "client/src/sync/**"
  - "common/src/sync/**/*.{ts,tsx}"
---

Before changing this component, read `guides/sync-actions.md`.
Preserve transaction IDs when deriving sync actions.
```

Rules with `paths` load after a shell operation reads a matching repository file. Rules without `paths` load when the Codex session starts. Each rule loads once per session and is restored after context compaction.

The rule's source path is its identity. One rule that covers several files still loads once.

## How it works

The Codex plugin registers synchronous `SessionStart`, `PostToolUse`, and `SessionEnd` hooks. A small Go executable:

1. Parses completed shell commands and their results.
2. Identifies repository files intentionally inspected by the operation.
3. Matches those paths against applicable rules.
4. Atomically reserves unseen rules for the current session.
5. Returns the rules through Codex `additionalContext` as developer context.

The hook does not rewrite commands, alter exit codes, replace tool results, or append to stderr. Nested `exec_command` calls made programmatically from Codex code mode use the same hook path.

Supported reads include `cat`, `head`, `tail`, `sed`, `awk`, `grep`, `rg`, `nl`, `bat`, `less`, shell input redirections, and content-oriented `git diff`, `git show`, `git blame`, and `git log` calls. Directory listings, `rg --files`, builds, tests, and language runtimes do not trigger rules merely because their output mentions a source path.

Arbitrary scripts that read files internally are outside the first release's deterministic detector.

## Development

The plugin ships prebuilt binaries so users do not need Go installed. To build them from source:

```sh
make build-all
```

Run the unit tests and static checks:

```sh
go test -race ./...
go vet ./...
```

Inspect the rules that apply to a path:

```sh
plugins/signposts/scripts/run-signposts check client/src/sync/SyncAction.ts
```

Run the opt-in Codex code-mode proof after building the current-platform binary:

```sh
plugins/signposts/scripts/test-code-mode
```

The proof starts an ephemeral Codex session, makes it read a matching file through a JavaScript `tools.exec_command` call, and verifies that the resulting answer follows a rule whose canary was absent from the prompt and target file.
