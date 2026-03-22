package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var version = "dev"

func main() {
	args := os.Args[1:]

	force := false
	simulate := false
	output := false
	histFile := defaultHistoryFilePath()

	// consume option flags
	for len(args) > 0 {
		switch args[0] {
		case "-f":
			force = true
		case "-s", "--dry-run":
			simulate = true
		case "-o":
			output = true
		case "--histfile":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "error: --histfile requires a path argument")
				os.Exit(1)
			}
			histFile = args[1]
			args = args[1:]
		default:
			goto doneFlags
		}
		args = args[1:]
	}
doneFlags:

	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	switch args[0] {
	case "del":
		runDel(args[1:], force, simulate, output, histFile)
	case "clean":
		runClean(args[1:], force, simulate, output, histFile)
	case "list":
		runList(args[1:], histFile)
	case "grep":
		runGrep(args[1:], histFile)
	case "stats":
		runStats(args[1:], histFile)
	case "undo":
		runUndo(force, simulate, histFile)
	case "pick":
		runPick(args[1:], histFile)
	case "completion":
		runCompletion(args[1:])
	case "version":
		v := version
		if !strings.HasPrefix(v, "v") {
			v = "v" + v
		}
		fmt.Println(v)
	default:
		fmt.Fprintf(os.Stderr, "error: unknown command %q\n\n", args[0])
		printUsage()
		os.Exit(1)
	}
}

// helpers

func loadHistory(histFile string) []*Entry {
	entries, err := ParseHistoryFile(histFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading history file %s: %v\n", histFile, err)
		os.Exit(1)
	}
	return entries
}

func writeHistory(histFile string, entries []*Entry) {
	if err := BackupHistoryFile(histFile); err != nil {
		fmt.Fprintf(os.Stderr, "error creating backup: %v\n", err)
		os.Exit(1)
	}
	if err := WriteHistoryFile(histFile, entries); err != nil {
		fmt.Fprintf(os.Stderr, "error writing history file: %v\n", err)
		os.Exit(1)
	}
}

func confirm(force bool, prompt string) bool {
	if force {
		return true
	}
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "y" && answer != "yes" {
		fmt.Println("Aborted.")
		return false
	}
	return true
}

func printEntries(entries []*Entry) {
	for _, e := range entries {
		fmt.Println(e.String())
	}
}

// printIndexed prints a subset of entries showing their index in the full history.
// If highlight is non-nil and color output is enabled, matched text is highlighted.
func printIndexed(all, toShow []*Entry, highlight *regexp.Regexp) {
	indexOf := make(map[*Entry]int, len(all))
	for i, e := range all {
		indexOf[e] = i + 1 // 1-based
	}
	width := len(fmt.Sprintf("%d", len(all)))
	for _, e := range toShow {
		var ts string
		if e.Timestamp == 0 {
			ts = "—"
		} else {
			ts = e.Time().Format(time.DateTime)
		}
		cmd := CommandText(e)
		if idx := strings.Index(cmd, "\n"); idx != -1 {
			cmd = cmd[:idx] + " ↵"
		}
		if colorEnabled {
			ts = colorDim(ts)
			if highlight != nil {
				cmd = colorHighlightMatches(cmd, highlight)
			}
		}
		fmt.Printf("%*d  %s  %s\n", width, indexOf[e], ts, cmd)
	}
}

func defaultHistoryFilePath() string {
	// $HISTFILE is respected by both zsh and bash and is the most authoritative
	// signal when the user has exported it from their shell config.
	if p := os.Getenv("HISTFILE"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}
	// $SHELL reflects the login shell set by the OS, so it works even when
	// the binary is invoked from a different shell session.
	shell := os.Getenv("SHELL")
	if strings.Contains(shell, "fish") {
		return filepath.Join(home, ".local", "share", "fish", "fish_history")
	}
	if strings.Contains(shell, "bash") {
		return filepath.Join(home, ".bash_history")
	}
	return filepath.Join(home, ".zsh_history")
}

