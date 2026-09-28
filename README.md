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

`pick` and `del --pick` also need [fzf](https://github.com/junegunn/fzf).

## Usage

```
shlog [options] <command> [args]
```

| Command | What it does |
| --- | --- |
| `list [<selection>]` | Print entries with index and timestamp |
| `grep <pattern> [<selection>]` | Print entries matching a regex |
| `stats [<selection>]` | Entry count, unique commands and the 10 most used |
| `del <selection>` | Delete entries |
| `del --match <pattern> [--invert]` | Delete entries that match (or don't match) a regex |
| `del --pick [<selection>]` | Choose entries to delete in fzf |
| `clean [--keep-oldest] [<selection>]` | Remove duplicates, keeping the newest (or oldest) copy |
| `pick [--multi] [<selection>]` | Choose entries in fzf and print their commands |
| `undo` | Restore the file from the last backup |
| `completion <shell>` | Print a completion script for zsh, bash or fish |
| `version` | Print the version |

| Option | What it does |
| --- | --- |
| `-f` | Skip the confirmation prompt |
| `-s`, `--dry-run` | Show which entries would be removed, without writing |
| `-o` | Print what the file would contain afterwards, without writing |
| `--histfile <path>` | Use another history file |

A selection narrows a command to part of the history:

| Selection | Entries |
| --- | --- |
| `-N` | The last N (`-100`) |
| `N` | The first N (`100`) |
| `-<duration>` | Added in the last duration (`-1h`, `-1h30m`) |
| `<duration>` | Within that duration of the first timestamped entry (`30m`) |
| `<date>` | On that day, hour, minute or second (`2024-01-15`, `2024-01-15T14`) |
| `<date>..<date>` | In that range, inclusive (`2024-01-01..2024-01-31`) |

Time and date selections need timestamps, so they don't work on a plain bash
history file. Set `HISTTIMEFORMAT` in bash to get them.

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
