<p align="center">
  <img src="assets/shlog-wordmark.png" alt="shlog — a terminal prompt inside a history arrow, beside the shlog wordmark" width="640">
</p>

# shlog

A command-line tool for reading and cleaning up your shell history.

Shell history fills up with typos, duplicates and the odd command you'd rather
not keep, and editing the file by hand is fiddly because every shell stores it
differently. shlog lets you list, search, dedupe and delete entries by count,
time or date, and it backs up the file before every change. It works with zsh,
bash and fish history and detects the format on its own.

## Install

With Homebrew:

```sh
brew install ivalkenburg/tap/shlog
```

With Go:

```sh
go install github.com/ivalkenburg/shlog@latest
```

From source:

```sh
git clone https://github.com/ivalkenburg/shlog.git
cd shlog
go build -o shlog .
```

Interactive picking is built in on macOS and Linux. No additional tools are required.

## Usage

```
shlog [options] <command> [args] [options]
```

| Command | What it does |
| --- | --- |
| `list [<selection>]` | Print entries with index and timestamp |
| `grep <pattern> [<selection>]` | Print entries matching a regex |
| `stats [<selection>]` | Entry count, unique commands and the 10 most used |
| `del <selection>` | Delete entries |
| `del --match <pattern> [--invert]` | Delete entries that match (or don't match) a regex |
| `del --pick [<selection>]` | Interactively choose entries to delete |
| `clean [--keep-oldest] [<selection>]` | Remove duplicates, keeping the newest (or oldest) copy |
| `pick [--multi] [--unique] [--sort <order>] [<selection>]` | Interactively choose entries and print their commands |
| `undo` | Restore the file from the last backup |
| `completion <shell>` | Print a completion script for zsh, bash or fish |
| `version` | Print the version |
| `help` | Print usage |

| Option | What it does |
| --- | --- |
| `-f` | Skip the confirmation prompt |
| `-s`, `--dry-run` | Show which entries would be removed, without writing |
| `-o` | Print what the file would contain afterwards, without writing |
| `--histfile <path>` | Use another history file |
| `-h`, `--help` | Print usage |

Options can appear before or after the command: `shlog clean --dry-run` and
`shlog --dry-run clean` are equivalent. Negative selections such as `-20` and
`-1h` keep their existing meaning. Use `--` to stop option parsing, for example
`shlog grep -- '-f'`. Values following `--match` and `--histfile` are taken
literally, even when they resemble flags.

`-o` takes precedence over `-s` and `--dry-run`.

A selection narrows a command to part of the history:

| Selection | Entries |
| --- | --- |
| `-N` | The last N (`-100`) |
| `N` | The first N (`100`) |
| `-<duration>` | Added in the last duration (`-1h`, `-1h30m`) |
| `<duration>` | Within that duration of the first timestamped entry (`30m`) |
| `<date>` | On that day, hour, minute or second (`2024-01-15`, `2024-01-15T14`) |
| `<date>..<date>` | In that range, inclusive (`2024-01-01..2024-01-31`) |

Dates use local calendar time. Nonexistent times during daylight-saving changes
are rejected; repeated hours, minutes, and seconds include both occurrences.
Durations use elapsed time. Fractional seconds are not accepted in date selections.

Time and date selections need timestamps, so they don't work on a plain bash
history file. Set `HISTTIMEFORMAT` in bash to get them.
Each command accepts at most one selection. `del --match` cannot be combined
with a selection.

### Examples

```sh
shlog -f del -1                  # delete the last entry without asking
shlog del -1h                    # delete everything from the last hour
shlog del --match '^aws '        # delete every aws command
shlog --dry-run clean            # see which duplicates clean would remove
shlog grep docker 2024-01-15     # search one day
shlog stats -168h                # stats for the last week
eval "$(shlog pick)"             # pick a command and run it again
```

### Interactive picker

`pick` and `del --pick` open a full-screen picker with compact command rows and a
scrollable command preview below. Results default to newest-first. Click the
`[ newest first ]` control in the header or press Ctrl-S to cycle through newest,
relevance, and frequency sorting. Start in a specific mode with
`pick --sort newest|relevance|frequency` or `del --pick --sort <order>`.
Relevance ranks the best search alignments first, breaking ties by shorter command
length and then recency. Empty queries and queries containing only exclusions
keep newest-first order in relevance mode. Frequency ranks exact command text by
its occurrence count within the selection, breaking ties by recency.
Changing sorting preserves focus and marked entries.

Use `pick --unique` to show one row per exact command, represented by its latest
occurrence in the selection. Rows show occurrence counts and the latest entry's
index and timestamp; frequency sorting also shows counts without grouping.
`del --pick` always exposes individual occurrences so deleting one does not delete
all copies. Unique mode can be combined with `--multi` and any sort order.

Each row shows the
original history index and timestamp when the terminal is wide enough; the
preview shows the full timestamp. Search covers the complete command, including
multiline content, rather than the displayed index or timestamp.

Type to search, use the arrow keys or Ctrl-P/Ctrl-N to move, and press Enter to
accept. Esc or Ctrl-C cancels. F1 opens the complete search and control reference.
The picker uses the terminal's palette and respects `NO_COLOR`.

With `pick --multi` or `del --pick`, Tab toggles the focused entry and moves down;
Shift-Tab toggles and moves up. Marks persist when a changed query hides entries,
and the selected count includes hidden marks. Enter accepts all marked entries,
or the focused entry when nothing is marked. Selected commands are printed
newest-first, regardless of marking order. A valid query with no matches can
still accept hidden marks; incomplete syntax blocks acceptance.

The mouse wheel scrolls results, or the preview when the pointer is over it.
Click a row to focus it, click its `[ ]` marker to toggle selection, or
double-click a row to accept, like Enter. Alt-Up/Alt-Down also scroll the preview.
The preview wraps long commands and reveals the first matching line with context.
Deletion still asks for confirmation after the picker closes unless `-f` is set.

Search supports ordered fuzzy subsequences and extended expressions:

```text
gco                 fuzzy: git checkout
git main            git AND main, in either order
git main | dev      git AND (main OR dev)
'checkout           exact substring
'git'               exact word
^git                command prefix
main$               command suffix
^pwd$               whole command
!sudo               exclude an exact substring
!^sudo              exclude a prefix
!log$               exclude a suffix
!'gco               exclude a fuzzy subsequence
'git\ checkout      exact substring containing a space
\!sudo              literal !sudo
\|                  literal pipe
\\                  literal backslash
```

OR joins adjacent terms; parentheses are ordinary search characters. Backslash
escapes operators (`!`, `'`, `^`, `$`, `|`, `\`) and whitespace, and is preserved
before ordinary characters. Anchors refer to the beginning/end of the complete
command, including multiline entries. Fuzzy search matches omitted characters;
it does not correct spelling mistakes or transpositions.

Lowercase terms ignore case. A term containing uppercase is case-sensitive, so
`Git main` requires `Git` but allows `MAIN`. Unaccented Latin terms also match
accented letters (`cafe` matches `café`); explicitly accented terms preserve that
distinction. Match quality determines highlighting and relevance ranking; newest-first remains
the default sort order.

The search prompt supports Left/Right, Home/End or Ctrl-A/Ctrl-E,
Backspace/Delete, Ctrl-U to clear, and Ctrl-W to erase the previous word.
Pasted newlines and tabs become spaces, and bracketed paste cannot accidentally
accept a command. Page-Up/Page-Down move through results. Ctrl-Z suspends the
picker; the shell's `fg` resumes it.

Interaction uses the controlling terminal, keeping stdout available for command
substitution and redirection. The picker requires a capable terminal at least
30 columns wide and 10 rows high. Without a controlling terminal, or with
`TERM=dumb`, use `list`, `grep`, or `del --match` for noninteractive work.

### Exit statuses

| Status | Meaning |
| --- | --- |
| `0` | Success, including `clean` finding no duplicates and `-o` producing an unchanged file |
| `1` | No matching entries for `list`, `grep`, `stats`, `pick`, or `del` |
| `2` | Invalid arguments, file/output errors, or unavailable terminal |
| `130` | Picker canceled with Esc/Ctrl-C/SIGINT, or confirmation declined (including end of input) |

An empty picker selection returns `1` without opening the terminal. A query with
no results leaves the picker open so it can be revised or canceled. Canceling
`del --pick`, including with `-o`, produces no output and never writes a file.
With `-o`, successfully producing the resulting file returns `0` even when no
entries would be removed.

### Which file it edits

shlog uses `$HISTFILE` if it is set. Otherwise it picks the fish, bash or zsh
history file based on `$SHELL`, and falls back to `~/.zsh_history`.
`--histfile` overrides all of this.

### Safety

`del`, `clean` and `undo` ask before writing unless you pass `-f`. Before each
write, shlog copies the file to `<histfile>.bak`, which `shlog undo` restores.
Writes go to a temporary file that is then renamed into place, so a crash can't
leave a half-written history.
If the history file changes while you are confirming a deletion or cleanup,
shlog stops and asks you to retry instead of overwriting those new entries.
Running `undo` twice restores the state from before the first undo.

### Shell completions

Homebrew installs them for you. Otherwise:

```sh
shlog completion zsh > "${fpath[1]}/_shlog"
echo 'source <(shlog completion bash)' >> ~/.bashrc
shlog completion fish > ~/.config/fish/completions/shlog.fish
```

## License

MIT. See [LICENSE](LICENSE).

Unicode display tables use Unicode data under the [Unicode License V3](UNICODE-LICENSE).