// color support

var colorEnabled = isTTY()

func isTTY() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if term := os.Getenv("TERM"); term == "dumb" || term == "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

const (
	ansiReset = "\033[0m"
	ansiDim   = "\033[2m"
	ansiBold  = "\033[1m"
	ansiGreen = "\033[32m"
)

func colorDim(s string) string {
	return ansiDim + s + ansiReset
}

func colorHighlightMatches(s string, re *regexp.Regexp) string {
	return re.ReplaceAllStringFunc(s, func(m string) string {
		return ansiBold + ansiGreen + m + ansiReset
	})
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `Usage: shlog [options] <command> [args]

Commands:
  del <selection>                 delete entries matching selection
  del --match <pattern>           delete entries whose command matches a regex
  del --match <pattern> --invert  delete entries that do NOT match the regex
  del --pick [<selection>]        interactively pick entries to delete (requires fzf)
  clean [<selection>]             remove duplicates (optionally scoped to selection)
  clean --keep-oldest [<sel>]     remove duplicates, keeping first occurrence instead of last
  list [<selection>]              print entries with index and timestamp
  grep <pattern> [<selection>]    print matching entries with index and timestamp
  stats [<selection>]             show entry count, unique commands, and top 10
  pick [<selection>]              interactively pick entries and print their commands (requires fzf)
  pick --multi [<selection>]      pick multiple entries
  undo                            restore history from the last backup
  completion <shell>              print shell completion script (zsh, bash)

Options:
  -f              force: never ask for confirmation
  -s, --dry-run   simulate: only output what would be changed
  -o              output: print what the history file would look like (no write, no confirmation)
  --histfile <p>  use <p> instead of ~/.zsh_history

Selections (for del, clean, list, grep, pick, and stats):
  -N              last N entries (e.g. -1, -100)
  N               first N entries (e.g. 1, 100)
  -<duration>     entries from last N time (e.g. -1h, -30m, -1h30m)
  <duration>      entries within N time from first entry (e.g. 1h, 30m)
  <date>..<date>  entries in date/datetime range (e.g. 2024-01-01..2024-01-31)
  <date>          all entries within that unit (e.g. 2024-01-15, 2024-01-15T14)
                  formats: YYYY-MM-DD, YYYY-MM-DDTHH, YYYY-MM-DDTHH:MM, YYYY-MM-DDTHH:MM:SS

Examples:
  shlog -f del -1                        delete last entry without confirmation
  shlog -s del -100                      show last 100 entries that would be deleted
  shlog del 100                          delete first 100 entries
  shlog del -1h                          delete entries from the last hour
  shlog del 2024-01-01..2024-01-31       delete entries in January 2024
  shlog del --match "^aws "              delete entries starting with 'aws'
  shlog del --match "^aws " --invert     delete all entries except those starting with 'aws'
  shlog del --pick                       interactively select entries to delete
  shlog del --pick -1h                   pick from entries in the last hour
  shlog clean                            remove all duplicate entries (keeps most recent)
  shlog clean --keep-oldest              remove duplicates, keeping oldest occurrence
  shlog clean -1h                        remove duplicates only within the last hour
  shlog list                             list all entries with index and timestamp
  shlog list -20                         list last 20 entries
  shlog list 2024-06-01..2024-06-30      list entries from June 2024
  shlog grep "docker"                    show entries matching 'docker'
  shlog grep "docker" -1h                show 'docker' entries from the last hour
  shlog grep "^aws (s3|ec2)"             show entries matching pattern
  shlog stats                            show overall history statistics
  shlog stats -7d                        show statistics for the last 7 days
  shlog pick                             interactively pick an entry and print its command
  shlog pick --multi                     pick multiple entries
  shlog pick -1h                         pick from entries in the last hour
  shlog undo                             restore history from last backup
  shlog --histfile /tmp/hist list        inspect a custom history file`)
}
