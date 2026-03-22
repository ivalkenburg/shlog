package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// helpers

func makeEntries(timestamps ...int64) []*Entry {
	entries := make([]*Entry, len(timestamps))
	for i, ts := range timestamps {
		entries[i] = &Entry{
			Timestamp: ts,
			Raw:       []string{formatEntryHeader(ts, 0, "cmd")},
		}
	}
	return entries
}

func formatEntryHeader(ts, elapsed int64, cmd string) string {
	return fmt.Sprintf(": %d:%d;%s", ts, elapsed, cmd)
}

func timestampsOf(entries []*Entry) []int64 {
	ts := make([]int64, len(entries))
	for i, e := range entries {
		ts[i] = e.Timestamp
	}
	return ts
}

func writeTempHistory(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp("", "shlog-test-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	t.Cleanup(func() {
		os.Remove(f.Name())
		os.Remove(f.Name() + ".bak")
	})
	return f.Name()
}

// parseZshHistory

const singleLineHistory = `: 1000:0;echo hello
: 1001:0;ls -la
: 1002:0;pwd
`

const multiLineHistory = `: 1000:0;sudo gem uninstall public_suffix && \
sudo gem uninstall addressable && \
sudo gem uninstall benchmark
: 1001:0;ls -la
`

const continuationWithBlank = `: 1000:0;eval "$(/usr/bin/brew shellenv)"\

: 1001:0;ls
`

const emptyHistory = ``

const onlyNonEntryLines = `# this is a comment
some random line
`

func TestParseHistoryReader_SingleLine(t *testing.T) {
	entries := parseZshHistory(strings.NewReader(singleLineHistory))
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].Timestamp != 1000 {
		t.Errorf("entry[0] timestamp = %d, want 1000", entries[0].Timestamp)
	}
	if entries[2].Timestamp != 1002 {
		t.Errorf("entry[2] timestamp = %d, want 1002", entries[2].Timestamp)
	}
	if len(entries[0].Raw) != 1 {
		t.Errorf("entry[0] should have 1 raw line, got %d", len(entries[0].Raw))
	}
}

func TestParseHistoryReader_MultiLine(t *testing.T) {
	entries := parseZshHistory(strings.NewReader(multiLineHistory))
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	// first entry spans 3 raw lines
	if len(entries[0].Raw) != 3 {
		t.Errorf("entry[0] should have 3 raw lines, got %d: %v", len(entries[0].Raw), entries[0].Raw)
	}
	if entries[0].Raw[1] != "sudo gem uninstall addressable && \\" {
		t.Errorf("unexpected continuation line: %q", entries[0].Raw[1])
	}
	if entries[1].Timestamp != 1001 {
		t.Errorf("entry[1] timestamp = %d, want 1001", entries[1].Timestamp)
	}
}

func TestParseHistoryReader_ContinuationWithBlankLine(t *testing.T) {
	entries := parseZshHistory(strings.NewReader(continuationWithBlank))
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	// the blank line is a continuation of entry[0]
	if len(entries[0].Raw) != 2 {
		t.Errorf("entry[0] should have 2 raw lines (header + blank continuation), got %d", len(entries[0].Raw))
	}
	if entries[0].Raw[1] != "" {
		t.Errorf("entry[0] continuation line should be empty string, got %q", entries[0].Raw[1])
	}
}

func TestParseHistoryReader_Empty(t *testing.T) {
	entries := parseZshHistory(strings.NewReader(emptyHistory))
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestParseHistoryReader_NonEntryLinesIgnored(t *testing.T) {
	entries := parseZshHistory(strings.NewReader(onlyNonEntryLines))
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestParseHistoryReader_ElapsedField(t *testing.T) {
	input := ": 5000:42;some command\n"
	entries := parseZshHistory(strings.NewReader(input))
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Elapsed != 42 {
		t.Errorf("elapsed = %d, want 42", entries[0].Elapsed)
	}
}

func TestParseHistoryReader_PreservesRawLines(t *testing.T) {
	input := ": 1000:0;echo hello\n: 1001:0;ls\n"
	entries := parseZshHistory(strings.NewReader(input))
	if entries[0].Raw[0] != ": 1000:0;echo hello" {
		t.Errorf("raw line not preserved: %q", entries[0].Raw[0])
	}
}

// WriteHistoryFile

func TestWriteHistoryFile_RoundTrip(t *testing.T) {
	original := `: 1000:0;echo hello
: 1001:0;sudo gem uninstall x && \
sudo gem uninstall y
: 1002:0;pwd
`
	path := writeTempHistory(t, original)
	entries := parseZshHistory(strings.NewReader(original))

	if err := WriteHistoryFile(path, entries); err != nil {
		t.Fatalf("WriteHistoryFile: %v", err)
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	entries2 := parseZshHistory(strings.NewReader(string(written)))
	if len(entries2) != len(entries) {
		t.Fatalf("round-trip: got %d entries, want %d", len(entries2), len(entries))
	}
	for i := range entries {
		if entries[i].String() != entries2[i].String() {
			t.Errorf("entry[%d] mismatch:\n  original: %q\n  got:      %q", i, entries[i].String(), entries2[i].String())
		}
	}
}

func TestWriteHistoryFile_Empty(t *testing.T) {
	path := writeTempHistory(t, "")

	if err := WriteHistoryFile(path, nil); err != nil {
		t.Fatalf("WriteHistoryFile with nil: %v", err)
	}

	data, _ := os.ReadFile(path)
	if len(data) != 0 {
		t.Errorf("expected empty file, got %d bytes", len(data))
	}
}

func TestWriteHistoryFile_Atomic(t *testing.T) {
	// Verify that writing replaces the file at the target path and no leftover
	// temp files are present in the same directory afterwards.
	path := writeTempHistory(t, ": 1000:0;original\n")
	dir := filepath.Dir(path)

	entries := []*Entry{entryWithCmd(2000, "new")}
	if err := WriteHistoryFile(path, entries); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "new") {
		t.Errorf("expected new content after atomic write, got: %q", string(data))
	}

	// no .hist_tmp_* leftovers in the directory where the file lives
	des, _ := os.ReadDir(dir)
	for _, de := range des {
		if strings.HasPrefix(de.Name(), ".hist_tmp_") {
			t.Errorf("leftover temp file found: %s", de.Name())
		}
	}
}

// BackupHistoryFile

func TestBackupHistoryFile_CreatesBackup(t *testing.T) {
	path := writeTempHistory(t, ": 1000:0;echo hello\n")

	if err := BackupHistoryFile(path); err != nil {
		t.Fatalf("BackupHistoryFile: %v", err)
	}

	orig, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	bak, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("backup file not created: %v", err)
	}
	if string(orig) != string(bak) {
		t.Errorf("backup content differs from original")
	}
}

func TestBackupHistoryFile_NonExistentIsNoop(t *testing.T) {
	err := BackupHistoryFile("/tmp/hist-test-does-not-exist-xyz.txt")
	if err != nil {
		t.Errorf("expected no error for missing file, got: %v", err)
	}
}

func TestBackupHistoryFile_OverwritesPreviousBackup(t *testing.T) {
	path := writeTempHistory(t, ": 1000:0;first\n")

	if err := BackupHistoryFile(path); err != nil {
		t.Fatal(err)
	}

	// overwrite original with new content
	if err := os.WriteFile(path, []byte(": 2000:0;second\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := BackupHistoryFile(path); err != nil {
		t.Fatal(err)
	}

	bak, _ := os.ReadFile(path + ".bak")
	if !strings.Contains(string(bak), "second") {
		t.Errorf("backup should contain latest content, got: %q", string(bak))
	}
}

// MatchEntries

func TestMatchEntries_LiteralMatch(t *testing.T) {
	entries := []*Entry{
		entryWithCmd(1, "ls -la"),
		entryWithCmd(2, "aws s3 ls"),
		entryWithCmd(3, "docker ps"),
		entryWithCmd(4, "aws ec2 describe-instances"),
	}
	result, err := MatchEntries(entries, `^aws `)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2, got %d", len(result))
	}
	if result[0].Timestamp != 2 || result[1].Timestamp != 4 {
		t.Errorf("unexpected matches: %v", timestampsOf(result))
	}
}

func TestMatchEntries_NoMatch(t *testing.T) {
	entries := []*Entry{
		entryWithCmd(1, "ls -la"),
		entryWithCmd(2, "pwd"),
	}
	result, err := MatchEntries(entries, `^git `)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 matches, got %d", len(result))
	}
}

