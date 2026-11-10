# shlog

Go CLI for inspecting and editing shell history files. Single `main` package, stdlib only (no deps; keep it that way). Interactive picking is built in for macOS and Linux.

## Layout

- `main.go`: flag parsing, command dispatch, usage text, default history file resolution.
- `cmd_*.go`: one file per command.
- `history.go`: parsing, format detection, selections, atomic write, backup.
- `search.go`: extended fuzzy query parsing, matching, and highlight alignment.
- `picker*.go`, `terminal*.go`: picker state, rendering, input, and OS-specific terminal handling. UI/input use the controlling terminal; stdout stays available for command output. Stop the input reader and restore terminal state before returning.
- `completion.go`: zsh/bash/fish completion scripts. Update when adding commands or flags.
- Tests: `history_test.go`, `completion_test.go`, `integration_test.go`.

## Commands

```sh
go test ./...
go vet ./...
go build -o shlog .
```

## Formats

Detected on every read:

| Format | Detected by |
| --- | --- |
| zsh | `: <ts>:<elapsed>;<cmd>`; lines ending in `\` continue the entry (zsh escapes newlines this way); lines without a header are untimestamped entries; command text is unmetafied |
| bash timestamped | `#<unix ts>` (10+ digits) lines; lines until the next marker are one entry |
| bash plain | fallback; backslash-newline continuations are one entry; no timestamps, so time/date selections error |
| fish | `- cmd: <text>` + `  when: <ts>`; multi-line stored as literal `\n` |

Default file: `$HISTFILE` → fish file if `$SHELL` contains fish → `~/.bash_history` if bash → `~/.zsh_history`.

## Invariants

- Every write: copy to `<histfile>.bak` first, then write temp file and rename. Never write in place.
- `del`, `clean`, `undo` confirm unless `-f`. `-o` beats `-s`.
- Entries keep their original lines (`Entry.Raw`) and are written back unchanged. Don't reformat them.
- Color only on a TTY; respect `NO_COLOR` and `TERM=dumb`.

## Commits

Use Conventional Commit messages: `type: description` or `type(scope): description`.

## Releasing

1. Push a `vX.Y.Z` tag. `.github/workflows/release.yml` builds `shlog-{darwin,linux}-{amd64,arm64}` with `main.version=X.Y.Z` and creates the release.
2. Update `Formula/shlog.rb` in `ivalkenburg/homebrew-tap`: version and the four sha256s (`shasum -a 256` of each asset).
