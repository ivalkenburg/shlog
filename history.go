package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var entryHeaderRe = regexp.MustCompile(`^: (\d+):(\d+);`)

// bashTimestampRe matches the `#<unix_timestamp>` lines written by bash when
// HISTTIMEFORMAT is set. Requires at least 10 digits to avoid false-positives
// on short shell comments like `#123`.
var bashTimestampRe = regexp.MustCompile(`^#(\d{10,})$`)

// fishCmdRe matches the `- cmd: <command>` lines in fish history files.
var fishCmdRe = regexp.MustCompile(`^- cmd: `)

type histFormat int

const (
	formatZsh             histFormat = iota // `: <ts>:<elapsed>;<cmd>`
	formatBashTimestamped                   // `#<unix_ts>` line before each command
	formatBashPlain                         // one command per line, no timestamps
	formatFish                              // `- cmd: <cmd>` + `  when: <ts>` (YAML-like)
)

// Entry represents a single logical entry in a zsh extended history file.
// Multi-line commands span multiple Raw lines.
type Entry struct {
	Timestamp int64
	Elapsed   int64
	Raw       []string // original lines, preserving exact content
}

func (e *Entry) Time() time.Time {
	return time.Unix(e.Timestamp, 0)
}

// String returns the entry as it appears in the history file.
func (e *Entry) String() string {
	return strings.Join(e.Raw, "\n")
}

// ParseHistoryFile reads a history file, auto-detects its format, and returns
// all entries. Supported formats: zsh extended history, bash with HISTTIMEFORMAT,
// and plain bash (one command per line, assumes cmdhist is enabled).
func ParseHistoryFile(path string) ([]*Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fmt := detectFormat(bytes.NewReader(data))
	return parseHistory(bytes.NewReader(data), fmt), nil
}

// detectFormat scans r and returns the history format based on the first
// recognisable line. Returns formatBashPlain if no marker is found.
func detectFormat(r io.Reader) histFormat {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if entryHeaderRe.MatchString(line) {
			return formatZsh
		}
		if bashTimestampRe.MatchString(line) {
			return formatBashTimestamped
		}
		if fishCmdRe.MatchString(line) {
			return formatFish
		}
	}
	return formatBashPlain
}

func parseHistory(r io.Reader, f histFormat) []*Entry {
	switch f {
	case formatZsh:
		return parseZshHistory(r)
	case formatBashTimestamped:
		return parseBashTimestamped(r)
	case formatFish:
		return parseFishHistory(r)
	default:
		return parseBashPlain(r)
	}
}

func parseZshHistory(r io.Reader) []*Entry {
	var entries []*Entry
	var current *Entry

	scanner := bufio.NewScanner(r)
	// large buffer for very long lines (e.g. big curl commands)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if m := entryHeaderRe.FindStringSubmatch(line); m != nil {
			ts, _ := strconv.ParseInt(m[1], 10, 64)
			elapsed, _ := strconv.ParseInt(m[2], 10, 64)
			current = &Entry{
				Timestamp: ts,
				Elapsed:   elapsed,
				Raw:       []string{line},
			}
			entries = append(entries, current)
		} else if current != nil {
			current.Raw = append(current.Raw, line)
		}
		// lines before first entry are silently ignored
	}

	return entries
}

// parseBashTimestamped parses a bash history file written with HISTTIMEFORMAT
// set. Each entry begins with a `#<unix_timestamp>` line followed by one or
// more command lines (multi-line commands are supported via lithist).
// Lines appearing before the first timestamp line are ignored.
func parseBashTimestamped(r io.Reader) []*Entry {
	var entries []*Entry
	var current *Entry

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if m := bashTimestampRe.FindStringSubmatch(line); m != nil {
			ts, _ := strconv.ParseInt(m[1], 10, 64)
			current = &Entry{Timestamp: ts, Raw: []string{line}}
			entries = append(entries, current)
		} else if current != nil {
			current.Raw = append(current.Raw, line)
		}
	}
	return entries
}

