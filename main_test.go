package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestWriteIndexed(t *testing.T) {
	all := []*Entry{
		{Raw: []string{"hidden"}},
		{Raw: []string{"echo hello", "second line"}},
	}
	for _, color := range []bool{false, true} {
		var out bytes.Buffer
		if err := writeIndexed(&out, all, all[1:], regexp.MustCompile("hello"), color); err != nil {
			t.Fatal(err)
		}
		want := "2  —  echo hello ↵\n"
		if color {
			want = "2  " + ansiDim + "—" + ansiReset + "  echo " + ansiBold + ansiGreen + "hello" + ansiReset + " ↵\n"
		}
		if out.String() != want {
			t.Errorf("color=%t: output = %q, want %q", color, out.String(), want)
		}
	}
}

func TestWriteIndexedReturnsWriteError(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "closed")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	all := makeEntries(1)
	if err := writeIndexed(f, all, all, nil, false); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("writeIndexed error = %v, want closed-file error", err)
	}
}

func TestDefaultHistoryFilePathFish(t *testing.T) {
	t.Setenv("HISTFILE", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SHELL", "/usr/local/bin/fish")
	want := filepath.Join(os.Getenv("HOME"), ".local", "share", "fish", "fish_history")
	if got := defaultHistoryFilePath(); got != want {
		t.Errorf("history path = %q, want %q", got, want)
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s content = %q, want %q", path, data, want)
	}
}

func assertNoBackup(t *testing.T, hist string) {
	t.Helper()
	if _, err := os.Stat(hist + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("unexpected backup after a non-writing operation: %v", err)
	}
}

func TestColorDisabledForNonTerminal(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	hist := writeTempHistory(t, singleLineHistory)
	out, err := runGoshist(t, "--histfile", hist, "grep", "echo")
	if err != nil {
		t.Fatalf("grep: %v\n%s", err, out)
	}
	if strings.Contains(out, "\x1b[") {
		t.Fatalf("piped output contains ANSI escapes: %q", out)
	}
}
