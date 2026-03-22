# shlog

A CLI tool to inspect and manage shell history files. Supports zsh extended history (`~/.zsh_history`), bash history (`~/.bash_history`), and fish history (`~/.local/share/fish/fish_history`), with auto-detection of the file format.

## Installation

```sh
go build -o shlog .
```

Move the binary somewhere on your `$PATH`, e.g. `/usr/local/bin/shlog`.

## Usage

```
shlog [options] <command> [args]
```

### Commands

| Command | Description |
|---|---|
| `del <selection>` | Delete entries matching a selection |
| `del --match <pattern>` | Delete entries whose command matches a regex |
| `del --match <pattern> --invert` | Delete entries that do **not** match the regex |
| `clean [<selection>]` | Remove duplicate entries, keeping the most recent occurrence (optionally scoped to a selection) |
| `clean --keep-oldest [<selection>]` | Remove duplicate entries, keeping the **oldest** (first) occurrence instead |
| `list [<selection>]` | Print entries with index and timestamp |
| `grep <pattern> [<selection>]` | Print matching entries with index and timestamp (read-only) |
| `stats [<selection>]` | Show entry count, unique commands, and top 10 most-used commands |
| `pick [<selection>]` | Interactively pick an entry and print its command (requires `fzf`) |
| `pick --multi [<selection>]` | Pick multiple entries |
| `del --pick [<selection>]` | Interactively pick entries to delete (requires `fzf`) |
| `undo` | Restore the history file from the last backup |
| `completion <shell>` | Print a shell completion script (`zsh`, `bash`, or `fish`) |
| `version` | Print the current version |

### Options

| Flag | Description |
|---|---|
| `-f` | Force — skip confirmation prompt |
| `-s`, `--dry-run` | Simulate — show what would change without writing |
| `-o` | Output — print the resulting file content without writing or confirming |
| `--histfile <path>` | Use a custom history file instead of the default |

By default `del`, `clean`, and `undo` ask for confirmation before modifying the history file. Before any write, a backup is automatically created at `<histfile>.bak`.

`-o` and `-s` are mutually exclusive in effect: `-o` takes precedence and shows the remaining entries (what the file would contain), while `-s` shows the entries that would be removed.

### Selections

Selections scope which entries a command operates on. They work with `del`, `clean`, `list`, `grep`, and `stats`.

| Selection | Description |
|---|---|
| `-N` | Last N entries (e.g. `-1`, `-100`) |
| `N` | First N entries (e.g. `1`, `100`) |
| `-<duration>` | Entries added within the last duration (e.g. `-1h`, `-30m`, `-1h30m`) |
| `<duration>` | Entries within duration from the first entry (e.g. `1h`, `30m`) |
| `<date>` | All entries within that date/datetime unit (e.g. `2024-01-15`, `2024-01-15T14`) |
| `<date>..<date>` | Entries within a date/datetime range, inclusive (e.g. `2024-01-01..2024-01-31`) |

**Durations** use Go's format: `h` hours, `m` minutes, `s` seconds, combinable (`1h30m`).

**Dates** support four formats: `YYYY-MM-DD`, `YYYY-MM-DDTHH`, `YYYY-MM-DDTHH:MM`, `YYYY-MM-DDTHH:MM:SS`. The precision determines the matched window — a bare date covers the full day, `T14` covers the full hour, `T14:30` covers the full minute, and so on. Both sides of a `..` range follow the same rule.

## Examples

### Deleting entries

```sh
# Delete the last entry, no confirmation
shlog -f del -1

# Delete the last 100 entries (ask for confirmation)
shlog del -100

# Delete the first 100 entries
shlog del 100

# Delete all entries added in the last hour
shlog del -1h

# Delete all entries on a specific day
shlog del 2024-01-15

# Delete all entries in January 2024
shlog del 2024-01-01..2024-01-31

# Preview what the history file would look like after deleting the last 50 entries
shlog -o del -50

# Show which entries would be deleted (dry run; --dry-run is an alias for -s)
shlog --dry-run del -1h

# Delete entries whose command matches a regex
shlog del --match "^aws "

# Delete all entries except those matching a pattern (keep only git commands)
shlog del --match "^git " --invert
```

### Deduplication

```sh
# Remove all duplicate entries (keeps most recent occurrence of each command)
shlog clean

# Remove duplicates, keeping the oldest (first) occurrence instead
shlog clean --keep-oldest

# Remove duplicates only within the last hour, leaving older entries untouched
shlog clean -1h

# Remove duplicates only on a specific day
shlog clean 2024-01-15

# Show which duplicates would be removed (dry run)
shlog --dry-run clean
```

### Listing entries