func TestMatchEntries_AllMatch(t *testing.T) {
	entries := []*Entry{
		entryWithCmd(1, "git status"),
		entryWithCmd(2, "git log"),
		entryWithCmd(3, "git diff"),
	}
	result, err := MatchEntries(entries, `git`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 3 {
		t.Errorf("expected 3, got %d", len(result))
	}
}

func TestMatchEntries_MultiLineCommand(t *testing.T) {
	e := &Entry{
		Timestamp: 1,
		Raw: []string{
			": 1:0;curl --location 'https://api.example.com' \\",
			"--header 'Authorization: Bearer token'",
		},
	}
	entries := []*Entry{e, entryWithCmd(2, "ls")}

	result, err := MatchEntries(entries, `Authorization`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].Timestamp != 1 {
		t.Errorf("expected to match multi-line entry, got %v", timestampsOf(result))
	}
}

func TestMatchEntries_InvalidPattern(t *testing.T) {
	entries := []*Entry{entryWithCmd(1, "ls")}
	_, err := MatchEntries(entries, `[invalid`)
	if err == nil {
		t.Error("expected error for invalid regex")
	}
}

func TestMatchEntries_EmptyEntries(t *testing.T) {
	result, err := MatchEntries(nil, `.*`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0, got %d", len(result))
	}
}

func TestMatchEntries_CaseInsensitive(t *testing.T) {
	entries := []*Entry{
		entryWithCmd(1, "Docker ps"),
		entryWithCmd(2, "docker ps"),
	}
	result, err := MatchEntries(entries, `(?i)docker`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 case-insensitive matches, got %d", len(result))
	}
}

// RestoreBackup

func TestRestoreBackup_RestoresContent(t *testing.T) {
	path := writeTempHistory(t, ": 1000:0;original\n")

	// create backup manually
	if err := os.WriteFile(path+".bak", []byte(": 2000:0;from backup\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := RestoreBackup(path); err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "from backup") {
		t.Errorf("expected restored content, got: %q", string(data))
	}
}

func TestRestoreBackup_MissingBakReturnsError(t *testing.T) {
	path := writeTempHistory(t, ": 1000:0;original\n")
	// no .bak file created
	err := RestoreBackup(path)
	if err == nil {
		t.Error("expected error when .bak is missing")
	}
}

func TestRestoreBackup_IsAtomic(t *testing.T) {
	path := writeTempHistory(t, ": 1000:0;original\n")
	if err := os.WriteFile(path+".bak", []byte(": 2000:0;backup\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := RestoreBackup(path); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Dir(path)
	des, _ := os.ReadDir(dir)
	for _, de := range des {
		if strings.HasPrefix(de.Name(), ".hist_tmp_") {
			t.Errorf("leftover temp file found: %s", de.Name())
		}
	}
}

// InvertEntries

func TestInvertEntries_RemovesSubset(t *testing.T) {
	all := makeEntries(1, 2, 3, 4, 5)
	subset := []*Entry{all[1], all[3]} // ts=2, ts=4

	result := InvertEntries(all, subset)
	if len(result) != 3 {
		t.Fatalf("expected 3, got %d", len(result))
	}
	if result[0].Timestamp != 1 || result[1].Timestamp != 3 || result[2].Timestamp != 5 {
		t.Errorf("unexpected timestamps: %v", timestampsOf(result))
	}
}

func TestInvertEntries_EmptySubset(t *testing.T) {
	all := makeEntries(1, 2, 3)
	result := InvertEntries(all, nil)
	if len(result) != 3 {
		t.Errorf("expected all 3, got %d", len(result))
	}
}

func TestInvertEntries_FullSubset(t *testing.T) {
	all := makeEntries(1, 2, 3)
	result := InvertEntries(all, all)
	if len(result) != 0 {
		t.Errorf("expected 0, got %d", len(result))
	}
}

func TestInvertEntries_PreservesOrder(t *testing.T) {
	all := makeEntries(1, 2, 3, 4, 5)
	subset := []*Entry{all[0], all[2], all[4]} // ts=1,3,5

	result := InvertEntries(all, subset)
	if len(result) != 2 {
		t.Fatalf("expected 2, got %d", len(result))
	}
	if result[0].Timestamp != 2 || result[1].Timestamp != 4 {
		t.Errorf("unexpected timestamps: %v", timestampsOf(result))
	}
}

// DeduplicateEntries - scoped (via InvertEntries)

func TestDeduplicateScoped_OnlyDedupsWithinScope(t *testing.T) {
	// all: [ls(1), ls(2), pwd(3), ls(4), pwd(5)]
	// scope: last 3 = [pwd(3), ls(4), pwd(5)]
	// within scope: pwd appears twice → pwd(3) removed, ls(4) and pwd(5) kept
	// outside scope: ls(1) and ls(2) untouched
	all := []*Entry{
		entryWithCmd(1, "ls"),
		entryWithCmd(2, "ls"),
		entryWithCmd(3, "pwd"),
		entryWithCmd(4, "ls"),
		entryWithCmd(5, "pwd"),
	}
	scope := all[2:] // [pwd(3), ls(4), pwd(5)]

	_, removed := DeduplicateEntries(scope, false)
	remaining := InvertEntries(all, removed)

	// pwd(3) should be removed; ls(1), ls(2), ls(4), pwd(5) kept
	if len(remaining) != 4 {
		t.Fatalf("expected 4, got %d: %v", len(remaining), timestampsOf(remaining))
	}
	wantTS := []int64{1, 2, 4, 5}
	for i, want := range wantTS {
		if remaining[i].Timestamp != want {
			t.Errorf("remaining[%d]: got ts=%d, want ts=%d", i, remaining[i].Timestamp, want)
		}
	}
}

// SelectEntries - integer selections

func TestSelectEntries_Empty(t *testing.T) {
	result, err := SelectEntries(nil, "-1")
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestSelectEntries_LastOne(t *testing.T) {
	entries := makeEntries(1, 2, 3, 4, 5)
	result, err := SelectEntries(entries, "-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
	if result[0].Timestamp != 5 {
		t.Errorf("expected last entry (ts=5), got ts=%d", result[0].Timestamp)
	}
}

func TestSelectEntries_LastN(t *testing.T) {
	entries := makeEntries(1, 2, 3, 4, 5)
	result, err := SelectEntries(entries, "-3")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3, got %d", len(result))
	}
	if result[0].Timestamp != 3 || result[2].Timestamp != 5 {
		t.Errorf("unexpected entries: %v", timestampsOf(result))
	}
}

func TestSelectEntries_LastN_ExceedsLen(t *testing.T) {
	entries := makeEntries(1, 2, 3)
	result, err := SelectEntries(entries, "-10")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 3 {
		t.Fatalf("expected all 3, got %d", len(result))
	}
}

func TestSelectEntries_FirstOne(t *testing.T) {
	entries := makeEntries(1, 2, 3, 4, 5)
	result, err := SelectEntries(entries, "1")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
	if result[0].Timestamp != 1 {
		t.Errorf("expected first entry (ts=1), got ts=%d", result[0].Timestamp)
	}
}

func TestSelectEntries_FirstN(t *testing.T) {
	entries := makeEntries(1, 2, 3, 4, 5)
	result, err := SelectEntries(entries, "3")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3, got %d", len(result))
	}
	if result[0].Timestamp != 1 || result[2].Timestamp != 3 {
		t.Errorf("unexpected entries: %v", timestampsOf(result))
	}
}

func TestSelectEntries_FirstN_ExceedsLen(t *testing.T) {
	entries := makeEntries(1, 2, 3)
	result, err := SelectEntries(entries, "10")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 3 {
		t.Fatalf("expected all 3, got %d", len(result))
	}
}

func TestSelectEntries_ZeroInt_Error(t *testing.T) {
	entries := makeEntries(1, 2)
	_, err := SelectEntries(entries, "0")
	if err == nil {
		t.Error("expected error for selection '0'")
	}
}

// SelectEntries - duration selections

func TestSelectEntries_NegDuration_RecentEntries(t *testing.T) {
	now := time.Now()
	old := now.Add(-2 * time.Hour).Unix()
	recent := now.Add(-30 * time.Minute).Unix()
	veryRecent := now.Add(-5 * time.Minute).Unix()

	entries := makeEntries(old, recent, veryRecent)

	result, err := SelectEntries(entries, "-1h")
	if err != nil {
		t.Fatal(err)
	}
	// should include entries from last hour (recent and veryRecent)
	if len(result) != 2 {
		t.Fatalf("expected 2 entries within last 1h, got %d", len(result))
	}
	if result[0].Timestamp != recent || result[1].Timestamp != veryRecent {
		t.Errorf("unexpected entries: %v", timestampsOf(result))
	}
}

func TestSelectEntries_NegDuration_AllOld(t *testing.T) {
	now := time.Now()
	old1 := now.Add(-3 * time.Hour).Unix()
	old2 := now.Add(-2 * time.Hour).Unix()

	entries := makeEntries(old1, old2)

	result, err := SelectEntries(entries, "-30m")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 entries, got %d", len(result))
	}
}

func TestSelectEntries_NegDuration_AllRecent(t *testing.T) {
	now := time.Now()
	entries := makeEntries(
		now.Add(-5*time.Minute).Unix(),
		now.Add(-3*time.Minute).Unix(),
		now.Add(-1*time.Minute).Unix(),
	)

	result, err := SelectEntries(entries, "-1h")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 3 {
		t.Fatalf("expected all 3, got %d", len(result))
	}
}

func TestSelectEntries_PosDuration_FromFirst(t *testing.T) {
	base := int64(1000000)
	entries := makeEntries(
		base,
		base+1800, // +30m
		base+3600, // +1h exactly
		base+3601, // just past 1h
		base+7200, // +2h
	)

	result, err := SelectEntries(entries, "1h")
	if err != nil {
		t.Fatal(err)
	}
	// entries at base, base+30m, base+1h exactly should be included
	if len(result) != 3 {
		t.Fatalf("expected 3 entries within 1h from first, got %d: %v", len(result), timestampsOf(result))
	}
}

func TestSelectEntries_PosDuration_AllWithin(t *testing.T) {
	base := int64(1000000)
	entries := makeEntries(base, base+60, base+120)

	result, err := SelectEntries(entries, "1h")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 3 {
		t.Fatalf("expected all 3, got %d", len(result))
	}
}

func TestSelectEntries_PosDuration_NoneWithin(t *testing.T) {
	base := int64(1000000)
	// first entry is the anchor; the rest are 2h+ later
	entries := makeEntries(base, base+7200, base+10800)

	result, err := SelectEntries(entries, "30m")
	if err != nil {
		t.Fatal(err)
	}
	// only the first entry (at t=base) is within 30m
	if len(result) != 1 {
		t.Fatalf("expected 1 (just the anchor), got %d", len(result))
	}
}

func TestSelectEntries_InvalidSelection(t *testing.T) {
	entries := makeEntries(1, 2)
	cases := []string{"xyz", "-xyz", "1x", "-1x", ""}
	for _, sel := range cases {
		_, err := SelectEntries(entries, sel)
		if err == nil {
			t.Errorf("expected error for selection %q", sel)
		}
	}
}

// SelectEntries - date range selections

func TestSelectEntries_DateRange_FullDay(t *testing.T) {
	// timestamps in seconds: 2024-01-15 12:00:00, 2024-01-20 00:00:00, 2024-02-01 00:00:00
	jan15, _ := time.ParseInLocation("2006-01-02T15:04:05", "2024-01-15T12:00:00", time.Local)
	jan20, _ := time.ParseInLocation("2006-01-02T15:04:05", "2024-01-20T00:00:00", time.Local)
	feb01, _ := time.ParseInLocation("2006-01-02T15:04:05", "2024-02-01T00:00:00", time.Local)

	entries := makeEntries(jan15.Unix(), jan20.Unix(), feb01.Unix())

	result, err := SelectEntries(entries, "2024-01-01..2024-01-31")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 entries in January, got %d", len(result))
	}
	if result[0].Timestamp != jan15.Unix() || result[1].Timestamp != jan20.Unix() {
		t.Errorf("unexpected entries: %v", timestampsOf(result))
	}
}

func TestSelectEntries_DateRange_ToDateInclusive(t *testing.T) {
	// entry at exactly 23:59:59 on the to-date should be included
	endOfDay, _ := time.ParseInLocation("2006-01-02T15:04:05", "2024-01-31T23:59:59", time.Local)
	entries := makeEntries(endOfDay.Unix())

	result, err := SelectEntries(entries, "2024-01-01..2024-01-31")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 entry (end-of-day inclusive), got %d", len(result))
	}
}

func TestSelectEntries_DateRange_Datetime(t *testing.T) {
	t10, _ := time.ParseInLocation("2006-01-02T15:04:05", "2024-01-15T10:00:00", time.Local)
	t12, _ := time.ParseInLocation("2006-01-02T15:04:05", "2024-01-15T12:00:00", time.Local)
	t14, _ := time.ParseInLocation("2006-01-02T15:04:05", "2024-01-15T14:00:00", time.Local)

	entries := makeEntries(t10.Unix(), t12.Unix(), t14.Unix())

	result, err := SelectEntries(entries, "2024-01-15T11:00:00..2024-01-15T13:00:00")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}
	if result[0].Timestamp != t12.Unix() {
		t.Errorf("expected t12, got ts=%d", result[0].Timestamp)
	}
}