// parseFishHistory parses a fish history file (~/.local/share/fish/fish_history).
// The format is YAML-like: each entry starts with `- cmd: <text>` followed by
// `  when: <unix_timestamp>`. Multi-line commands are stored with literal `\n`
// escape sequences on a single line. All lines until the next `- cmd: ` marker
// belong to the current entry and are preserved verbatim in Raw for round-tripping.
func parseFishHistory(r io.Reader) []*Entry {
	var entries []*Entry
	var current *Entry

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if fishCmdRe.MatchString(line) {
			current = &Entry{Raw: []string{line}}
			entries = append(entries, current)
		} else if current != nil {
			current.Raw = append(current.Raw, line)
			if v, ok := strings.CutPrefix(line, "  when: "); ok {
				ts, _ := strconv.ParseInt(v, 10, 64)
				current.Timestamp = ts
			}
		}
	}
	return entries
}

// parseBashPlain parses a plain bash history file (no HISTTIMEFORMAT).
// Each non-empty line is treated as a single entry with no timestamp.
//
// When both cmdhist and lithist are set, bash stores multi-line commands with
// backslash-newline continuation: all lines except the last end with '\'.
// We detect this and group consecutive continuation lines into a single entry.
//
// Note: a command that genuinely ends with '\' (e.g. a sed expression) is
// indistinguishable from a continuation marker — this is an inherent ambiguity
// in the plain bash history format (bash itself has the same limitation).
func parseBashPlain(r io.Reader) []*Entry {
	var entries []*Entry
	var current *Entry

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			// Blank lines are not part of any command; reset continuation state.
			current = nil
			continue
		}
		if current != nil && strings.HasSuffix(current.Raw[len(current.Raw)-1], `\`) {
			current.Raw = append(current.Raw, line)
		} else {
			current = &Entry{Raw: []string{line}}
			entries = append(entries, current)
		}
	}
	return entries
}

// WriteHistoryFile writes entries to path atomically: it writes to a temp file
// in the same directory and renames it over the target so a crash mid-write
// cannot corrupt the history file.
func WriteHistoryFile(path string, entries []*Entry) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".hist_tmp_*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	w := bufio.NewWriter(tmp)
	for _, e := range entries {
		for _, line := range e.Raw {
			if _, err := fmt.Fprintln(w, line); err != nil {
				tmp.Close()
				os.Remove(tmpName)
				return err
			}
		}
	}
	if err := w.Flush(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

// BackupHistoryFile copies path to path+".bak". It is a no-op if path does
// not exist yet.
func BackupHistoryFile(path string) error {
	src, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer src.Close()

	dst, err := os.Create(path + ".bak")
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}

// RestoreBackup atomically copies path+".bak" back over path.
func RestoreBackup(path string) error {
	bak := path + ".bak"
	src, err := os.Open(bak)
	if err != nil {
		return err
	}
	defer src.Close()

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".hist_tmp_*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	if _, err := io.Copy(tmp, src); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

// CommandText returns the command text of an entry, stripping any format-specific
// header lines (zsh `': ts:elapsed;'` prefix or bash `#timestamp` line).
func CommandText(e *Entry) string {
	if len(e.Raw) == 0 {
		return ""
	}
	first := e.Raw[0]

	// zsh extended history: strip `: <ts>:<elapsed>;` prefix from first line
	if entryHeaderRe.MatchString(first) {
		first = entryHeaderRe.ReplaceAllString(first, "")
		if len(e.Raw) == 1 {
			return first
		}
		parts := make([]string, len(e.Raw))
		parts[0] = first
		copy(parts[1:], e.Raw[1:])
		return strings.Join(parts, "\n")
	}

	// bash timestamped: first line is `#<ts>`, command starts at Raw[1]
	if bashTimestampRe.MatchString(first) {
		if len(e.Raw) < 2 {
			return ""
		}
		return strings.Join(e.Raw[1:], "\n")
	}

	// fish: `- cmd: <text>` with literal \n escape sequences for multi-line commands
	if cmd, ok := strings.CutPrefix(first, "- cmd: "); ok {
		return strings.ReplaceAll(cmd, `\n`, "\n")
	}

	// bash plain: the line itself is the command
	return strings.Join(e.Raw, "\n")
}

// MatchEntries returns entries whose command text matches the given regex pattern.
func MatchEntries(entries []*Entry, pattern string) ([]*Entry, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid pattern %q: %v", pattern, err)
	}
	return MatchEntriesRe(entries, re), nil
}

