package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// binaryPath holds the path to the compiled shlog binary built by TestMain.
var binaryPath string

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "shlog-integ-*")
	if err != nil {
		panic("failed to create temp dir for binary: " + err.Error())
	}
	defer os.RemoveAll(tmp)

	binaryPath = filepath.Join(tmp, "shlog")
	if out, err := exec.Command("go", "build", "-o", binaryPath, ".").CombinedOutput(); err != nil {
		panic("failed to build binary: " + string(out))
	}

	os.Exit(m.Run())
}

// runGoshist runs the compiled binary with the given arguments and returns
// combined stdout+stderr output and any error.
func runGoshist(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(binaryPath, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestDryRunFlag verifies that --dry-run behaves identically to -s:
// it prints what would be deleted without modifying the history file.
func TestDryRunFlag(t *testing.T) {
	hist := writeTempHistory(t, singleLineHistory) // 3 entries

	out, err := runGoshist(t, "--histfile", hist, "--dry-run", "del", "-1")
	if err != nil {
		t.Fatalf("command failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "(simulate)") {
		t.Errorf("expected simulate output, got: %s", out)
	}

	// file must be unchanged
	entries, parseErr := ParseHistoryFile(hist)
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	if len(entries) != 3 {
		t.Errorf("expected file unchanged (3 entries), got %d", len(entries))
	}
}

// TestDryRunSameAsSimulate verifies --dry-run and -s produce identical output.
func TestDryRunSameAsSimulate(t *testing.T) {
	hist1 := writeTempHistory(t, singleLineHistory)
	hist2 := writeTempHistory(t, singleLineHistory)

	outS, err := runGoshist(t, "--histfile", hist1, "-s", "del", "-1")
	if err != nil {
		t.Fatalf("-s failed: %v\noutput: %s", err, outS)
	}
	outDR, err := runGoshist(t, "--histfile", hist2, "--dry-run", "del", "-1")
	if err != nil {
		t.Fatalf("--dry-run failed: %v\noutput: %s", err, outDR)
	}
	if outS != outDR {
		t.Errorf("-s and --dry-run produced different output:\n  -s:        %q\n  --dry-run: %q", outS, outDR)
	}
}

// TestDryRunClean verifies --dry-run works with the clean subcommand.
func TestDryRunClean(t *testing.T) {
	content := `: 1000:0;ls
: 1001:0;pwd
: 1002:0;ls
`
	hist := writeTempHistory(t, content)

	out, err := runGoshist(t, "--histfile", hist, "--dry-run", "clean")
	if err != nil {
		t.Fatalf("command failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "(simulate)") {
		t.Errorf("expected simulate output, got: %s", out)
	}

	// file must be unchanged
	entries, _ := ParseHistoryFile(hist)
	if len(entries) != 3 {
		t.Errorf("expected file unchanged (3 entries), got %d", len(entries))
	}
}

// TestCompletionSubcommand_Zsh verifies the completion subcommand emits a
// recognisable zsh completion script.
func TestCompletionSubcommand_Zsh(t *testing.T) {
	out, err := runGoshist(t, "completion", "zsh")
	if err != nil {
		t.Fatalf("completion zsh failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "#compdef shlog") {
		t.Errorf("zsh completion missing #compdef header, got: %s", out)
	}
}

// TestCompletionSubcommand_Bash verifies the completion subcommand emits a
// recognisable bash completion script.
func TestCompletionSubcommand_Bash(t *testing.T) {
	out, err := runGoshist(t, "completion", "bash")
	if err != nil {
		t.Fatalf("completion bash failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "complete -F _shlog shlog") {
		t.Errorf("bash completion missing complete directive, got: %s", out)
	}
}


// TestCompletionSubcommand_UnknownShell verifies that an unknown shell exits
// with a non-zero status and prints a useful error.
func TestCompletionSubcommand_UnknownShell(t *testing.T) {
	out, err := runGoshist(t, "completion", "powershell")
	if err == nil {
		t.Fatal("expected non-zero exit for unknown shell")
	}
	if !strings.Contains(out, "powershell") {
		t.Errorf("error output should mention the unknown shell, got: %s", out)
	}
}

// TestCompletionSubcommand_NoArg verifies that omitting the shell argument
// exits with a non-zero status.
func TestCompletionSubcommand_NoArg(t *testing.T) {
	_, err := runGoshist(t, "completion")
	if err == nil {
		t.Fatal("expected non-zero exit when shell argument is missing")
	}
}

// ---- bash history integration tests ----

// TestBashPlain_List verifies that list works on a plain bash history file
// and shows "—" instead of a timestamp.
func TestBashPlain_List(t *testing.T) {
	hist := writeTempHistory(t, bashPlainHistory)
	out, err := runGoshist(t, "--histfile", hist, "list")
	if err != nil {
		t.Fatalf("list failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "ls -la") {
		t.Errorf("expected ls -la in output, got: %s", out)
	}
	if !strings.Contains(out, "—") {
		t.Errorf("expected — placeholder for missing timestamp, got: %s", out)
	}
}

// TestBashPlain_Grep verifies that grep works on a plain bash history file.
func TestBashPlain_Grep(t *testing.T) {
	hist := writeTempHistory(t, bashPlainHistory)
	out, err := runGoshist(t, "--histfile", hist, "grep", "git")
	if err != nil {
		t.Fatalf("grep failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "git status") {
		t.Errorf("expected git status in output, got: %s", out)
	}
	if strings.Contains(out, "ls -la") {
		t.Errorf("ls -la should not appear in grep git output: %s", out)
	}
}

// TestBashPlain_Stats verifies that stats omits the date range line for
// plain bash history (no timestamps).
func TestBashPlain_Stats(t *testing.T) {
	hist := writeTempHistory(t, bashPlainHistory)
	out, err := runGoshist(t, "--histfile", hist, "stats")
	if err != nil {
		t.Fatalf("stats failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Entries: 3") {
		t.Errorf("expected Entries: 3, got: %s", out)
	}
	if strings.Contains(out, "Date range") {
		t.Errorf("stats should not show date range for no-timestamp file, got: %s", out)
	}
}

// TestBashPlain_Del_Integer verifies that del with an integer selection works
// on a plain bash history file.
func TestBashPlain_Del_Integer(t *testing.T) {
	hist := writeTempHistory(t, bashPlainHistory) // 3 entries
	out, err := runGoshist(t, "--histfile", hist, "-f", "del", "-1")
	if err != nil {
		t.Fatalf("del failed: %v\noutput: %s", err, out)
	}
	entries, _ := ParseHistoryFile(hist)
	if len(entries) != 2 {
		t.Errorf("expected 2 entries after del -1, got %d", len(entries))
	}
}

// TestBashPlain_Del_TimeBased_Errors verifies that time-based selections
// return a clear error on plain bash history files.
func TestBashPlain_Del_TimeBased_Errors(t *testing.T) {
	hist := writeTempHistory(t, bashPlainHistory)
	out, err := runGoshist(t, "--histfile", hist, "del", "-1h")
	if err == nil {
		t.Fatal("expected non-zero exit for time-based selection on no-timestamp file")
	}
	if !strings.Contains(out, "timestamps") {
		t.Errorf("error should mention timestamps, got: %s", out)
	}
}

// TestBashPlain_Clean verifies that clean (dedup) works on a plain bash file.
func TestBashPlain_Clean(t *testing.T) {
	content := "ls\npwd\nls\n"
	hist := writeTempHistory(t, content)
	out, err := runGoshist(t, "--histfile", hist, "-f", "clean")
	if err != nil {
		t.Fatalf("clean failed: %v\noutput: %s", err, out)
	}
	entries, _ := ParseHistoryFile(hist)
	if len(entries) != 2 {
		t.Errorf("expected 2 entries after clean, got %d", len(entries))
	}
}

// TestBashTimestamped_List verifies that list shows real timestamps for bash
// timestamped history.
func TestBashTimestamped_List(t *testing.T) {
	hist := writeTempHistory(t, bashTimestampedHistory)
	out, err := runGoshist(t, "--histfile", hist, "list")
	if err != nil {
		t.Fatalf("list failed: %v\noutput: %s", err, out)
	}
	if strings.Contains(out, "—") {
		t.Errorf("should not show — placeholder for timestamped file, got: %s", out)
	}
	if !strings.Contains(out, "ls -la") {
		t.Errorf("expected ls -la in output, got: %s", out)
	}
}

// TestBashTimestamped_Del_Integer verifies del with an integer selection.
func TestBashTimestamped_Del_Integer(t *testing.T) {
	hist := writeTempHistory(t, bashTimestampedHistory) // 3 entries
	_, err := runGoshist(t, "--histfile", hist, "-f", "del", "-1")
	if err != nil {
		t.Fatalf("del failed: %v", err)
	}
	entries, _ := ParseHistoryFile(hist)
	if len(entries) != 2 {
		t.Errorf("expected 2 entries after del -1, got %d", len(entries))
	}
}

// TestCleanKeepOldest verifies that clean --keep-oldest keeps the first
// occurrence of each duplicate rather than the most recent.
func TestCleanKeepOldest(t *testing.T) {
	// ls appears at positions 1 and 3; with --keep-oldest, position 1 is kept
	content := `: 1000:0;ls
: 1001:0;pwd
: 1002:0;ls
`
	hist := writeTempHistory(t, content)
	out, err := runGoshist(t, "--histfile", hist, "-f", "clean", "--keep-oldest")
	if err != nil {
		t.Fatalf("clean --keep-oldest failed: %v\noutput: %s", err, out)
	}
	entries, _ := ParseHistoryFile(hist)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries after clean --keep-oldest, got %d", len(entries))
	}
	// first entry should be the oldest ls (ts=1000)
	if entries[0].Timestamp != 1000 {
		t.Errorf("expected oldest ls (ts=1000) to be kept, got ts=%d", entries[0].Timestamp)
	}
}

// TestCleanKeepOldest_vs_Default verifies that default clean keeps the newest
// while --keep-oldest keeps the oldest.
func TestCleanKeepOldest_vs_Default(t *testing.T) {
	content := `: 1000:0;ls
: 1001:0;pwd
: 1002:0;ls
`
	histDefault := writeTempHistory(t, content)
	histOldest := writeTempHistory(t, content)

	if _, err := runGoshist(t, "--histfile", histDefault, "-f", "clean"); err != nil {
		t.Fatalf("clean failed: %v", err)
	}
	if _, err := runGoshist(t, "--histfile", histOldest, "-f", "clean", "--keep-oldest"); err != nil {
		t.Fatalf("clean --keep-oldest failed: %v", err)
	}

	defaultEntries, _ := ParseHistoryFile(histDefault)
	oldestEntries, _ := ParseHistoryFile(histOldest)

	// default keeps most recent ls (ts=1002)
	lsDefault := defaultEntries[len(defaultEntries)-1]
	if lsDefault.Timestamp != 1002 {
		t.Errorf("default clean: expected ls at ts=1002 kept, got ts=%d", lsDefault.Timestamp)
	}
	// --keep-oldest keeps oldest ls (ts=1000)
	lsOldest := oldestEntries[0]
	if lsOldest.Timestamp != 1000 {
		t.Errorf("clean --keep-oldest: expected ls at ts=1000 kept, got ts=%d", lsOldest.Timestamp)
	}
}

// TestDelPick_NoFzf verifies that del --pick returns a clear error when fzf
// is not available.
func TestDelPick_NoFzf(t *testing.T) {
	if fzfAvailable() {
		t.Skip("fzf is installed; skipping no-fzf error test")
	}
	hist := writeTempHistory(t, singleLineHistory)
	out, err := runGoshist(t, "--histfile", hist, "del", "--pick")
	if err == nil {
		t.Fatal("expected non-zero exit when fzf is not available")
	}
	if !strings.Contains(out, "fzf") {
		t.Errorf("error should mention fzf, got: %s", out)
	}
}

// TestPick_NoFzf verifies that pick returns a clear error when fzf is not
// available.
func TestPick_NoFzf(t *testing.T) {
	if fzfAvailable() {
		t.Skip("fzf is installed; skipping no-fzf error test")
	}
	hist := writeTempHistory(t, singleLineHistory)
	out, err := runGoshist(t, "--histfile", hist, "pick")
	if err == nil {
		t.Fatal("expected non-zero exit when fzf is not available")
	}
	if !strings.Contains(out, "fzf") {
		t.Errorf("error should mention fzf, got: %s", out)
	}
}

// TestBashTimestamped_Undo verifies that undo restores a bash timestamped
// file correctly.
func TestBashTimestamped_Undo(t *testing.T) {
	hist := writeTempHistory(t, bashTimestampedHistory) // 3 entries
	// Delete one entry to create a backup
	if _, err := runGoshist(t, "--histfile", hist, "-f", "del", "-1"); err != nil {
		t.Fatalf("del failed: %v", err)
	}
	// Undo should restore 3 entries
	if _, err := runGoshist(t, "--histfile", hist, "-f", "undo"); err != nil {
		t.Fatalf("undo failed: %v", err)
	}
	entries, _ := ParseHistoryFile(hist)
	if len(entries) != 3 {
		t.Errorf("expected 3 entries after undo, got %d", len(entries))
	}
}

// ---- fish history integration tests ----

// TestFish_List verifies that list works on a fish history file and shows
// real timestamps.
func TestFish_List(t *testing.T) {
	hist := writeTempHistory(t, fishHistory)
	out, err := runGoshist(t, "--histfile", hist, "list")
	if err != nil {
		t.Fatalf("list failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "echo hello") {
		t.Errorf("expected 'echo hello' in output, got: %s", out)
	}
	if strings.Contains(out, "—") {
		t.Errorf("should not show — placeholder for fish history (has timestamps), got: %s", out)
	}
}

// TestFish_Grep verifies that grep works on a fish history file.
func TestFish_Grep(t *testing.T) {
	hist := writeTempHistory(t, fishHistory)
	out, err := runGoshist(t, "--histfile", hist, "grep", "git")
	if err != nil {
		t.Fatalf("grep failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "git status") {
		t.Errorf("expected 'git status' in output, got: %s", out)
	}
	if strings.Contains(out, "echo hello") {
		t.Errorf("'echo hello' should not appear in grep git output: %s", out)
	}
}

// TestFish_Del_Integer verifies that del with an integer selection works on
// a fish history file.
func TestFish_Del_Integer(t *testing.T) {
	hist := writeTempHistory(t, fishHistory) // 3 entries
	_, err := runGoshist(t, "--histfile", hist, "-f", "del", "-1")
	if err != nil {
		t.Fatalf("del failed: %v", err)
	}
	entries, _ := ParseHistoryFile(hist)
	if len(entries) != 2 {
		t.Errorf("expected 2 entries after del -1, got %d", len(entries))
	}
}

// TestFish_Clean verifies that deduplication works on a fish history file.
func TestFish_Clean(t *testing.T) {
	content := "- cmd: ls\n  when: 1609459200\n- cmd: pwd\n  when: 1609459260\n- cmd: ls\n  when: 1609459320\n"
	hist := writeTempHistory(t, content)
	_, err := runGoshist(t, "--histfile", hist, "-f", "clean")
	if err != nil {
		t.Fatalf("clean failed: %v", err)
	}
	entries, _ := ParseHistoryFile(hist)
	if len(entries) != 2 {
		t.Errorf("expected 2 entries after clean, got %d", len(entries))
	}
}

// TestFish_Stats verifies that stats shows a date range for fish history
// (which has timestamps).
func TestFish_Stats(t *testing.T) {
	hist := writeTempHistory(t, fishHistory)
	out, err := runGoshist(t, "--histfile", hist, "stats")
	if err != nil {
		t.Fatalf("stats failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Entries: 3") {
		t.Errorf("expected 'Entries: 3', got: %s", out)
	}
	if !strings.Contains(out, "Date range") {
		t.Errorf("expected date range for fish history (has timestamps), got: %s", out)
	}
}

// TestCompletionSubcommand_Fish verifies the completion subcommand emits a
// recognisable fish completion script.
func TestCompletionSubcommand_Fish(t *testing.T) {
	out, err := runGoshist(t, "completion", "fish")
	if err != nil {
		t.Fatalf("completion fish failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "complete -c shlog") {
		t.Errorf("fish completion missing 'complete -c shlog', got: %s", out)
	}
}