func TestSelectEntries_DateRange_NoMatch(t *testing.T) {
	jan15, _ := time.ParseInLocation("2006-01-02", "2024-01-15", time.Local)
	entries := makeEntries(jan15.Unix())

	result, err := SelectEntries(entries, "2024-02-01..2024-02-28")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 entries, got %d", len(result))
	}
}

func TestSelectEntries_DateRange_InvalidFrom(t *testing.T) {
	entries := makeEntries(1000)
	_, err := SelectEntries(entries, "not-a-date..2024-01-31")
	if err == nil {
		t.Error("expected error for invalid from-date")
	}
}

func TestSelectEntries_DateRange_InvalidTo(t *testing.T) {
	entries := makeEntries(1000)
	_, err := SelectEntries(entries, "2024-01-01..not-a-date")
	if err == nil {
		t.Error("expected error for invalid to-date")
	}
}

func TestSelectEntries_DateRange_HourGranularity(t *testing.T) {
	t09, _ := time.ParseInLocation("2006-01-02T15:04:05", "2024-01-15T09:30:00", time.Local)
	t10, _ := time.ParseInLocation("2006-01-02T15:04:05", "2024-01-15T10:30:00", time.Local)
	t11, _ := time.ParseInLocation("2006-01-02T15:04:05", "2024-01-15T11:30:00", time.Local)
	entries := makeEntries(t09.Unix(), t10.Unix(), t11.Unix())

	result, err := SelectEntries(entries, "2024-01-15T10..2024-01-15T10")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].Timestamp != t10.Unix() {
		t.Errorf("expected t10 only, got %v", timestampsOf(result))
	}
}

// SelectEntries - single date/datetime selections

func TestSelectEntries_SingleDate_Day(t *testing.T) {
	jan15, _ := time.ParseInLocation("2006-01-02T15:04:05", "2024-01-15T12:00:00", time.Local)
	jan16, _ := time.ParseInLocation("2006-01-02T15:04:05", "2024-01-16T08:00:00", time.Local)
	entries := makeEntries(jan15.Unix(), jan16.Unix())

	result, err := SelectEntries(entries, "2024-01-15")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].Timestamp != jan15.Unix() {
		t.Errorf("expected only jan15 entry, got %v", timestampsOf(result))
	}
}