// MatchEntriesRe returns entries whose command text matches the compiled regex.
func MatchEntriesRe(entries []*Entry, re *regexp.Regexp) []*Entry {
	var result []*Entry
	for _, e := range entries {
		if re.MatchString(CommandText(e)) {
			result = append(result, e)
		}
	}
	return result
}

// InvertEntries returns entries from all that are not in subset,
// preserving the original order. subset is matched by pointer identity.
func InvertEntries(all, subset []*Entry) []*Entry {
	subSet := make(map[*Entry]bool, len(subset))
	for _, e := range subset {
		subSet[e] = true
	}
	result := make([]*Entry, 0, len(all)-len(subset))
	for _, e := range all {
		if !subSet[e] {
			result = append(result, e)
		}
	}
	return result
}

// DeduplicateEntries removes duplicate commands. When keepOldest is false
// (the default), the most recent occurrence of each command is kept and earlier
// duplicates are removed. When keepOldest is true, the first (oldest)
// occurrence is kept instead. It returns the entries to keep and the duplicate
// entries that were removed, both in their original order.
func DeduplicateEntries(entries []*Entry, keepOldest bool) (keep []*Entry, removed []*Entry) {
	seen := make(map[string]bool, len(entries))
	isDup := make([]bool, len(entries))
	if keepOldest {
		// walk forward: first occurrence wins
		for i, e := range entries {
			cmd := CommandText(e)
			if seen[cmd] {
				isDup[i] = true
			} else {
				seen[cmd] = true
			}
		}
	} else {
		// walk backwards: last (most recent) occurrence wins
		for i := len(entries) - 1; i >= 0; i-- {
			cmd := CommandText(entries[i])
			if seen[cmd] {
				isDup[i] = true
			} else {
				seen[cmd] = true
			}
		}
	}
	for i, e := range entries {
		if isDup[i] {
			removed = append(removed, e)
		} else {
			keep = append(keep, e)
		}
	}
	return keep, removed
}

// CommandCount holds a command string and the number of times it appears.
type CommandCount struct {
	Command string
	Count   int
}

// Stats holds summary statistics for a set of history entries.
type Stats struct {
	Total  int
	Unique int
	TopN   []CommandCount
}

// ComputeStats returns total entries, unique command count, and the top topN
// most-frequent commands for the given entries.
func ComputeStats(entries []*Entry, topN int) Stats {
	counts := make(map[string]int, len(entries))
	for _, e := range entries {
		counts[CommandText(e)]++
	}
	all := make([]CommandCount, 0, len(counts))
	for cmd, n := range counts {
		all = append(all, CommandCount{cmd, n})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Count != all[j].Count {
			return all[i].Count > all[j].Count
		}
		return all[i].Command < all[j].Command
	})
	if topN > len(all) {
		topN = len(all)
	}
	return Stats{Total: len(entries), Unique: len(counts), TopN: all[:topN]}
}

// hasTimestamps reports whether the entries carry timestamp information.
// Plain bash history files have Timestamp == 0 for all entries.
func hasTimestamps(entries []*Entry) bool {
	return len(entries) > 0 && entries[0].Timestamp != 0
}

const errNoTimestamps = "selection %q requires timestamps, but no timestamps were found in this history file"

