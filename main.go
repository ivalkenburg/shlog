package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var version = "dev"

func main() {
	opts, err := parseCLI(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(exitError)
	}
	if opts.help {
		printOutput("%s\n", usage)
		return
	}
	args := opts.args
	force, simulate, output, histFile := opts.force, opts.simulate, opts.output, opts.histFile

	if len(args) == 0 {
		printUsage()
		os.Exit(exitError)
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
		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "error: undo does not accept arguments")
			os.Exit(exitError)
		}
		runUndo(force, simulate, output, histFile)
	case "pick":
		runPick(args[1:], histFile)
	case "completion":
		runCompletion(args[1:])
	case "help":
		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "error: help does not accept arguments")
			os.Exit(exitError)
		}
		printOutput("%s\n", usage)
	case "version":
		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "error: version does not accept arguments")
			os.Exit(exitError)
		}
		printOutput("%s\n", version)
	default:
		fmt.Fprintf(os.Stderr, "error: unknown command %q\n\n", args[0])
		printUsage()
		os.Exit(exitError)
	}
}

func loadHistory(histFile string) []*Entry {
	entries, _ := loadHistorySnapshot(histFile)
	return entries
}

func loadHistorySnapshot(histFile string) ([]*Entry, []byte) {
	data, err := os.ReadFile(histFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading history file %s: %v\n", histFile, err)
		os.Exit(exitError)
	}
	return parseHistoryData(data), data
}

func writeHistory(histFile string, original []byte, entries []*Entry) {
	if err := ReplaceHistoryFile(histFile, original, entries); err != nil {
		fmt.Fprintf(os.Stderr, "error updating history file: %v\n", err)
		os.Exit(exitError)
	}
}

func confirm(force bool, prompt string) bool {
	if force {
		return true
	}
	printOutput("%s", prompt)
	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		fmt.Fprintf(os.Stderr, "error reading confirmation: %v\n", err)
		os.Exit(exitError)
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "y" && answer != "yes" {
		printOutput("Aborted.\n")
		return false
	}
	return true
}

func printOutput(format string, args ...any) {
	if _, err := fmt.Fprintf(os.Stdout, format, args...); err != nil {
		fmt.Fprintf(os.Stderr, "error writing output: %v\n", err)
		os.Exit(exitError)
	}
}

func printEntries(entries []*Entry) {
	w := bufio.NewWriter(os.Stdout)
	err := writeEntries(w, entries)
	if err == nil {
		err = w.Flush()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error writing output: %v\n", err)
		os.Exit(exitError)
	}
}

func selectOptionalEntries(entries []*Entry, args []string) ([]*Entry, error) {
	if len(args) > 1 {
		return nil, fmt.Errorf("expected at most one selection, got %d", len(args))
	}
	if len(args) == 0 {
		return entries, nil
	}
	return SelectEntries(entries, args[0])
}

// printIndexed prints a subset of entries showing their index in the full history.
// If highlight is non-nil and color output is enabled, matched text is highlighted.
func printIndexed(all, toShow []*Entry, highlight *regexp.Regexp) {
	w := bufio.NewWriter(os.Stdout)
	err := writeIndexed(w, all, toShow, highlight, colorEnabled)
	if err == nil {
		err = w.Flush()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error writing output: %v\n", err)
		os.Exit(exitError)
	}
}

// writeIndexed formats indexed output for list and grep.
func writeIndexed(w io.Writer, all, toShow []*Entry, highlight *regexp.Regexp, color bool) error {
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
		cmd := commandPreview(CommandText(e))
		if color {
			ts = colorDim(ts)
			if highlight != nil {
				cmd = colorHighlightMatches(cmd, highlight)
			}
		}
		if _, err := fmt.Fprintf(w, "%*d  %s  %s\n", width, indexOf[e], ts, cmd); err != nil {
			return err
		}
	}
	return nil
}

func commandPreview(cmd string) string {
	first, _, multiline := strings.Cut(cmd, "\n")
	// Human-facing previews share the picker's control/bidi escaping. Raw
	// history and selected-command output are written through separate paths.
	preview := first
	if strings.IndexFunc(first, func(r rune) bool {
		return unicode.IsControl(r) || isBidiControl(r) || r == utf8.RuneError
	}) >= 0 {
		glyphs := pickerGlyphs(first, nil)
		preview = renderPickerGlyphs(glyphs, glyphWidth(glyphs), false, "")
	}
	if multiline {
		preview += " ↵"
	}
	return preview
}

func defaultHistoryFilePath() string {
	// $HISTFILE is respected by both zsh and bash and is the most authoritative
	// signal when the user has exported it from their shell config.
	if p := os.Getenv("HISTFILE"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
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
	return isTerminal(int(os.Stdout.Fd()))
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
		if m == "" {
			return m // patterns like `^` or `x*` would otherwise wrap every gap
		}
		return ansiBold + ansiGreen + m + ansiReset
	})
}

func printUsage() {
	fmt.Fprintln(os.Stderr, usage)
}

const usage = `Usage: shlog [options] <command> [args] [options]

Commands:
  del <selection>                 delete entries matching selection
  del --match <pattern>           delete entries whose command matches a regex
  del --match <pattern> --invert  delete entries that do NOT match the regex
  del --pick [<selection>]        interactively pick entries to delete
  clean [<selection>]             remove duplicates (optionally scoped to selection)
  clean --keep-oldest [<sel>]     remove duplicates, keeping first occurrence instead of last
  list [<selection>]              print entries with index and timestamp
  grep <pattern> [<selection>]    print matching entries with index and timestamp
  stats [<selection>]             show entry count, unique commands, and top 10
  pick [<selection>]              interactively pick entries and print their commands
  pick --multi [<selection>]      pick multiple entries
  pick --unique [<selection>]     group identical commands, keeping the latest row
  pick --sort <order> [<sel>]     order by newest (default), relevance, or frequency
  del --pick --sort <order>       choose individual entries in that order
  undo                            restore history from the last backup
  completion <shell>              print shell completion script (zsh, bash, fish)
  version                         print the current version
  help                            print this help

Options (before or after the command; -- ends option parsing):
  -f              force: never ask for confirmation
  -s, --dry-run   simulate: only output what would be changed
  -o              output: print what the history file would look like (no write, no confirmation)
  --histfile <p>  use <p> instead of the default history file
  -h, --help      print this help

Interactive picker (macOS/Linux):
  Full-screen fuzzy search with multiline preview and mouse support.
  Click [ newest first ] or press Ctrl-S to cycle result sorting.
  Tab marks entries in multi-select mode; output remains newest-first.
  Enter accepts; Esc/Ctrl-C cancels; F1 shows syntax and controls.
  Uses the controlling terminal, including with redirected stdout.

Exit statuses:
  0 success (including clean with no duplicates and -o with no changes)
  1 no matching entries
  2 invalid arguments, I/O failures, or terminal errors
  130 canceled picker or declined confirmation

Selections (for del, clean, list, grep, pick, and stats):
  -N              last N entries (e.g. -1, -100)
  N               first N entries (e.g. 1, 100)
  -<duration>     entries from last N time (e.g. -1h, -30m, -1h30m)
  <duration>      entries within N time from first timestamped entry (e.g. 1h, 30m)
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
  shlog stats -168h                      show statistics for the last 7 days
  shlog pick                             interactively pick an entry and print its command
  shlog pick --multi                     pick multiple entries
  shlog pick -1h                         pick from entries in the last hour
  shlog undo                             restore history from last backup
  shlog --histfile /tmp/hist list        inspect a custom history file`