```sh
# List all entries with index and timestamp
shlog list

# List the last 20 entries
shlog list -20

# List entries from the last hour
shlog list -1h

# List entries for a specific day
shlog list 2024-06-15

# List entries for a specific hour
shlog list 2024-06-15T14

# List entries for a specific minute
shlog list 2024-06-15T14:30

# List entries over a date range
shlog list 2024-06-01..2024-06-30
```

### Searching

```sh
# Find all entries matching a pattern
shlog grep "docker"
shlog grep "^aws (s3|ec2)"

# Search only within the last hour
shlog grep "docker" -1h

# Search only on a specific day
shlog grep "kubectl" 2024-01-15

# Search within a date range
shlog grep "kubectl" 2024-01-01..2024-03-31
```

### Statistics

```sh
# Show overall statistics: totals, date range, top 10 commands
shlog stats

# Show statistics for entries from the last 7 days
shlog stats -168h

# Show statistics for a specific month
shlog stats 2024-01-01..2024-01-31

# Show statistics for a single day
shlog stats 2024-06-15
```

### Interactive picking (fzf)

These commands require [fzf](https://github.com/junegunn/fzf) to be installed and on your `$PATH`.

```sh
# Interactively pick one entry and print its command (useful for re-running)
shlog pick

# Pick multiple entries and print each command on its own line
shlog pick --multi

# Pick from entries in the last hour only
shlog pick -1h

# Interactively select entries to delete
shlog del --pick

# Select entries to delete from the last hour
shlog del --pick -1h

# Re-run a chosen command directly
eval "$(shlog pick)"
```

### Undo and custom files

```sh
# Restore the history file from the last backup
shlog undo

# Preview what undo would restore (dry run)
shlog -s undo

# Inspect a custom history file
shlog --histfile /tmp/other_history list
```

### Shell completions

```sh
# Zsh — write to a directory in $fpath, then reload
shlog completion zsh > "${fpath[1]}/_shlog"

# Bash — source from ~/.bashrc
echo 'source <(shlog completion bash)' >> ~/.bashrc

# Fish — write to the completions directory
shlog completion fish > ~/.config/fish/completions/shlog.fish
```

## Output

When attached to a terminal, `list` and `grep` use color output: timestamps are dimmed and matched patterns in `grep` results are highlighted in bold. Set `NO_COLOR=1` or `TERM=dumb` to disable.

## Safety

- **Atomic writes** — changes are written to a temp file and renamed into place, so a crash mid-write cannot corrupt your history file.
- **Auto-backup** — before every write, the history file is copied to `<histfile>.bak` (e.g. `~/.zsh_history.bak`).
- **Confirmation prompt** — by default, `del`, `clean`, and `undo` ask for confirmation before making changes. Use `-f` to skip.
- **Undo** — `shlog undo` restores from the `.bak` file created by the last write operation.

## History file format

`shlog` auto-detects the format of the history file on every read:

| Format | Detected by | Default file |
|---|---|---|
| **zsh extended** | Lines matching `: <ts>:<elapsed>;<cmd>` | `~/.zsh_history` |
| **bash timestamped** | Lines matching `#<unix_timestamp>` (10+ digits) | `~/.bash_history` |
| **bash plain** | Everything else (fallback) | `~/.bash_history` |
| **fish** | Lines matching `- cmd: <text>` | `~/.local/share/fish/fish_history` |

### zsh extended history

Requires `setopt EXTENDED_HISTORY` (enabled by default in most zsh configs). Each entry looks like:

```
: <unix_timestamp>:<elapsed>;<command>
```

Multi-line commands are stored as continuation lines and treated as a single entry.

### bash timestamped history

Written when `HISTTIMEFORMAT` is set and `history -w` saves the file. Each entry looks like:

```
#<unix_timestamp>
<command>
```

Multi-line commands (via `lithist`) are supported — all lines between two timestamp markers are grouped as one entry.

### bash plain history

The default `~/.bash_history` format with no timestamps — one command per line. When both `cmdhist` and `lithist` are enabled, bash stores multi-line commands with backslash-newline continuations; `shlog` detects and groups these into a single entry.

**Timestamp-based selections** (`-1h`, `2024-01-15`, date ranges) are not available for plain bash history files — they return an error. Integer selections (`-10`, `100`) and pattern-based operations (`grep`, `del --match`, `clean`) work regardless of format.

### fish history

Fish stores history in `~/.local/share/fish/fish_history` using a YAML-like format. Each entry is a `- cmd: <text>` line followed by a `  when: <unix_timestamp>` line. Multi-line commands are stored with literal `\n` escape sequences on a single line and are automatically expanded when displayed.

## Default history file

The default history file is resolved in this order:

1. `$HISTFILE` if set and non-empty (honoured by both zsh and bash)
2. `~/.local/share/fish/fish_history` if `$SHELL` contains `fish`
3. `~/.bash_history` if `$SHELL` contains `bash`
4. `~/.zsh_history` otherwise

Override at any time with `--histfile <path>`.
