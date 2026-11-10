package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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

// Fish escapes both backslashes and newlines; decode them in a single pass so
// an escaped literal backslash followed by "n" is not turned into a newline.
var fishUnescaper = strings.NewReplacer(`\\`, `\`, `\n`, "\n")

type histFormat int

const (
	formatZsh             histFormat = iota // `: <ts>:<elapsed>;<cmd>`
	formatBashTimestamped                   // `#<unix_ts>` line before each command
	formatBashPlain                         // one command per line, no timestamps
	formatFish                              // `- cmd: <cmd>` + `  when: <ts>` (YAML-like)
)

// Entry represents a single logical shell history entry.
// Multi-line commands span multiple Raw lines.
type Entry struct {
	Timestamp int64
	Elapsed   int64
	Raw       []string // original lines, preserving exact content
	zsh       bool     // read from a zsh file; CommandText decodes zsh escaping
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
// plain bash (one command per line, assumes cmdhist is enabled), and fish.
func ParseHistoryFile(path string) ([]*Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseHistoryData(data), nil
}

func parseHistoryData(data []byte) []*Entry {
	lines := splitHistoryLines(data)
	switch detectFormatLines(lines) {
	case formatZsh:
		return parseZshLines(lines)
	case formatBashTimestamped:
		return parseBashTimestampedLines(lines)
	case formatFish:
		return parseFishLines(lines)
	default:
		return parseBashPlainLines(lines)
	}
}

// Split without a scanner token limit: shell commands can be arbitrarily long.
func splitHistoryLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// detectFormatLines returns the history format based on the first
// recognisable line. Returns formatBashPlain if no marker is found.
func detectFormatLines(lines []string) histFormat {
	for _, line := range lines {
		if entryHeaderRe.MatchString(line) {
			return formatZsh
		}
		if bashTimestampRe.MatchString(line) {
			return formatBashTimestamped
		}
		if strings.HasPrefix(line, "- cmd: ") {
			return formatFish
		}
	}
	return formatBashPlain
}

// parseZshLines parses zsh history. zsh ends every line of a multi-line
// command except the last with a backslash. A line without a `: ts:elapsed;`
// header that does not continue the previous line is an entry without a
// timestamp, which zsh writes when EXTENDED_HISTORY is off. Blank lines stay
// with the preceding entry so they are written back unchanged.
func parseZshLines(lines []string) []*Entry {
	var entries []*Entry
	var current *Entry

	for _, line := range lines {
		if current != nil && strings.HasSuffix(current.Raw[len(current.Raw)-1], `\`) {
			current.Raw = append(current.Raw, line)
		} else if m := entryHeaderRe.FindStringSubmatch(line); m != nil {
			ts, _ := strconv.ParseInt(m[1], 10, 64)
			elapsed, _ := strconv.ParseInt(m[2], 10, 64)
			current = &Entry{
				Timestamp: ts,
				Elapsed:   elapsed,
				Raw:       []string{line},
				zsh:       true,
			}
			entries = append(entries, current)
		} else if current != nil && line == "" {
			current.Raw = append(current.Raw, line)
		} else if line != "" {
			current = &Entry{Raw: []string{line}, zsh: true}
			entries = append(entries, current)
		}
	}

	return entries
}

// zshMeta prefixes bytes that zsh stores XORed with 0x20 ("metafied") in its
// history file, such as some UTF-8 continuation bytes.
const zshMeta = 0x83

// zshCommandText decodes the command lines of a zsh entry: the backslash
// before each newline is removed and metafied bytes are restored. Blank lines
// kept after the command are not part of it.
func zshCommandText(lines []string) string {
	var b strings.Builder
	for i, line := range lines {
		if i == len(lines)-1 || !strings.HasSuffix(line, `\`) {
			b.WriteString(line)
			break
		}
		b.WriteString(line[:len(line)-1])
		b.WriteByte('\n')
	}
	s := b.String()
	if strings.IndexByte(s, zshMeta) < 0 {
		return s
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == zshMeta && i+1 < len(s) {
			i++
			c = s[i] ^ 0x20
		}
		out = append(out, c)
	}
	return string(out)
}

// parseBashTimestampedLines parses a bash history file written with HISTTIMEFORMAT
// set. Each entry begins with a `#<unix_timestamp>` line followed by one or
// more command lines (multi-line commands are supported via lithist).
// Commands before the first timestamp are kept as plain entries. Bash files
// can contain both kinds when HISTTIMEFORMAT was enabled after earlier use.
func parseBashTimestampedLines(lines []string) []*Entry {
	firstMarker := 0
	for firstMarker < len(lines) && !bashTimestampRe.MatchString(lines[firstMarker]) {
		firstMarker++
	}
	entries := parseBashPlainLines(lines[:firstMarker])
	var current *Entry

	for _, line := range lines[firstMarker:] {
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

// parseFishLines parses a fish history file (~/.local/share/fish/fish_history).
// The format is YAML-like: each entry starts with `- cmd: <text>` followed by
// `  when: <unix_timestamp>`. Multi-line commands are stored with literal `\n`
// escape sequences on a single line. All lines until the next `- cmd: ` marker
// belong to the current entry and are preserved verbatim in Raw for round-tripping.
func parseFishLines(lines []string) []*Entry {
	var entries []*Entry
	var current *Entry

	for _, line := range lines {
		if strings.HasPrefix(line, "- cmd: ") {
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

// parseBashPlainLines parses a plain bash history file (no HISTTIMEFORMAT).
// Each non-empty line is treated as a single entry with no timestamp.
//
// Backslash-newline continuations are grouped into one entry. Plain history
// does not otherwise identify multiline boundaries; lithist can also store
// literal newlines without continuation markers, which cannot be grouped here.
//
// Note: a command that genuinely ends with '\' (e.g. a sed expression) is
// indistinguishable from a continuation marker — this is an inherent ambiguity
// in the plain bash history format (bash itself has the same limitation).
func parseBashPlainLines(lines []string) []*Entry {
	var entries []*Entry
	var current *Entry

	for _, line := range lines {
		if line == "" {
			// Keep separators on disk: dropping one after a trailing backslash
			// would merge surviving commands when the file is read again.
			if len(entries) > 0 {
				last := entries[len(entries)-1]
				last.Raw = append(last.Raw, line)
			}
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
	var err error
	path, err = writeTarget(path)
	if err != nil {
		return err
	}
	mode := os.FileMode(0600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return err
	}
	return atomicWrite(path, mode, func(dst io.Writer) error {
		w := bufio.NewWriter(dst)
		if err := writeEntries(w, entries); err != nil {
			return err
		}
		return w.Flush()
	})
}

func writeEntries(w io.Writer, entries []*Entry) error {
	for _, e := range entries {
		for _, line := range e.Raw {
			if _, err := fmt.Fprintln(w, line); err != nil {
				return err
			}
		}
	}
	return nil
}

// ReplaceHistoryFile refuses to overwrite history changed since it was read.
// This catches shell appends while the user is reviewing a confirmation prompt.
func ReplaceHistoryFile(path string, original []byte, entries []*Entry) error {
	current, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, original) {
		return fmt.Errorf("history file changed since it was read; retry the command")
	}
	if err := BackupHistoryFile(path); err != nil {
		return err
	}
	return WriteHistoryFile(path, entries)
}

// writeTarget follows an existing history-file symlink so an atomic rename
// updates its target instead of replacing the link itself.
func writeTarget(path string) (string, error) {
	target, err := filepath.EvalSymlinks(path)
	if os.IsNotExist(err) {
		if _, statErr := os.Lstat(path); statErr == nil {
			return "", err // broken symlink: do not replace it with a new file
		}
		return path, nil
	}
	return target, err
}

func atomicWrite(path string, mode os.FileMode, write func(io.Writer) error) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".hist_tmp_*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := write(tmp); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func atomicCopy(srcPath, dstPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil {
		return err
	}
	return atomicWrite(dstPath, info.Mode().Perm(), func(dst io.Writer) error {
		_, err := io.Copy(dst, src)
		return err
	})
}

// BackupHistoryFile copies path to path+".bak". It is a no-op if path does
// not exist yet.
func BackupHistoryFile(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	return atomicCopy(path, path+".bak")
}

// RestoreBackup atomically restores path+".bak" and first backs up the current
// history file. A second undo therefore restores the state before the first.
func RestoreBackup(path string) error {
	target, err := writeTarget(path)
	if err != nil {
		return err
	}
	bak := path + ".bak"
	src, err := os.Open(bak)
	if err != nil {
		return err
	}
	info, err := src.Stat()
	if err != nil {
		src.Close()
		return err
	}
	data, err := io.ReadAll(src)
	src.Close()
	if err != nil {
		return err
	}
	if err := BackupHistoryFile(path); err != nil {
		return err
	}
	return atomicWrite(target, info.Mode().Perm(), func(dst io.Writer) error {
		_, err := dst.Write(data)
		return err
	})
}

// CommandText returns the command text of an entry, stripping any format-specific
// header lines (zsh `': ts:elapsed;'` prefix or bash `#timestamp` line).
func CommandText(e *Entry) string {
	if len(e.Raw) == 0 {
		return ""
	}
	first := e.Raw[0]

	// zsh: strip `: <ts>:<elapsed>;` prefix from first line, then decode
	if header := entryHeaderRe.FindStringIndex(first); header != nil {
		lines := slices.Clone(e.Raw)
		lines[0] = first[header[1]:]
		return zshCommandText(lines)
	}
	if e.zsh {
		return zshCommandText(e.Raw)
	}

	// bash timestamped: first line is `#<ts>`, command starts at Raw[1]
	if bashTimestampRe.MatchString(first) {
		return strings.Join(e.Raw[1:], "\n")
	}

	// fish: `- cmd: <text>` with literal \n escape sequences for multi-line commands
	if cmd, ok := strings.CutPrefix(first, "- cmd: "); ok {
		return fishUnescaper.Replace(cmd)
	}

	// Plain-history blank separators belong to Raw for round-tripping, but
	// are not part of the command used for matching and deduplication.
	lines := e.Raw
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
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
	result := make([]*Entry, 0, max(len(all)-len(subSet), 0))
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
	for _, e := range entries {
		if e.Timestamp != 0 {
			return true
		}
	}
	return false
}

const errNoTimestamps = "selection %q requires timestamps, but no timestamps were found in this history file"

// SelectEntries returns the entries matched by the selection string.
//
// Selection forms:
//
//	-N                  last N entries (negative integer)
//	N                   first N entries (positive integer)
//	-<duration>         entries whose timestamp >= now - duration  (e.g. -1h, -30m)
//	<duration>          entries within duration from the first timestamped entry (e.g. 1h, 30m)
//	<date>..<date>      entries within the date/datetime range (inclusive)
//	                    dates: YYYY-MM-DD or YYYY-MM-DDTHH:MM:SS
func SelectEntries(entries []*Entry, selection string) ([]*Entry, error) {
	// Try date range: from..to
	if fromStr, toStr, ok := strings.Cut(selection, ".."); ok {
		from, to, err := parseDateRange(fromStr, toStr)
		if err != nil {
			return nil, err
		}
		if !hasTimestamps(entries) {
			return nil, fmt.Errorf(errNoTimestamps, selection)
		}
		return entriesBetweenDates(entries, from, to), nil
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
			pd, dateErr := parseDateTime(selection)
			if dateErr != nil {
				return nil, fmt.Errorf("invalid selection %q: %w", selection, dateErr)
			}
			if !hasTimestamps(entries) {
				return nil, fmt.Errorf(errNoTimestamps, selection)
			}
			return entriesBetweenDates(entries, pd.t, pd.end()), nil
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
			if e.Timestamp != 0 && !e.Time().Before(cutoff) {
				result = append(result, e)
			}
		}
		return result, nil
	}

	// entries within <dur> starting from the first timestamped entry
	var start time.Time
	for _, e := range entries {
		if e.Timestamp != 0 {
			start = e.Time()
			break
		}
	}
	return entriesBetween(entries, start, start.Add(dur)), nil
}

func entriesBetween(entries []*Entry, from, to time.Time) []*Entry {
	var result []*Entry
	for _, e := range entries {
		t := e.Time()
		if e.Timestamp != 0 && !t.Before(from) && !t.After(to) {
			result = append(result, e)
		}
	}
	return result
}

// Date selections compare local civil fields so repeated DST hours include
// both occurrences. Durations continue to use elapsed time in entriesBetween.
func entriesBetweenDates(entries []*Entry, from, to time.Time) []*Entry {
	var result []*Entry
	for _, e := range entries {
		if e.Timestamp == 0 {
			continue
		}
		local := e.Time().In(time.Local)
		year, month, day := local.Date()
		hour, minute, second := local.Clock()
		civil := time.Date(year, month, day, hour, minute, second, 0, time.UTC)
		if !civil.Before(from) && !civil.After(to) {
			result = append(result, e)
		}
	}
	return result
}

// parsedDate stores civil date/time fields in UTC for comparison, independent
// of DST offsets, and the granularity of the smallest unit present.
type parsedDate struct {
	t    time.Time
	gran time.Duration
}

func (d parsedDate) end() time.Time {
	return d.t.Add(d.gran - time.Nanosecond)
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

// parseDateTime validates local civil dates and times without silently shifting
// nonexistent times during DST. Dates can span a skipped local midnight.
// Supported formats: YYYY-MM-DD, YYYY-MM-DDTHH, YYYY-MM-DDTHH:MM, YYYY-MM-DDTHH:MM:SS.
func parseDateTime(s string) (parsedDate, error) {
	for _, f := range dateFormats {
		t, err := time.Parse(f.layout, s)
		if err != nil || t.Format(f.layout) != s {
			continue
		}
		if f.gran < 24*time.Hour {
			local, err := time.ParseInLocation(f.layout, s, time.Local)
			if err != nil || local.Format(f.layout) != s {
				return parsedDate{}, fmt.Errorf("local time %q does not exist in %s", s, time.Local)
			}
		}
		return parsedDate{t, f.gran}, nil
	}
	return parsedDate{}, fmt.Errorf("unrecognized date format %q (expected YYYY-MM-DD, YYYY-MM-DDTHH, YYYY-MM-DDTHH:MM, or YYYY-MM-DDTHH:MM:SS)", s)
}

// parseDateRange parses a "from..to" civil date range. The to-value is made inclusive
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
	if from.t.After(to.end()) {
		return time.Time{}, time.Time{}, fmt.Errorf("from-date must not be after to-date")
	}
	return from.t, to.end(), nil
}