func TestSelectEntries_SingleDate_DayBoundary(t *testing.T) {
	// entries at 00:00:00 and 23:59:59 on the same day should both be included
	startOfDay, _ := time.ParseInLocation("2006-01-02T15:04:05", "2024-01-15T00:00:00", time.Local)
	endOfDay, _ := time.ParseInLocation("2006-01-02T15:04:05", "2024-01-15T23:59:59", time.Local)
	entries := makeEntries(startOfDay.Unix(), endOfDay.Unix())

	result, err := SelectEntries(entries, "2024-01-15")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result))
	}
}

func TestSelectEntries_SingleDate_Hour(t *testing.T) {
	t09, _ := time.ParseInLocation("2006-01-02T15:04:05", "2024-01-15T09:59:59", time.Local)
	t10a, _ := time.ParseInLocation("2006-01-02T15:04:05", "2024-01-15T10:00:00", time.Local)
	t10b, _ := time.ParseInLocation("2006-01-02T15:04:05", "2024-01-15T10:30:00", time.Local)
	t10c, _ := time.ParseInLocation("2006-01-02T15:04:05", "2024-01-15T10:59:59", time.Local)
	t11, _ := time.ParseInLocation("2006-01-02T15:04:05", "2024-01-15T11:00:00", time.Local)
	entries := makeEntries(t09.Unix(), t10a.Unix(), t10b.Unix(), t10c.Unix(), t11.Unix())

	result, err := SelectEntries(entries, "2024-01-15T10")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 entries in the 10:xx hour, got %d: %v", len(result), timestampsOf(result))
	}
}

func TestSelectEntries_SingleDate_Minute(t *testing.T) {
	t1029, _ := time.ParseInLocation("2006-01-02T15:04:05", "2024-01-15T10:29:59", time.Local)
	t1030a, _ := time.ParseInLocation("2006-01-02T15:04:05", "2024-01-15T10:30:00", time.Local)
	t1030b, _ := time.ParseInLocation("2006-01-02T15:04:05", "2024-01-15T10:30:45", time.Local)
	t1031, _ := time.ParseInLocation("2006-01-02T15:04:05", "2024-01-15T10:31:00", time.Local)
	entries := makeEntries(t1029.Unix(), t1030a.Unix(), t1030b.Unix(), t1031.Unix())

	result, err := SelectEntries(entries, "2024-01-15T10:30")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 entries in 10:30, got %d: %v", len(result), timestampsOf(result))
	}
	if result[0].Timestamp != t1030a.Unix() || result[1].Timestamp != t1030b.Unix() {
		t.Errorf("unexpected entries: %v", timestampsOf(result))
	}
}

func TestSelectEntries_SingleDate_Second(t *testing.T) {
	t1, _ := time.ParseInLocation("2006-01-02T15:04:05", "2024-01-15T10:30:44", time.Local)
	t2, _ := time.ParseInLocation("2006-01-02T15:04:05", "2024-01-15T10:30:45", time.Local)
	t3, _ := time.ParseInLocation("2006-01-02T15:04:05", "2024-01-15T10:30:46", time.Local)
	entries := makeEntries(t1.Unix(), t2.Unix(), t3.Unix())

	result, err := SelectEntries(entries, "2024-01-15T10:30:45")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].Timestamp != t2.Unix() {
		t.Errorf("expected only t2, got %v", timestampsOf(result))
	}
}

func TestSelectEntries_SingleDate_NoMatch(t *testing.T) {
	jan15, _ := time.ParseInLocation("2006-01-02", "2024-01-15", time.Local)
	entries := makeEntries(jan15.Unix())

	result, err := SelectEntries(entries, "2024-02-01")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 entries, got %d", len(result))
	}
}

// MatchEntriesRe

func TestMatchEntriesRe_Basic(t *testing.T) {
	entries := []*Entry{
		entryWithCmd(1, "git status"),
		entryWithCmd(2, "docker ps"),
		entryWithCmd(3, "git log"),
	}
	re := regexp.MustCompile(`^git`)
	result := MatchEntriesRe(entries, re)
	if len(result) != 2 {
		t.Fatalf("expected 2, got %d", len(result))
	}
	if result[0].Timestamp != 1 || result[1].Timestamp != 3 {
		t.Errorf("unexpected: %v", timestampsOf(result))
	}
}

// ComputeStats

func TestComputeStats_Basic(t *testing.T) {
	entries := []*Entry{
		entryWithCmd(1, "ls"),
		entryWithCmd(2, "git status"),
		entryWithCmd(3, "ls"),
		entryWithCmd(4, "pwd"),
		entryWithCmd(5, "ls"),
	}
	stats := ComputeStats(entries, 10)

	if stats.Total != 5 {
		t.Errorf("Total: got %d, want 5", stats.Total)
	}
	if stats.Unique != 3 {
		t.Errorf("Unique: got %d, want 3", stats.Unique)
	}
	if len(stats.TopN) != 3 {
		t.Fatalf("TopN len: got %d, want 3", len(stats.TopN))
	}
	// ls appears 3 times and should be first
	if stats.TopN[0].Command != "ls" || stats.TopN[0].Count != 3 {
		t.Errorf("TopN[0]: got %q x%d, want \"ls\" x3", stats.TopN[0].Command, stats.TopN[0].Count)
	}
}

func TestComputeStats_TopNTruncated(t *testing.T) {
	entries := []*Entry{
		entryWithCmd(1, "a"),
		entryWithCmd(2, "b"),
		entryWithCmd(3, "c"),
		entryWithCmd(4, "d"),
		entryWithCmd(5, "e"),
	}
	stats := ComputeStats(entries, 3)
	if len(stats.TopN) != 3 {
		t.Errorf("expected 3 top entries, got %d", len(stats.TopN))
	}
}

func TestComputeStats_TopNLargerThanUnique(t *testing.T) {
	entries := []*Entry{
		entryWithCmd(1, "ls"),
		entryWithCmd(2, "pwd"),
	}
	stats := ComputeStats(entries, 10)
	if len(stats.TopN) != 2 {
		t.Errorf("expected 2 (capped to unique count), got %d", len(stats.TopN))
	}
}

func TestComputeStats_SortedByCount(t *testing.T) {
	entries := []*Entry{
		entryWithCmd(1, "a"),
		entryWithCmd(2, "b"),
		entryWithCmd(3, "b"),
		entryWithCmd(4, "b"),
		entryWithCmd(5, "c"),
		entryWithCmd(6, "c"),
	}
	stats := ComputeStats(entries, 10)
	if stats.TopN[0].Command != "b" || stats.TopN[0].Count != 3 {
		t.Errorf("expected 'b'x3 first, got %q x%d", stats.TopN[0].Command, stats.TopN[0].Count)
	}
	if stats.TopN[1].Command != "c" || stats.TopN[1].Count != 2 {
		t.Errorf("expected 'c'x2 second, got %q x%d", stats.TopN[1].Command, stats.TopN[1].Count)
	}
}

func TestComputeStats_TiesBrokenAlphabetically(t *testing.T) {
	entries := []*Entry{
		entryWithCmd(1, "zebra"),
		entryWithCmd(2, "apple"),
		entryWithCmd(3, "mango"),
	}
	stats := ComputeStats(entries, 10)
	// all count=1, should be sorted alphabetically
	names := make([]string, len(stats.TopN))
	for i, cc := range stats.TopN {
		names[i] = cc.Command
	}
	want := []string{"apple", "mango", "zebra"}
	for i, w := range want {
		if names[i] != w {
			t.Errorf("TopN[%d]: got %q, want %q", i, names[i], w)
		}
	}
}

func TestComputeStats_Empty(t *testing.T) {
	stats := ComputeStats(nil, 10)
	if stats.Total != 0 || stats.Unique != 0 || len(stats.TopN) != 0 {
		t.Errorf("expected empty stats for nil input")
	}
}

