package main

import (
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
