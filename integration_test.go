package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// binaryPath holds the path to the compiled shlog binary built by TestMain.
var binaryPath string

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	tmp, err := os.MkdirTemp("", "shlog-integ-*")
	if err != nil {
		panic("failed to create temp dir for binary: " + err.Error())
	}
	defer os.RemoveAll(tmp)

	binaryPath = filepath.Join(tmp, "shlog")
	if out, err := exec.Command("go", "build", "-o", binaryPath, ".").CombinedOutput(); err != nil {
		panic("failed to build binary: " + string(out))
	}

	return m.Run()
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

func TestBashMixedHistory_CleanPreservesEarlierCommands(t *testing.T) {
	content := "old command\n#1704067200\nls\n#1704067300\nls\n"
	hist := writeTempHistory(t, content)
	if out, err := runGoshist(t, "--histfile", hist, "-f", "clean"); err != nil {
		t.Fatalf("clean failed: %v\n%s", err, out)
	}
	written, err := os.ReadFile(hist)
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != "old command\n#1704067300\nls\n" {
		t.Errorf("mixed history was changed unexpectedly: %q", written)
	}
	stats, err := runGoshist(t, "--histfile", hist, "stats")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stats, "Date range: 2024-01-01 to 2024-01-01") {
		t.Errorf("stats should show range from timestamped entries: %s", stats)
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

func TestDelPickRequiresCapableTerminal(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("TERM", "dumb")
	hist := writeTempHistory(t, singleLineHistory)
	out, err := runGoshist(t, "--histfile", hist, "del", "--pick")
	if err == nil {
		t.Fatal("expected an error for an incapable terminal")
	}
	if !strings.Contains(out, "terminal") {
		t.Errorf("error should mention the terminal, got: %s", out)
	}
}

func TestPickRequiresCapableTerminal(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("TERM", "dumb")
	hist := writeTempHistory(t, singleLineHistory)
	out, err := runGoshist(t, "--histfile", hist, "pick")
	if err == nil {
		t.Fatal("expected an error for an incapable terminal")
	}
	if !strings.Contains(out, "terminal") {
		t.Errorf("error should mention the terminal, got: %s", out)
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

func TestUndoOutputOverridesDryRunWithoutWriting(t *testing.T) {
	hist := writeTempHistory(t, ": 1000:0;current\n")
	backup := ": 900:0;previous\n"
	if err := os.WriteFile(hist+".bak", []byte(backup), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := runGoshist(t, "--histfile", hist, "-s", "-o", "undo")
	if err != nil {
		t.Fatalf("undo output failed: %v\n%s", err, out)
	}
	if out != backup {
		t.Errorf("output = %q, want exact backup content %q", out, backup)
	}
	current, err := os.ReadFile(hist)
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != ": 1000:0;current\n" {
		t.Errorf("history changed during output: %q", current)
	}
	stillBackup, err := os.ReadFile(hist + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(stillBackup) != backup {
		t.Errorf("backup changed during output: %q", stillBackup)
	}
}

func TestDelEmptyRegexMatchesAllEntries(t *testing.T) {
	hist := writeTempHistory(t, singleLineHistory)
	out, err := runGoshist(t, "--histfile", hist, "-o", "del", "--match", "")
	if err != nil {
		t.Fatalf("empty regex should be valid: %v\n%s", err, out)
	}
	if out != "" {
		t.Errorf("deleting every entry should produce empty output, got %q", out)
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

func TestOutputOverridesDryRun(t *testing.T) {
	for _, tt := range []struct {
		name, content, want string
		args                []string
	}{
		{"del", "ls\npwd\nls\n", "ls\npwd\n", []string{"del", "-1"}},
		{"clean", "ls\npwd\nls\n", "pwd\nls\n", []string{"clean"}},
		{"del no match", singleLineHistory, singleLineHistory, []string{"del", "--match", "never matches"}},
		{"clean no duplicates", singleLineHistory, singleLineHistory, []string{"clean"}},
		{"del empty", "", "", []string{"del", "-1"}},
		{"clean empty", "", "", []string{"clean"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			hist := writeTempHistory(t, tt.content)
			if err := os.WriteFile(hist+".bak", []byte("previous backup\n"), 0600); err != nil {
				t.Fatal(err)
			}
			args := append([]string{"--histfile", hist, "-s", "-o"}, tt.args...)
			out, err := runGoshist(t, args...)
			if err != nil {
				t.Fatalf("command failed: %v\n%s", err, out)
			}
			if out != tt.want {
				t.Fatalf("output = %q, want %q", out, tt.want)
			}
			assertFileContent(t, hist, tt.content)
			assertFileContent(t, hist+".bak", "previous backup\n")
		})
	}
}

func TestInvalidArgumentsDoNotWrite(t *testing.T) {
	for _, args := range [][]string{
		{"del"}, {"del", "1", "2"}, {"del", "--match"},
		{"del", "--invert", "1"}, {"del", "--pick", "--match", "ls"},
		{"del", "--match", "ls", "1"}, {"del", "--match", "["},
		{"clean", "1", "2"}, {"clean", "--keep-oldest", "1", "2"},
		{"list", "1", "2"}, {"grep"}, {"grep", "["}, {"grep", "ls", "1", "2"},
		{"stats", "1", "2"}, {"pick", "--multi", "1", "2"},
		{"undo", "1"}, {"version", "extra"}, {"completion", "bash", "extra"},
		{"list", "0"}, {"list", "2024-02-01..2024-01-01"},
		{"bogus"}, {"help", "extra"}, {"--histfile"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			hist := writeTempHistory(t, singleLineHistory)
			out, err := runGoshist(t, append([]string{"--histfile", hist, "-f"}, args...)...)
			if err == nil || !strings.Contains(out, "error:") {
				t.Fatalf("expected an argument error, got %v\n%s", err, out)
			}
			assertFileContent(t, hist, singleLineHistory)
			assertNoBackup(t, hist)
		})
	}
}

func TestConfirmation(t *testing.T) {
	for _, command := range []string{"del", "clean", "undo"} {
		for _, answer := range []string{"n\n", "", " YES \n"} {
			t.Run(command+"/"+strings.TrimSpace(answer), func(t *testing.T) {
				original := "ls\npwd\nls\n"
				hist := writeTempHistory(t, original)
				args := []string{"--histfile", hist, command}
				want := "pwd\nls\n"
				if command == "del" {
					args = append(args, "-1")
					want = "ls\npwd\n"
				} else if command == "undo" {
					want = "backup\n"
					if err := os.WriteFile(hist+".bak", []byte(want), 0600); err != nil {
						t.Fatal(err)
					}
				}
				cmd := exec.Command(binaryPath, args...)
				cmd.Stdin = strings.NewReader(answer)
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("confirmation: %v\n%s", err, out)
				}
				if !strings.Contains(string(out), "[y/N]") {
					t.Fatalf("confirmation prompt missing: %s", out)
				}
				if answer == " YES \n" {
					assertFileContent(t, hist, want)
					assertFileContent(t, hist+".bak", original)
				} else {
					assertFileContent(t, hist, original)
					if command == "undo" {
						assertFileContent(t, hist+".bak", want)
					} else {
						assertNoBackup(t, hist)
					}
				}
			})
		}
	}
}

func TestHistoryChangedDuringConfirmation(t *testing.T) {
	for _, args := range [][]string{{"del", "-1"}, {"clean"}} {
		t.Run(args[0], func(t *testing.T) {
			original := "ls\npwd\nls\n"
			hist := writeTempHistory(t, original)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, binaryPath, append([]string{"--histfile", hist}, args...)...)
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { cancel(); cmd.Wait() })
			if _, err := bufio.NewReader(stdout).ReadString('?'); err != nil {
				t.Fatalf("waiting for confirmation prompt: %v", err)
			}
			updated := original + "appended by shell\n"
			if err := os.WriteFile(hist, []byte(updated), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := io.WriteString(stdin, "yes\n"); err != nil {
				t.Fatal(err)
			}
			stdin.Close()
			if _, err := io.Copy(io.Discard, stdout); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Wait(); err == nil || !strings.Contains(stderr.String(), "changed since it was read") {
				t.Fatalf("expected a history conflict: %v\n%s", err, &stderr)
			}
			assertFileContent(t, hist, updated)
			assertNoBackup(t, hist)
		})
	}
}

func TestCleanScoped(t *testing.T) {
	for _, keepOldest := range []bool{false, true} {
		t.Run(fmt.Sprint(keepOldest), func(t *testing.T) {
			original := ": 1000:0;ls\n: 1001:0;ls\n: 1002:0;pwd\n: 1003:0;ls\n"
			hist := writeTempHistory(t, original)
			args := []string{"--histfile", hist, "-f", "clean", "-3"}
			want := ": 1000:0;ls\n: 1002:0;pwd\n: 1003:0;ls\n"
			if keepOldest {
				args = append(args, "--keep-oldest")
				want = ": 1000:0;ls\n: 1001:0;ls\n: 1002:0;pwd\n"
			}
			if out, err := runGoshist(t, args...); err != nil {
				t.Fatalf("scoped clean: %v\n%s", err, out)
			}
			assertFileContent(t, hist, want)
			assertFileContent(t, hist+".bak", original)
		})
	}
}

func TestStatsBeforeUnixEpoch(t *testing.T) {
	t.Setenv("TZ", "UTC")
	hist := writeTempHistory(t, "- cmd: ls\n  when: -86400\n- cmd: pwd\n  when: -1\n")
	out, err := runGoshist(t, "--histfile", hist, "stats")
	if err != nil || !strings.Contains(out, "Date range: 1969-12-31 to 1969-12-31") {
		t.Fatalf("stats date range: %v\n%s", err, out)
	}
}

func TestDelMatchAndInvert(t *testing.T) {
	original := ": 1000:0;aws s3 ls\\\ncontinuation\n: 1001:0;pwd\n: 1002:0;aws ec2 help\n"
	for _, tt := range []struct {
		name, pattern, want string
		invert              bool
	}{
		{"match", "^aws ", ": 1001:0;pwd\n", false},
		{"invert", "^aws ", ": 1000:0;aws s3 ls\\\ncontinuation\n: 1002:0;aws ec2 help\n", true},
		{"empty", "", "", false},
		{"invert empty", "", original, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			hist := writeTempHistory(t, original)
			args := []string{"--histfile", hist, "-f", "del", "--match", tt.pattern}
			if tt.invert {
				args = append(args, "--invert")
			}
			if out, err := runGoshist(t, args...); err != nil {
				t.Fatalf("del match: %v\n%s", err, out)
			}
			assertFileContent(t, hist, tt.want)
			if tt.want == original {
				assertNoBackup(t, hist)
			} else {
				assertFileContent(t, hist+".bak", original)
			}
		})
	}
}

func TestGrepScopedPreservesHistoryIndex(t *testing.T) {
	hist := writeTempHistory(t, "echo first\npwd\necho last\n")
	out, err := runGoshist(t, "--histfile", hist, "grep", "echo", "-2")
	if err != nil || out != "3  —  echo last\n" {
		t.Fatalf("scoped grep output = %q, err=%v", out, err)
	}
	assertNoBackup(t, hist)
}

func TestHistoryOutputReportsWriteFailure(t *testing.T) {
	for _, args := range [][]string{
		{"-o", "del", "-1"}, {"-o", "clean"},
		{"-s", "del", "-1"}, {"-s", "clean"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			original := "ls\npwd\nls\n"
			hist := writeTempHistory(t, original)
			readOnly, err := os.Open(hist)
			if err != nil {
				t.Fatal(err)
			}
			defer readOnly.Close()
			cmd := exec.Command(binaryPath, append([]string{"--histfile", hist}, args...)...)
			cmd.Stdout = readOnly
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Run(); err == nil || !strings.Contains(stderr.String(), "error writing output") {
				t.Fatalf("failed output was not reported: error=%v, stderr=%s", err, &stderr)
			}
			assertFileContent(t, hist, original)
			assertNoBackup(t, hist)
		})
	}
}

func TestHelpAndVersion(t *testing.T) {
	for _, args := range [][]string{{"-h"}, {"--help"}, {"help"}, {"-f", "--help", "del"}, {"version"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			cmd := exec.Command(binaryPath, args...)
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Run(); err != nil || stderr.Len() != 0 {
				t.Fatalf("%v: err=%v stderr=%q", args, err, stderr.String())
			}
			want := "Usage: shlog"
			if args[0] == "version" {
				want = "dev\n"
			}
			if !strings.HasPrefix(stdout.String(), want) {
				t.Fatalf("%v: stdout = %q, want prefix %q", args, stdout.String(), want)
			}
		})
	}
}

func TestZshEntriesWithoutTimestampsArePreserved(t *testing.T) {
	t.Setenv("TZ", "UTC")
	original := "old plain\n: 1000:0;timed\nlater plain\n"
	hist := writeTempHistory(t, original)
	out, err := runGoshist(t, "--histfile", hist, "list")
	if want := "1  —  old plain\n2  1970-01-01 00:16:40  timed\n3  —  later plain\n"; err != nil || out != want {
		t.Fatalf("list = %q, err=%v; want %q", out, err, want)
	}
	if out, err := runGoshist(t, "--histfile", hist, "-f", "del", "-1"); err != nil {
		t.Fatalf("del: %v\n%s", err, out)
	}
	assertFileContent(t, hist, "old plain\n: 1000:0;timed\n")
	assertFileContent(t, hist+".bak", original)
}