// SelectEntries returns the entries matched by the selection string.
//
// Selection forms:
//
//	-N                  last N entries (negative integer)
//	N                   first N entries (positive integer)
//	-<duration>         entries whose timestamp >= now - duration  (e.g. -1h, -30m)
//	<duration>          entries within duration from the first entry (e.g. 1h, 30m)
//	<date>..<date>      entries within the date/datetime range (inclusive)
//	                    dates: YYYY-MM-DD or YYYY-MM-DDTHH:MM:SS
func SelectEntries(entries []*Entry, selection string) ([]*Entry, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	// Try date range: from..to
	if idx := strings.Index(selection, ".."); idx >= 0 {
		fromStr := selection[:idx]
		toStr := selection[idx+2:]
		from, to, err := parseDateRange(fromStr, toStr)
		if err != nil {
			return nil, err
		}
		if !hasTimestamps(entries) {
			return nil, fmt.Errorf(errNoTimestamps, selection)
		}
		var result []*Entry
		for _, e := range entries {
			t := e.Time()
			if !t.Before(from) && !t.After(to) {
				result = append(result, e)
			}
		}
		return result, nil
	}

	negative := strings.HasPrefix(selection, "-")
	raw := selection
	if negative {
		raw = selection[1:]
	}

	// Try integer first
	if n, err := strconv.Atoi(raw); err == nil {
		if n <= 0 {
			return nil, fmt.Errorf("selection integer must be positive, got %d", n)
		}
		if negative {
			// last N entries
			start := max(len(entries)-n, 0)
			return entries[start:], nil
		}
		// first N entries
		end := min(n, len(entries))
		return entries[:end], nil
	}

	// Try duration
	dur, err := time.ParseDuration(raw)
	if err != nil {
		// Try single date/datetime (only without leading "-")
		if !negative {
			if pd, perr := parseDateTime(selection); perr == nil {
				if !hasTimestamps(entries) {
					return nil, fmt.Errorf(errNoTimestamps, selection)
				}
				to := pd.t.Add(pd.gran - time.Nanosecond)
				var result []*Entry
				for _, e := range entries {
					t := e.Time()
					if !t.Before(pd.t) && !t.After(to) {
						result = append(result, e)
					}
				}
				return result, nil
			}
		}
		return nil, fmt.Errorf("invalid selection %q: not a valid integer, duration, date, or date range (%v)", selection, err)
	}
	if dur <= 0 {
		return nil, fmt.Errorf("duration must be positive, got %v", dur)
	}

	if !hasTimestamps(entries) {
		return nil, fmt.Errorf(errNoTimestamps, selection)
	}

	if negative {
		// entries added within the last <dur>
		cutoff := time.Now().Add(-dur)
		var result []*Entry
		for _, e := range entries {
			if !e.Time().Before(cutoff) {
				result = append(result, e)
			}
		}
		return result, nil
	}

	// entries within <dur> starting from the first entry's timestamp
	start := entries[0].Time()
	cutoff := start.Add(dur)
	var result []*Entry
	for _, e := range entries {
		if !e.Time().After(cutoff) {
			result = append(result, e)
		}
	}
	return result, nil
}

// parsedDate holds a parsed date/datetime and the granularity of the smallest
// unit present (second, minute, hour, or day).
type parsedDate struct {
	t    time.Time
	gran time.Duration // span of the unit: second=1s, minute=1m, hour=1h, day=24h
}

var dateFormats = []struct {
	layout string
	gran   time.Duration
}{
	{"2006-01-02T15:04:05", time.Second},
	{"2006-01-02T15:04", time.Minute},
	{"2006-01-02T15", time.Hour},
	{"2006-01-02", 24 * time.Hour},
}

// parseDateTime parses a date or datetime string in local time.
// Supported formats: YYYY-MM-DD, YYYY-MM-DDTHH, YYYY-MM-DDTHH:MM, YYYY-MM-DDTHH:MM:SS.
func parseDateTime(s string) (parsedDate, error) {
	for _, f := range dateFormats {
		if t, err := time.ParseInLocation(f.layout, s, time.Local); err == nil {
			return parsedDate{t, f.gran}, nil
		}
	}
	return parsedDate{}, fmt.Errorf("unrecognized date format %q (expected YYYY-MM-DD, YYYY-MM-DDTHH, YYYY-MM-DDTHH:MM, or YYYY-MM-DDTHH:MM:SS)", s)
}

// parseDateRange parses a "from..to" date range. The to-value is made inclusive
// by advancing it to the end of its granularity unit.
func parseDateRange(fromStr, toStr string) (time.Time, time.Time, error) {
	from, err := parseDateTime(fromStr)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid from-date: %v", err)
	}
	to, err := parseDateTime(toStr)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid to-date: %v", err)
	}
	return from.t, to.t.Add(to.gran - time.Nanosecond), nil
}
