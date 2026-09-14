# Outline CLI

Command-line client for searching and managing an [Outline](https://www.getoutline.com/) workspace from a terminal. Every command prints compact JSON on stdout and `outline: <message>` errors on stderr, so it works the same in a shell script and in an LLM agent harness.

- `outline` — the CLI.
- `ol` — the same command, installed as a Homebrew shortcut.

## Install

### Homebrew (macOS, Linux)

```sh
brew install yudhiesh-oc/outline-cli/outline-cli
outline --version   # and: ol --version
```

Upgrade with `brew upgrade outline-cli`, remove with `brew uninstall outline-cli`.

### Go

```sh
go install github.com/yudhiesh-oc/outline-cli/cmd/outline@latest
```

The binary lands in `$(go env GOPATH)/bin`. `go install` builds report `dev` for `--version`; release binaries report the tagged version.

## Configure

### Create an API key

In Outline open **Settings → API Keys → Create API Key**, optionally limiting scopes (`documents.info`, `documents.*`, …). An unscoped key has the creating user's permissions. Copy the key when it is shown; Outline does not display it again. See the [Outline API documentation](https://docs.getoutline.com/s/guide/doc/api-1rEIXDfLF6).

### Provide the workspace URL and key

Environment variables:

```sh
export OUTLINE_URL="https://your-workspace.getoutline.com"
export OUTLINE_API_KEY="ol_api_..."
```

Or a config file containing exactly these fields:

```json
{ "url": "https://your-workspace.getoutline.com", "apiKey": "ol_api_..." }
```

The CLI reads the config file named by `OUTLINE_CONFIG`; with no `OUTLINE_CONFIG` it looks for `outline/config.json` in the OS user config directory — macOS `~/Library/Application Support/outline/config.json`, Linux `$XDG_CONFIG_HOME/outline/config.json` (default `~/.config/outline/config.json`), Windows `%AppData%\outline\config.json`.

```sh
mkdir -p ~/.config/outline
printf '%s\n' '{"url":"https://your-workspace.getoutline.com","apiKey":"ol_api_..."}' > ~/.config/outline/config.json
chmod 600 ~/.config/outline/config.json
export OUTLINE_CONFIG="$HOME/.config/outline/config.json"   # only needed off Linux
```

Rules that matter:

- `OUTLINE_URL` is the workspace base URL: absolute `http://` or `https://`, no `/api` suffix, no query, no fragment, and a trailing slash is ignored. Self-hosted paths such as `https://kb.example.com/outline` are supported.
- Environment variables win over the config file. A complete `OUTLINE_URL` + `OUTLINE_API_KEY` pair is used as-is; otherwise the config file supplies the missing value.
- A key saved in the config file is only used with the URL saved next to it. Overriding the URL is refused unless `OUTLINE_API_KEY` is also set.
- Setting `OUTLINE_CONFIG` to a missing file is an error even when both environment variables are set.
- The CLI never prints credentials. Use a least-privilege key and keep it out of shell history.

### Check it works, and find IDs

```sh
outline collections                        # collection UUIDs
outline search "runbook" --limit 5         # ranked hits: .document.id plus .context
outline list --collection COLLECTION_UUID  # document metadata in a collection
outline tree COLLECTION_UUID               # the collection's document hierarchy
```

## Command reference

Read:

| Command | Returns |
| --- | --- |
| `outline search <query> [--collection UUID] [--limit N] [--offset N] [--all]` | Ranked hits; each has `document`, `context`, and `ranking`. |
| `outline get <id-or-urlId>` | One document including its markdown `text`. Pass a document `id` (UUID) or the `urlId` from a document URL, e.g. `runbook-deploy-abc123`. |
| `outline list [--collection UUID] [--parent UUID] [--sort F] [--direction asc\|desc] [--limit N] [--offset N] [--all]` | Document metadata. `--sort` is `updatedAt`, `createdAt`, `title`, or `index`. |
| `outline collections [name-filter]` | Collections with `id`, `name`, `url`, `permission`, `documentsCount`. |
| `outline tree <collectionId>` | Complete published hierarchy of a collection. |
| `outline comments <documentId>` | Comments, with `createdBy` and `resolvedBy` users. |
| `outline users [name-or-email-filter]` | Workspace members. |
| `outline templates [title-filter]` | Template metadata. |

Write:

| Command | Effect |
| --- | --- |
| `outline create <title> [--collection UUID] [--parent UUID] [--text MARKDOWN] [--template ID] [--publish=false]` | Creates a document; published by default, `--publish=false` creates a draft (no collection needed). Publishing requires `--collection`. |
| `outline update <id> [--title T] [--text MARKDOWN] [--append] [--patch --find TEXT] [--publish] [--collection UUID]` | Edits a document. `--collection` here only publishes a draft; use `move` to relocate. |
| `outline move <id> [--collection UUID] [--parent UUID] [--index N]` | Moves or reorders a document; requires `--collection` or `--parent`. |
| `outline archive <id>` | Archives a document (recoverable). |
| `outline restore <id>` | Restores an archived or trashed document. |
| `outline delete <id> [--permanent]` | Moves a document to trash; `--permanent` destroys it with no undo. |
| `outline comment <documentId> <text>` | Adds a comment. |

Escape hatches:

| Command | Notes |
| --- | --- |
| `outline api <domain.action> [--data JSON]` | Any Outline REST method, e.g. `documents.unpublish`, `comments.resolve`, `collections.info`. `--data` takes one JSON object; omitting it sends `{}`. Output is always raw. |
| `outline skill install <harness> [--force]` | Installs the bundled agent skill (see below). |

Every list command takes `--limit` (1–100, default 25), `--offset`, and `--all`. `--all` follows pagination up to 50 pages and then fails instead of returning partial results; prefer bounded `--limit`/`--offset` for large workspaces.

`outline <command> --help` and `outline help <command>` print the reference for one command. Flags may appear before or after arguments, and `--` ends option parsing (use it for titles that start with a dash).

## Examples

Find a document, then read it:

```sh
outline search "deploy runbook" --limit 5
outline get "$(outline search 'deploy runbook' --limit 1 | jq -r '.[0].document.id')"
```

Edit without dropping rich formatting:

```sh
outline update DOC_ID --patch --find "Step one." --text "Step two."   # first exact match only
outline update DOC_ID --find "Step one." --patch --text ""            # empty text deletes the match
outline update DOC_ID --append --text "\nStep three."                 # nonempty text required
outline update DOC_ID --title "Runbook: deploy"                       # title only
```

`--text` alone replaces the entire body as plain markdown, so prefer `--patch` or `--append`. `--patch` and `--append` are mutually exclusive, and both require `--text`.

Create and publish:

```sh
outline create "Runbook: deploy" --collection COLLECTION_UUID --text "# Deploy\n\nStep one."
outline create "Draft idea" --publish=false                          # draft, outside any collection
outline create "RFC" --collection COLLECTION_UUID --template TEMPLATE_ID
```

Reorganize:

```sh
outline move DOC_ID --collection COLLECTION_UUID --index 0
outline move DOC_ID --parent PARENT_DOC_ID
outline tree COLLECTION_UUID
```

Review, archive, and remove:

```sh
outline comments DOC_ID
outline comment DOC_ID "Please add rollback steps."
outline archive DOC_ID                 # recoverable
outline restore DOC_ID
outline delete DOC_ID                  # trash; re-read before you do this
outline delete DOC_ID --permanent      # irreversible
```

Methods the CLI does not wrap:

```sh
outline api documents.unpublish --data '{"id":"DOC_ID"}'
outline api comments.resolve --data '{"id":"COMMENT_ID"}'
outline api collections.info --data '{"id":"COLLECTION_UUID"}'
```

## Output and limits

- stdout is compact JSON on one line; errors go to stderr as `outline: <message>`. Exit codes: `0` success, `1` API/runtime/output failure, `2` usage error.
- Summaries keep the fields an agent needs and drop document bodies and editor JSON. Add the global `--raw` flag (`outline get DOC_ID --raw` or `outline --raw get DOC_ID`) for the full REST payload; `outline api` is always raw.
- Mutations return a receipt, not the new body: re-read with `outline get` when you need the content.
- Relative `url` values such as `/doc/runbook-deploy-abc123` are workspace-relative; prefix them with `OUTLINE_URL` to open them.
- Only an HTTP 429 is retried automatically — once, honoring `Retry-After` up to 10 seconds. Every other failure (API error, timeout, or an unwritable stdout) stops immediately and prints `outline: <method>: HTTP <status>: <detail>`, so re-read with `outline get` before repeating a write.

A search hit looks like this — one compact line, keys sorted:

```console
$ outline search "deploy runbook" --limit 1
[{"context":"Step one. Step two.","document":{"archivedAt":null,"collectionId":"c0ffee00-0000-0000-0000-000000000002","id":"d1a2b3c4-0000-0000-0000-000000000001","parentDocumentId":null,"publishedAt":"2026-08-01T10:00:00.000Z","title":"Runbook: deploy","updatedAt":"2026-09-12T02:00:00.000Z","url":"/doc/runbook-deploy-abc123"},"ranking":0.93}]

$ outline search "deploy runbook" --limit 1 | jq -r '.[0].document | "\(.title) → \(.id)"'
Runbook: deploy → d1a2b3c4-0000-0000-0000-000000000001
```

### Summary fields

Summaries keep these fields, and drop fields the API does not return; the JSON object itself has its keys sorted alphabetically:

| Command | Fields |
| --- | --- |
| `get` | `id`, `title`, `url`, `text`, `collectionId`, `parentDocumentId`, `updatedAt`, `revision`, `revisionCount`, `publishedAt`, `archivedAt`, `deletedAt` |
| `search` | `context`, `ranking`, and a `document` with the `list` fields |
| `list`, `move` | `id`, `title`, `url`, `collectionId`, `parentDocumentId`, `updatedAt`, `publishedAt`, `archivedAt` |
| `create`, `update`, `archive`, `restore` | `id`, `title`, `url`, `success`, `updatedAt`, `revision`, `revisionCount`, `publishedAt`, `archivedAt`, `deletedAt` |
| `delete` | `success` |
| `collections` | `id`, `name`, `url`, `description`, `permission`, `documentsCount` |
| `users` | `id`, `name`, `email`, `role`, `isSuspended` |
| `templates` | `id`, `title`, `collectionId`, `updatedAt` |
| `comments`, `comment` | `id`, `documentId`, `parentCommentId`, `text`, `createdBy`, `createdAt`, `updatedAt`, `resolvedAt`, `resolvedBy` — plus `data` when the API returns editor JSON without computed `text` |
| `tree` | unfiltered hierarchy; `--raw` is not needed |

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

Each command prints the installed path as JSON and fails if a skill file already exists; pass `--force` to replace it. Homebrew also installs `ol`, so `ol skill install pi` is equivalent to `outline skill install pi`. The embedded skill source is [`internal/skill/outline/SKILL.md`](internal/skill/outline/SKILL.md).

## Troubleshooting

| Message | Fix |
| --- | --- |
| `no workspace URL: set OUTLINE_URL or url in the Outline config file` | No credentials were found: export the environment variables or create the config file. |
| `no API key: set OUTLINE_API_KEY or apiKey in the Outline config file` | The workspace URL is known but the key is missing. |
| `reading Outline config: open …: no such file or directory` | `OUTLINE_CONFIG` points at a file that does not exist. |
| `OUTLINE_URL differs from the saved workspace` | The environment URL must match the config file's URL, or set `OUTLINE_API_KEY` as well. |
| `HTTP 401` / `HTTP 403` | The key is wrong, revoked, expired, or lacks scope for that method. |
| `HTTP 429` | Rate limited; narrow the request or retry later. |
| `documents.info: HTTP 400: id: Must be a valid UUID or slug` | Pass an `id` or `urlId` from a listing command, not a full document URL. |
| `decoding response: invalid character '<'` | Something other than the API answered (proxy, SSO page); check `OUTLINE_URL`. |
| `--version` prints `dev` | Expected for `go install` builds; release binaries print the tag. |

## License

MIT
