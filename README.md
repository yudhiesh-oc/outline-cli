# Outline CLI

Command-line client for searching and managing an Outline workspace from a terminal.

## Install

Install the latest Homebrew release from the custom tap:

```sh
brew install yudhiesh-oc/outline-cli/outline-cli
```

Or install the command with Go:

```sh
go install github.com/yudhiesh-oc/outline-cli/cmd/outline@latest
```

## Configure

Set `OUTLINE_URL` and `OUTLINE_API_KEY`, or set `OUTLINE_CONFIG` to a JSON file containing `url` and `apiKey`. With no explicit `OUTLINE_CONFIG`, the CLI reads `outline/config.json` under the operating system's user configuration directory. Never print API keys or other credentials.

## Usage

```sh
outline search "query" --limit 5
outline get DOCUMENT_ID
outline create "Title" --collection COLLECTION_UUID --text "Body"
outline update DOCUMENT_ID --patch --find "old" --text "new"
```

Summaries are returned by default; add `--raw` when the full API response is required. Run `outline <command> --help` to discover command-specific options.

## Install the Outline skill in LLM harnesses

The CLI ships the Outline skill and writes it into the harness's user-level skills directory:

```sh
outline skill install claude-code
outline skill install codex
outline skill install cursor
outline skill install gemini
outline skill install pi
```

| Harness | Skill path |
| --- | --- |
| `claude-code` | `~/.claude/skills/outline/SKILL.md` |
| `codex` | `~/.codex/skills/outline/SKILL.md` |
| `cursor` | `~/.cursor/skills/outline/SKILL.md` |
| `gemini` | `~/.gemini/skills/outline/SKILL.md` |
| `pi` | `~/.pi/agent/skills/outline/SKILL.md` |

Each command prints the installed path and stops with an error if a skill file already exists; pass `--force` to replace it. Homebrew also installs `ol` as a shortcut, so `ol skill install pi` is equivalent to `outline skill install pi`. The embedded skill source is [`internal/skill/outline/SKILL.md`](internal/skill/outline/SKILL.md).

## Exit codes

- `0` — success.
- `1` — runtime, API, or output failure.
- `2` — usage error.

## License

MIT
