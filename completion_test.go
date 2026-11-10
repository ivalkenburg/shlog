package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestCompletionScript_Zsh(t *testing.T) {
	script, err := completionScript("zsh")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"#compdef shlog", "_shlog", "--dry-run", "--histfile", "del", "clean", "list", "grep", "stats", "undo", "completion"} {
		if !strings.Contains(script, want) {
			t.Errorf("zsh script missing %q", want)
		}
	}
}

func TestCompletionScript_Bash(t *testing.T) {
	script, err := completionScript("bash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"_shlog", "complete -F _shlog shlog", "--dry-run", "--histfile", "del", "clean", "list", "grep", "stats", "undo", "completion"} {
		if !strings.Contains(script, want) {
			t.Errorf("bash script missing %q", want)
		}
	}
}

func TestCompletionScript_UnknownShell(t *testing.T) {
	_, err := completionScript("powershell")
	if err == nil {
		t.Fatal("expected error for unknown shell")
	}
	if !strings.Contains(err.Error(), "powershell") {
		t.Errorf("error should mention the unknown shell name, got: %v", err)
	}
}

func TestCompletionScript_EmptyShell(t *testing.T) {
	_, err := completionScript("")
	if err == nil {
		t.Fatal("expected error for empty shell")
	}
}

func TestCompletionScript_Fish(t *testing.T) {
	script, err := completionScript("fish")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"complete -c shlog", "dry-run", "histfile", "del", "clean", "list", "grep", "stats", "undo", "completion"} {
		if !strings.Contains(script, want) {
			t.Errorf("fish script missing %q", want)
		}
	}
}

func TestCompletionScript_AllShellsNonEmpty(t *testing.T) {
	for _, shell := range []string{"zsh", "bash", "fish"} {
		script, err := completionScript(shell)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", shell, err)
			continue
		}
		if len(strings.TrimSpace(script)) == 0 {
			t.Errorf("%s: script is empty", shell)
		}
	}
}

func TestBashCompletion(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "completion.bash")
	if err := os.WriteFile(script, []byte(bashCompletion), 0600); err != nil {
		t.Fatal(err)
	}
	hist := filepath.Join(dir, "history with spaces")
	if err := os.WriteFile(hist, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name  string
		words []string
		cword int
		want  string
	}{
		{"partial command", []string{"shlog", "gr"}, 1, "grep"},
		{"command after histfile", []string{"shlog", "--histfile", hist, "cl"}, 3, "clean"},
		{"command before later word", []string{"shlog", "li", "unused"}, 1, "list"},
		{"del flag", []string{"shlog", "-f", "del", "--ma"}, 3, "--match"},
		{"global flag before command", []string{"shlog", "--dry"}, 1, "--dry-run"},
		{"global flag after command", []string{"shlog", "del", "--dry"}, 2, "--dry-run"},
		{"unique", []string{"shlog", "pick", "--uni"}, 2, "--unique"},
		{"sort", []string{"shlog", "pick", "--so"}, 2, "--sort"},
		{"sort value", []string{"shlog", "pick", "--sort", "fr"}, 3, "frequency"},
		{"deletion sort value", []string{"shlog", "del", "--pick", "--sort", "re"}, 4, "relevance"},
		{"list global", []string{"shlog", "list", "--dry"}, 2, "--dry-run"},
		{"literal after delimiter", []string{"shlog", "pick", "--", "--so"}, 3, ""},
		{"literal histfile after delimiter", []string{"shlog", "grep", "--", "--histfile", filepath.Join(dir, "history")}, 4, ""},
		{"opaque match value", []string{"shlog", "del", "--match", "--histfile", filepath.Join(dir, "history")}, 4, ""},
		{"opaque match delimiter", []string{"shlog", "del", "--match", "--", "--dry"}, 4, "--dry-run"},
		{"pattern value no flags", []string{"shlog", "del", "--match", "--dry"}, 3, ""},
		{"inline histfile", []string{"shlog", "--histfile=" + hist, "pi"}, 2, "pick"},
		{"file after command", []string{"shlog", "pick", "--histfile", filepath.Join(dir, "history")}, 3, hist},
		{"shell", []string{"shlog", "completion", "fi"}, 2, "fish"},
		{"file with spaces", []string{"shlog", "--histfile", filepath.Join(dir, "history")}, 2, hist},
	} {
		t.Run(tt.name, func(t *testing.T) {
			args := []string{"--noprofile", "--norc", "-c", `
source "$1"
COMP_CWORD=$2
shift 2
COMP_WORDS=("$@")
_shlog
printf '%s\n' "${COMPREPLY[@]}"
`, "shlog-completion-test", script, strconv.Itoa(tt.cword)}
			out, err := exec.Command(bash, append(args, tt.words...)...).CombinedOutput()
			if err != nil {
				t.Fatalf("bash completion: %v\n%s", err, out)
			}
			if got := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n"); !slices.Equal(got, []string{tt.want}) {
				t.Fatalf("completions = %q, want %q", got, tt.want)
			}
		})
	}
}

// usageCommands returns the command names listed in the usage text.
func usageCommands(t *testing.T) []string {
	t.Helper()
	_, rest, _ := strings.Cut(usage, "Commands:\n")
	section, _, _ := strings.Cut(rest, "\n\n")
	var cmds []string
	for line := range strings.SplitSeq(section, "\n") {
		if f := strings.Fields(line); len(f) > 0 && !slices.Contains(cmds, f[0]) {
			cmds = append(cmds, f[0])
		}
	}
	if len(cmds) == 0 {
		t.Fatal("no commands found in usage")
	}
	return cmds
}

func TestCompletionsListEveryCommand(t *testing.T) {
	cmds := usageCommands(t)
	for _, cmd := range cmds {
		if !strings.Contains(zshCompletion, "'"+cmd+":") {
			t.Errorf("zsh completion missing command %q", cmd)
		}
		if !strings.Contains(fishCompletion, "-a "+cmd+" ") {
			t.Errorf("fish completion missing command %q", cmd)
		}
	}
	_, rest, _ := strings.Cut(bashCompletion, `local commands="`)
	bashCmds, _, _ := strings.Cut(rest, `"`)
	got := strings.Fields(bashCmds)
	slices.Sort(got)
	slices.Sort(cmds)
	if !slices.Equal(got, cmds) {
		t.Errorf("bash commands = %v, usage commands = %v", got, cmds)
	}
}

func TestZshCompletionSyntax(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh is not installed")
	}
	cmd := exec.Command(zsh, "-n")
	cmd.Stdin = strings.NewReader(zshCompletion)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("zsh -n: %v\n%s", err, out)
	}
}