// Entry.String

func TestEntryString_SingleLine(t *testing.T) {
	e := &Entry{Raw: []string{": 1000:0;echo hello"}}
	if e.String() != ": 1000:0;echo hello" {
		t.Errorf("unexpected: %q", e.String())
	}
}

func TestEntryString_MultiLine(t *testing.T) {
	e := &Entry{Raw: []string{": 1000:0;cmd \\", "continuation"}}
	want := ": 1000:0;cmd \\\ncontinuation"
	if e.String() != want {
		t.Errorf("got %q, want %q", e.String(), want)
	}
}

// CommandText

func TestCommandText_SingleLine(t *testing.T) {
	e := &Entry{Raw: []string{": 1000:0;ls -la"}}
	if got := CommandText(e); got != "ls -la" {
		t.Errorf("got %q, want %q", got, "ls -la")
	}
}

func TestCommandText_MultiLine(t *testing.T) {
	e := &Entry{Raw: []string{": 1000:0;cmd arg1 \\", "arg2 \\", "arg3"}}
	want := "cmd arg1 \\\narg2 \\\narg3"
	if got := CommandText(e); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCommandText_EmptyCommand(t *testing.T) {
	e := &Entry{Raw: []string{": 1000:0;"}}
	if got := CommandText(e); got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestCommandText_EmptyRaw(t *testing.T) {
	e := &Entry{}
	if got := CommandText(e); got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

// DeduplicateEntries

func entryWithCmd(ts int64, cmd string) *Entry {
	return &Entry{
		Timestamp: ts,
		Raw:       []string{fmt.Sprintf(": %d:0;%s", ts, cmd)},
	}
}

func TestDeduplicateEntries_NoDuplicates(t *testing.T) {
	entries := []*Entry{
		entryWithCmd(1, "ls"),
		entryWithCmd(2, "pwd"),
		entryWithCmd(3, "cd /tmp"),
	}
	keep, removed := DeduplicateEntries(entries, false)
	if len(keep) != 3 {
		t.Errorf("keep: expected 3, got %d", len(keep))
	}
	if len(removed) != 0 {
		t.Errorf("removed: expected 0, got %d", len(removed))
	}
}

func TestDeduplicateEntries_AllSame(t *testing.T) {
	entries := []*Entry{
		entryWithCmd(1, "ls"),
		entryWithCmd(2, "ls"),
		entryWithCmd(3, "ls"),
	}
	keep, removed := DeduplicateEntries(entries, false)
	if len(keep) != 1 {
		t.Fatalf("keep: expected 1, got %d", len(keep))
	}
	// most recent (ts=3) is kept
	if keep[0].Timestamp != 3 {
		t.Errorf("expected most recent (ts=3) to be kept, got ts=%d", keep[0].Timestamp)
	}
	if len(removed) != 2 {
		t.Errorf("removed: expected 2, got %d", len(removed))
	}
}

func TestDeduplicateEntries_KeepsMostRecent(t *testing.T) {
	entries := []*Entry{
		entryWithCmd(1, "ls"),  // duplicate, earlier
		entryWithCmd(2, "pwd"), // unique
		entryWithCmd(3, "ls"),  // duplicate, more recent → kept
		entryWithCmd(4, "ls"),  // duplicate, most recent → kept
	}
	keep, removed := DeduplicateEntries(entries, false)
	if len(keep) != 2 {
		t.Fatalf("keep: expected 2, got %d: %v", len(keep), timestampsOf(keep))
	}
	// kept entries should be pwd (ts=2) and ls at ts=4
	if keep[0].Timestamp != 2 {
		t.Errorf("keep[0] should be pwd (ts=2), got ts=%d", keep[0].Timestamp)
	}
	if keep[1].Timestamp != 4 {
		t.Errorf("keep[1] should be ls (ts=4), got ts=%d", keep[1].Timestamp)
	}
	if len(removed) != 2 {
		t.Errorf("removed: expected 2, got %d", len(removed))
	}
	// removed entries should be the earlier ls entries (ts=1, ts=3)
	if removed[0].Timestamp != 1 || removed[1].Timestamp != 3 {
		t.Errorf("removed entries unexpected: %v", timestampsOf(removed))
	}
}

func TestDeduplicateEntries_PreservesOrder(t *testing.T) {
	entries := []*Entry{
		entryWithCmd(1, "a"),
		entryWithCmd(2, "b"),
		entryWithCmd(3, "c"),
		entryWithCmd(4, "a"), // dup of ts=1, this one kept
		entryWithCmd(5, "d"),
	}
	keep, removed := DeduplicateEntries(entries, false)
	if len(keep) != 4 {
		t.Fatalf("keep: expected 4, got %d", len(keep))
	}
	// order should be preserved: b(2), c(3), a(4), d(5)
	wantTS := []int64{2, 3, 4, 5}
	for i, want := range wantTS {
		if keep[i].Timestamp != want {
			t.Errorf("keep[%d]: got ts=%d, want ts=%d", i, keep[i].Timestamp, want)
		}
	}
	if len(removed) != 1 || removed[0].Timestamp != 1 {
		t.Errorf("removed: expected [ts=1], got %v", timestampsOf(removed))
	}
}

func TestDeduplicateEntries_MultiLineCommands(t *testing.T) {
	mkMulti := func(ts int64) *Entry {
		return &Entry{
			Timestamp: ts,
			Raw: []string{
				fmt.Sprintf(": %d:0;cmd \\", ts),
				"continuation",
			},
		}
	}
	entries := []*Entry{
		mkMulti(1),
		entryWithCmd(2, "other"),
		mkMulti(3), // same command as ts=1, more recent → kept
	}
	keep, removed := DeduplicateEntries(entries, false)
	if len(keep) != 2 {
		t.Fatalf("keep: expected 2, got %d", len(keep))
	}
	if keep[0].Timestamp != 2 || keep[1].Timestamp != 3 {
		t.Errorf("unexpected keep timestamps: %v", timestampsOf(keep))
	}
	if len(removed) != 1 || removed[0].Timestamp != 1 {
		t.Errorf("removed: expected [ts=1], got %v", timestampsOf(removed))
	}
}

func TestDeduplicateEntries_Empty(t *testing.T) {
	keep, removed := DeduplicateEntries(nil, false)
	if len(keep) != 0 || len(removed) != 0 {
		t.Errorf("expected empty results for nil input")
	}
}

func TestDeduplicateEntries_SingleEntry(t *testing.T) {
	entries := []*Entry{entryWithCmd(1, "ls")}
	keep, removed := DeduplicateEntries(entries, false)
	if len(keep) != 1 || len(removed) != 0 {
		t.Errorf("single entry: keep=%d removed=%d", len(keep), len(removed))
	}
}

func TestDeduplicateEntries_SameTimestampDifferentCommands(t *testing.T) {
	// same timestamp but different commands: both unique
	entries := []*Entry{
		entryWithCmd(1000, "ls"),
		entryWithCmd(1000, "pwd"),
	}
	keep, removed := DeduplicateEntries(entries, false)
	if len(keep) != 2 {
		t.Errorf("keep: expected 2, got %d", len(keep))
	}
	if len(removed) != 0 {
		t.Errorf("removed: expected 0, got %d", len(removed))
	}
}

// DeduplicateEntries - keepOldest=true

func TestDeduplicateEntries_KeepOldest_Basic(t *testing.T) {
	entries := []*Entry{
		entryWithCmd(1, "ls"),  // oldest → kept
		entryWithCmd(2, "pwd"), // unique
		entryWithCmd(3, "ls"),  // duplicate → removed
		entryWithCmd(4, "ls"),  // duplicate → removed
	}
	keep, removed := DeduplicateEntries(entries, true)
	if len(keep) != 2 {
		t.Fatalf("keep: expected 2, got %d: %v", len(keep), timestampsOf(keep))
	}
	if keep[0].Timestamp != 1 {
		t.Errorf("expected oldest ls (ts=1) to be kept, got ts=%d", keep[0].Timestamp)
	}
	if len(removed) != 2 {
		t.Errorf("removed: expected 2, got %d", len(removed))
	}
}

func TestDeduplicateEntries_KeepOldest_PreservesOrder(t *testing.T) {
	entries := []*Entry{
		entryWithCmd(1, "a"),
		entryWithCmd(2, "b"),
		entryWithCmd(3, "c"),
		entryWithCmd(4, "a"), // dup → removed
		entryWithCmd(5, "d"),
	}
	keep, removed := DeduplicateEntries(entries, true)
	if len(keep) != 4 {
		t.Fatalf("keep: expected 4, got %d", len(keep))
	}
	want := []int64{1, 2, 3, 5}
	for i, ts := range want {
		if keep[i].Timestamp != ts {
			t.Errorf("keep[%d]: expected ts=%d, got ts=%d", i, ts, keep[i].Timestamp)
		}
	}
	if len(removed) != 1 || removed[0].Timestamp != 4 {
		t.Errorf("removed: expected [ts=4], got %v", timestampsOf(removed))
	}
}

func TestDeduplicateEntries_KeepOldest_AllSame(t *testing.T) {
	entries := []*Entry{
		entryWithCmd(1, "ls"),
		entryWithCmd(2, "ls"),
		entryWithCmd(3, "ls"),
	}
	keep, removed := DeduplicateEntries(entries, true)
	if len(keep) != 1 {
		t.Fatalf("keep: expected 1, got %d", len(keep))
	}
	if keep[0].Timestamp != 1 {
		t.Errorf("expected ts=1 (oldest) to be kept, got ts=%d", keep[0].Timestamp)
	}
	if len(removed) != 2 {
		t.Errorf("removed: expected 2, got %d", len(removed))
	}
}

func TestDeduplicateEntries_KeepOldest_VsKeepNewest(t *testing.T) {
	// keepOldest=false keeps ts=3 (most recent); keepOldest=true keeps ts=1 (oldest)
	entries := []*Entry{
		entryWithCmd(1, "ls"),
		entryWithCmd(2, "pwd"),
		entryWithCmd(3, "ls"),
	}
	keepNewest, _ := DeduplicateEntries(entries, false)
	keepOldest, _ := DeduplicateEntries(entries, true)

	// newest: ls kept at ts=3
	if keepNewest[1].Timestamp != 3 {
		t.Errorf("keepNewest: expected ls at ts=3, got ts=%d", keepNewest[1].Timestamp)
	}
	// oldest: ls kept at ts=1
	if keepOldest[0].Timestamp != 1 {
		t.Errorf("keepOldest: expected ls at ts=1, got ts=%d", keepOldest[0].Timestamp)
	}
}

// bash history fixtures

const bashPlainHistory = `ls -la
git status
docker ps
`

const bashTimestampedHistory = `#1704067200
ls -la
#1704067260
git status
#1704067320
docker ps
`

const bashTimestampedMultiLine = `#1704067200
for i in 1 2 3
do
  echo $i
done
#1704067320
pwd
`

// detectFormat

func TestDetectFormat_Zsh(t *testing.T) {
	if got := detectFormat(strings.NewReader(singleLineHistory)); got != formatZsh {
		t.Errorf("expected formatZsh, got %v", got)
	}
}

func TestDetectFormat_BashTimestamped(t *testing.T) {
	if got := detectFormat(strings.NewReader(bashTimestampedHistory)); got != formatBashTimestamped {
		t.Errorf("expected formatBashTimestamped, got %v", got)
	}
}

func TestDetectFormat_BashPlain(t *testing.T) {
	if got := detectFormat(strings.NewReader(bashPlainHistory)); got != formatBashPlain {
		t.Errorf("expected formatBashPlain, got %v", got)
	}
}

func TestDetectFormat_Empty(t *testing.T) {
	if got := detectFormat(strings.NewReader("")); got != formatBashPlain {
		t.Errorf("expected formatBashPlain for empty input, got %v", got)
	}
}

func TestDetectFormat_ZshWinsOverBash(t *testing.T) {
	// A file that starts with a zsh header should be detected as zsh even if
	// it also contains lines that look like bash timestamps.
	mixed := ": 1000:0;echo hello\n#1704067200\nls\n"
	if got := detectFormat(strings.NewReader(mixed)); got != formatZsh {
		t.Errorf("expected formatZsh, got %v", got)
	}
}

// parseBashPlain

func TestParseBashPlain_Basic(t *testing.T) {
	entries := parseBashPlain(strings.NewReader(bashPlainHistory))
	if len(entries) != 3 {
		t.Fatalf("expected 3, got %d", len(entries))
	}
	if CommandText(entries[0]) != "ls -la" {
		t.Errorf("unexpected cmd: %q", CommandText(entries[0]))
	}
	if CommandText(entries[2]) != "docker ps" {
		t.Errorf("unexpected cmd: %q", CommandText(entries[2]))
	}
}

func TestParseBashPlain_SkipsBlankLines(t *testing.T) {
	input := "ls\n\npwd\n\n"
	entries := parseBashPlain(strings.NewReader(input))
	if len(entries) != 2 {
		t.Errorf("expected 2, got %d", len(entries))
	}
}

func TestParseBashPlain_NoTimestamps(t *testing.T) {
	entries := parseBashPlain(strings.NewReader(bashPlainHistory))
	for _, e := range entries {
		if e.Timestamp != 0 {
			t.Errorf("expected zero timestamp for plain entry, got %d", e.Timestamp)
		}
	}
}

func TestParseBashPlain_RawPreserved(t *testing.T) {
	entries := parseBashPlain(strings.NewReader("git status\n"))
	if len(entries) != 1 {
		t.Fatalf("expected 1, got %d", len(entries))
	}
	if entries[0].Raw[0] != "git status" {
		t.Errorf("unexpected raw: %q", entries[0].Raw[0])
	}
}

func TestParseBashPlain_Empty(t *testing.T) {
	entries := parseBashPlain(strings.NewReader(""))
	if len(entries) != 0 {
		t.Errorf("expected 0, got %d", len(entries))
	}
}

const bashPlainLithistHistory = `for i in 1 2 3 \
do \
  echo $i \
done
ls -la
`

func TestParseBashPlain_LithistMultiLine(t *testing.T) {
	entries := parseBashPlain(strings.NewReader(bashPlainLithistHistory))
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	wantCmd := "for i in 1 2 3 \\\ndo \\\n  echo $i \\\ndone"
	if got := CommandText(entries[0]); got != wantCmd {
		t.Errorf("entry[0] command text:\ngot:  %q\nwant: %q", got, wantCmd)
	}
	if len(entries[0].Raw) != 4 {
		t.Errorf("entry[0] should have 4 raw lines, got %d", len(entries[0].Raw))
	}
	if got := CommandText(entries[1]); got != "ls -la" {
		t.Errorf("entry[1] command text: got %q, want %q", got, "ls -la")
	}
}

func TestParseBashPlain_LithistRoundTrip(t *testing.T) {
	path := writeTempHistory(t, bashPlainLithistHistory)
	entries, err := ParseHistoryFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteHistoryFile(path, entries); err != nil {
		t.Fatal(err)
	}
	entries2, err := ParseHistoryFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(entries2) {
		t.Fatalf("entry count mismatch after round-trip: %d vs %d", len(entries), len(entries2))
	}
	for i := range entries {
		if CommandText(entries[i]) != CommandText(entries2[i]) {
			t.Errorf("entry[%d] changed after round-trip:\nbefore: %q\nafter:  %q",
				i, CommandText(entries[i]), CommandText(entries2[i]))
		}
	}
}

func TestParseBashPlain_BlankLineBreaksContinuation(t *testing.T) {
	// Blank line between two commands must prevent the second from being
	// absorbed into the first even though the first ends with '\'.
	input := "cmd1 \\\n\ncmd2\n"
	entries := parseBashPlain(strings.NewReader(input))
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if got := CommandText(entries[0]); got != `cmd1 \` {
		t.Errorf("entry[0]: got %q, want %q", got, `cmd1 \`)
	}
	if got := CommandText(entries[1]); got != "cmd2" {
		t.Errorf("entry[1]: got %q, want %q", got, "cmd2")
	}
}

func TestParseBashPlain_SingleLineContinuationOnly(t *testing.T) {
	// A file consisting of a single backslash-only line should produce one entry.
	entries := parseBashPlain(strings.NewReader("\\\n"))
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if got := CommandText(entries[0]); got != `\` {
		t.Errorf("got %q, want %q", got, `\`)
	}
}

func TestParseBashPlain_NoContinuationRegression(t *testing.T) {
	// Files without any backslash-terminated lines must behave identically
	// to the old implementation: one entry per non-blank line.
	entries := parseBashPlain(strings.NewReader(bashPlainHistory))
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
}

// parseBashTimestamped

func TestParseBashTimestamped_Basic(t *testing.T) {
	entries := parseBashTimestamped(strings.NewReader(bashTimestampedHistory))
	if len(entries) != 3 {
		t.Fatalf("expected 3, got %d", len(entries))
	}
	if entries[0].Timestamp != 1704067200 {
		t.Errorf("unexpected ts: %d", entries[0].Timestamp)
	}
	if CommandText(entries[0]) != "ls -la" {
		t.Errorf("unexpected cmd: %q", CommandText(entries[0]))
	}
}

func TestParseBashTimestamped_MultiLine(t *testing.T) {
	entries := parseBashTimestamped(strings.NewReader(bashTimestampedMultiLine))
	if len(entries) != 2 {
		t.Fatalf("expected 2, got %d", len(entries))
	}
	cmd := CommandText(entries[0])
	for _, want := range []string{"for i in 1 2 3", "do", "done"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("multi-line cmd missing %q, got: %q", want, cmd)
		}
	}
	if CommandText(entries[1]) != "pwd" {
		t.Errorf("unexpected second cmd: %q", CommandText(entries[1]))
	}
}

func TestParseBashTimestamped_LinesBeforeFirstTimestampIgnored(t *testing.T) {
	input := "ignored line\n#1704067200\nls\n"
	entries := parseBashTimestamped(strings.NewReader(input))
	if len(entries) != 1 {
		t.Fatalf("expected 1, got %d", len(entries))
	}
	if CommandText(entries[0]) != "ls" {
		t.Errorf("unexpected cmd: %q", CommandText(entries[0]))
	}
}

func TestParseBashTimestamped_RawIncludesTimestampLine(t *testing.T) {
	input := "#1704067200\nls -la\n"
	entries := parseBashTimestamped(strings.NewReader(input))
	if len(entries) != 1 {
		t.Fatalf("expected 1, got %d", len(entries))
	}
	if entries[0].Raw[0] != "#1704067200" {
		t.Errorf("Raw[0] should be timestamp line, got %q", entries[0].Raw[0])
	}
	if entries[0].Raw[1] != "ls -la" {
		t.Errorf("Raw[1] should be command, got %q", entries[0].Raw[1])
	}
}

func TestParseBashTimestamped_Empty(t *testing.T) {
	entries := parseBashTimestamped(strings.NewReader(""))
	if len(entries) != 0 {
		t.Errorf("expected 0, got %d", len(entries))
	}
}

// CommandText - bash formats

func TestCommandText_BashPlain(t *testing.T) {
	e := &Entry{Raw: []string{"git status"}}
	if got := CommandText(e); got != "git status" {
		t.Errorf("got %q, want %q", got, "git status")
	}
}

func TestCommandText_BashTimestamped(t *testing.T) {
	e := &Entry{Timestamp: 1704067200, Raw: []string{"#1704067200", "ls -la"}}
	if got := CommandText(e); got != "ls -la" {
		t.Errorf("got %q, want %q", got, "ls -la")
	}
}

func TestCommandText_BashTimestampedMultiLine(t *testing.T) {
	e := &Entry{
		Timestamp: 1704067200,
		Raw:       []string{"#1704067200", "for i in 1 2 3", "do", "  echo $i", "done"},
	}
	want := "for i in 1 2 3\ndo\n  echo $i\ndone"
	if got := CommandText(e); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCommandText_BashTimestampedNoCommand(t *testing.T) {
	e := &Entry{Timestamp: 1704067200, Raw: []string{"#1704067200"}}
	if got := CommandText(e); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// ParseHistoryFile - format dispatch

func TestParseHistoryFile_DetectsZsh(t *testing.T) {
	path := writeTempHistory(t, singleLineHistory)
	entries, err := ParseHistoryFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3, got %d", len(entries))
	}
	if entries[0].Timestamp != 1000 {
		t.Errorf("expected zsh timestamp 1000, got %d", entries[0].Timestamp)
	}
}

func TestParseHistoryFile_DetectsBashTimestamped(t *testing.T) {
	path := writeTempHistory(t, bashTimestampedHistory)
	entries, err := ParseHistoryFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3, got %d", len(entries))
	}
	if entries[0].Timestamp != 1704067200 {
		t.Errorf("expected bash timestamp 1704067200, got %d", entries[0].Timestamp)
	}
}

func TestParseHistoryFile_DetectsBashPlain(t *testing.T) {
	path := writeTempHistory(t, bashPlainHistory)
	entries, err := ParseHistoryFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3, got %d", len(entries))
	}
	if entries[0].Timestamp != 0 {
		t.Errorf("expected zero timestamp for plain bash, got %d", entries[0].Timestamp)
	}
}

// hasTimestamps

func TestHasTimestamps_True(t *testing.T) {
	entries := makeEntries(1000, 2000)
	if !hasTimestamps(entries) {
		t.Error("expected hasTimestamps=true")
	}
}

func TestHasTimestamps_False(t *testing.T) {
	entries := []*Entry{{Raw: []string{"ls"}}, {Raw: []string{"pwd"}}}
	if hasTimestamps(entries) {
		t.Error("expected hasTimestamps=false")
	}
}

func TestHasTimestamps_Empty(t *testing.T) {
	if hasTimestamps(nil) {
		t.Error("expected hasTimestamps=false for nil")
	}
}

// SelectEntries - timestamp requirement errors

func TestSelectEntries_NoTimestamp_NegDurationErrors(t *testing.T) {
	entries := []*Entry{{Raw: []string{"ls"}}, {Raw: []string{"pwd"}}}
	_, err := SelectEntries(entries, "-1h")
	if err == nil {
		t.Error("expected error for duration selection on no-timestamp entries")
	}
}

func TestSelectEntries_NoTimestamp_PosDurationErrors(t *testing.T) {
	entries := []*Entry{{Raw: []string{"ls"}}}
	_, err := SelectEntries(entries, "1h")
	if err == nil {
		t.Error("expected error for positive duration selection on no-timestamp entries")
	}
}

func TestSelectEntries_NoTimestamp_DateErrors(t *testing.T) {
	entries := []*Entry{{Raw: []string{"ls"}}}
	_, err := SelectEntries(entries, "2024-01-15")
	if err == nil {
		t.Error("expected error for date selection on no-timestamp entries")
	}
}

func TestSelectEntries_NoTimestamp_DateRangeErrors(t *testing.T) {
	entries := []*Entry{{Raw: []string{"ls"}}}
	_, err := SelectEntries(entries, "2024-01-01..2024-01-31")
	if err == nil {
		t.Error("expected error for date-range selection on no-timestamp entries")
	}
}

func TestSelectEntries_NoTimestamp_IntegerOK(t *testing.T) {
	entries := []*Entry{
		{Raw: []string{"ls"}},
		{Raw: []string{"pwd"}},
		{Raw: []string{"cd /tmp"}},
	}
	result, err := SelectEntries(entries, "-2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2, got %d", len(result))
	}
}

// defaultHistoryFilePath

func TestDefaultHistoryFilePath_UsesHISTFILE(t *testing.T) {
	t.Setenv("HISTFILE", "/custom/history")
	if got := defaultHistoryFilePath(); got != "/custom/history" {
		t.Errorf("got %q, want /custom/history", got)
	}
}

func TestDefaultHistoryFilePath_BashFromSHELL(t *testing.T) {
	t.Setenv("HISTFILE", "")
	t.Setenv("SHELL", "/bin/bash")
	got := defaultHistoryFilePath()
	if !strings.HasSuffix(got, ".bash_history") {
		t.Errorf("expected .bash_history suffix, got %q", got)
	}
}

func TestDefaultHistoryFilePath_ZshFromSHELL(t *testing.T) {
	t.Setenv("HISTFILE", "")
	t.Setenv("SHELL", "/bin/zsh")
	got := defaultHistoryFilePath()
	if !strings.HasSuffix(got, ".zsh_history") {
		t.Errorf("expected .zsh_history suffix, got %q", got)
	}
}

func TestDefaultHistoryFilePath_UnknownShellDefaultsToZsh(t *testing.T) {
	t.Setenv("HISTFILE", "")
	t.Setenv("SHELL", "/bin/sh")
	got := defaultHistoryFilePath()
	if !strings.HasSuffix(got, ".zsh_history") {
		t.Errorf("expected .zsh_history for unknown shell, got %q", got)
	}
}

func TestDefaultHistoryFilePath_HISTFILETakesPrecedenceOverSHELL(t *testing.T) {
	t.Setenv("HISTFILE", "/my/custom/hist")
	t.Setenv("SHELL", "/bin/bash")
	if got := defaultHistoryFilePath(); got != "/my/custom/hist" {
		t.Errorf("HISTFILE should take precedence, got %q", got)
	}
}

// WriteHistoryFile - bash format round-trips

func TestWriteHistoryFile_RoundTrip_BashTimestamped(t *testing.T) {
	path := writeTempHistory(t, bashTimestampedHistory)
	entries, err := ParseHistoryFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteHistoryFile(path, entries); err != nil {
		t.Fatal(err)
	}
	entries2, err := ParseHistoryFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries2) != len(entries) {
		t.Fatalf("round-trip: got %d entries, want %d", len(entries2), len(entries))
	}
	for i := range entries {
		if entries[i].String() != entries2[i].String() {
			t.Errorf("entry[%d] mismatch:\n  original: %q\n  got:      %q", i, entries[i].String(), entries2[i].String())
		}
	}
}

func TestWriteHistoryFile_RoundTrip_BashPlain(t *testing.T) {
	path := writeTempHistory(t, bashPlainHistory)
	entries, err := ParseHistoryFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteHistoryFile(path, entries); err != nil {
		t.Fatal(err)
	}
	entries2, err := ParseHistoryFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries2) != len(entries) {
		t.Fatalf("round-trip: got %d entries, want %d", len(entries2), len(entries))
	}
	for i := range entries {
		if CommandText(entries[i]) != CommandText(entries2[i]) {
			t.Errorf("entry[%d] cmd mismatch: %q vs %q", i, CommandText(entries[i]), CommandText(entries2[i]))
		}
	}
}

// fish history fixtures

const fishHistory = `- cmd: echo hello
  when: 1609459200
- cmd: git status
  when: 1609459260
- cmd: docker ps
  when: 1609459320
`

const fishHistoryMultiLine = `- cmd: for i in (seq 3)\n    echo $i\nend
  when: 1609459200
- cmd: ls -la
  when: 1609459260
`

const fishHistoryWithPaths = `- cmd: cd /tmp
  when: 1609459200
  paths:
    - /tmp
- cmd: ls
  when: 1609459260
`

// detectFormat - fish

func TestDetectFormat_Fish(t *testing.T) {
	if got := detectFormat(strings.NewReader(fishHistory)); got != formatFish {
		t.Errorf("expected formatFish, got %v", got)
	}
}

// parseFishHistory

func TestParseFishHistory_Basic(t *testing.T) {
	entries := parseFishHistory(strings.NewReader(fishHistory))
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].Timestamp != 1609459200 {
		t.Errorf("entry[0] timestamp = %d, want 1609459200", entries[0].Timestamp)
	}
	if entries[2].Timestamp != 1609459320 {
		t.Errorf("entry[2] timestamp = %d, want 1609459320", entries[2].Timestamp)
	}
}

func TestParseFishHistory_CommandText(t *testing.T) {
	entries := parseFishHistory(strings.NewReader(fishHistory))
	if CommandText(entries[0]) != "echo hello" {
		t.Errorf("entry[0] cmd = %q, want %q", CommandText(entries[0]), "echo hello")
	}
	if CommandText(entries[1]) != "git status" {
		t.Errorf("entry[1] cmd = %q, want %q", CommandText(entries[1]), "git status")
	}
}

func TestParseFishHistory_MultiLineCmd(t *testing.T) {
	entries := parseFishHistory(strings.NewReader(fishHistoryMultiLine))
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	want := "for i in (seq 3)\n    echo $i\nend"
	if got := CommandText(entries[0]); got != want {
		t.Errorf("multi-line cmd = %q, want %q", got, want)
	}
}

func TestParseFishHistory_RawPreserved(t *testing.T) {
	entries := parseFishHistory(strings.NewReader(fishHistory))
	if entries[0].Raw[0] != "- cmd: echo hello" {
		t.Errorf("raw[0] not preserved: %q", entries[0].Raw[0])
	}
	if entries[0].Raw[1] != "  when: 1609459200" {
		t.Errorf("raw[1] not preserved: %q", entries[0].Raw[1])
	}
}

func TestParseFishHistory_WithPaths(t *testing.T) {
	entries := parseFishHistory(strings.NewReader(fishHistoryWithPaths))
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: paths section should not start a new entry", len(entries))
	}
	if CommandText(entries[0]) != "cd /tmp" {
		t.Errorf("entry[0] cmd = %q, want %q", CommandText(entries[0]), "cd /tmp")
	}
}

func TestParseFishHistory_NoWhen(t *testing.T) {
	input := "- cmd: ls -la\n- cmd: pwd\n"
	entries := parseFishHistory(strings.NewReader(input))
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Timestamp != 0 {
		t.Errorf("expected zero timestamp when no when: line, got %d", entries[0].Timestamp)
	}
}

func TestParseFishHistory_Empty(t *testing.T) {
	entries := parseFishHistory(strings.NewReader(""))
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

// CommandText - fish

func TestCommandText_Fish(t *testing.T) {
	e := &Entry{Timestamp: 1609459200, Raw: []string{"- cmd: echo hello", "  when: 1609459200"}}
	if got := CommandText(e); got != "echo hello" {
		t.Errorf("got %q, want %q", got, "echo hello")
	}
}

func TestCommandText_Fish_MultiLine(t *testing.T) {
	e := &Entry{
		Timestamp: 1609459200,
		Raw:       []string{`- cmd: for i in (seq 3)\n    echo $i\nend`, "  when: 1609459200"},
	}
	want := "for i in (seq 3)\n    echo $i\nend"
	if got := CommandText(e); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// ParseHistoryFile - fish format dispatch

func TestParseHistoryFile_DetectsFish(t *testing.T) {
	path := writeTempHistory(t, fishHistory)
	entries, err := ParseHistoryFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3, got %d", len(entries))
	}
	if entries[0].Timestamp != 1609459200 {
		t.Errorf("expected fish timestamp 1609459200, got %d", entries[0].Timestamp)
	}
}

// WriteHistoryFile - fish round-trip

func TestWriteHistoryFile_RoundTrip_Fish(t *testing.T) {
	path := writeTempHistory(t, fishHistory)
	entries, err := ParseHistoryFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteHistoryFile(path, entries); err != nil {
		t.Fatal(err)
	}
	entries2, err := ParseHistoryFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries2) != len(entries) {
		t.Fatalf("round-trip: got %d entries, want %d", len(entries2), len(entries))
	}
	for i := range entries {
		if CommandText(entries[i]) != CommandText(entries2[i]) {
			t.Errorf("entry[%d] cmd mismatch: %q vs %q", i, CommandText(entries[i]), CommandText(entries2[i]))
		}
	}
}
