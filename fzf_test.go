package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func fakeFzf(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fzf"), []byte("#!/bin/sh\n"+script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func TestPickWithFzf(t *testing.T) {
	for _, multi := range []bool{false, true} {
		t.Run(fmt.Sprintf("multi=%t", multi), func(t *testing.T) {
			input := filepath.Join(t.TempDir(), "input")
			args := filepath.Join(t.TempDir(), "args")
			t.Setenv("SHLOG_TEST_FZF_INPUT", input)
			t.Setenv("SHLOG_TEST_FZF_ARGS", args)
			fakeFzf(t, `printf '%s\n' "$@" > "$SHLOG_TEST_FZF_ARGS"
/bin/cat > "$SHLOG_TEST_FZF_INPUT"
/bin/cat "$SHLOG_TEST_FZF_INPUT"
`)
			all := []*Entry{{Raw: []string{"hidden"}}, {Raw: []string{"first line", "second line"}}}
			got, err := pickWithFzf(all, all[1:], multi)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, all[1:]) {
				t.Fatal("selection did not map back to the original entry")
			}
			assertFileContent(t, input, "2  —  first line ↵\n")
			wantArgs := "--no-sort\n--reverse\n--height=40%\n"
			if multi {
				wantArgs += "--multi\n"
			}
			assertFileContent(t, args, wantArgs)
		})
	}
}

func TestPickWithFzfExitCodes(t *testing.T) {
	for _, code := range []int{1, 130, 2} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			fakeFzf(t, fmt.Sprintf("/bin/cat > /dev/null\nexit %d\n", code))
			all := makeEntries(1)
			got, err := pickWithFzf(all, all, false)
			if code == 2 {
				if err == nil || !strings.Contains(err.Error(), "fzf") {
					t.Fatalf("expected fzf failure, got %v", err)
				}
			} else if err != nil || len(got) != 0 {
				t.Fatalf("exit %d: got %v, %v; want no selection or error", code, got, err)
			}
		})
	}
}

func TestPickWithFzfIgnoresInvalidAndRepeatedRows(t *testing.T) {
	fakeFzf(t, `IFS= read -r first
IFS= read -r second
printf 'garbage\n0\n-1\n999\n   \n2  wrong command\n'
printf '%s\n' "$second" "$second" "$first"
`)
	all := makeEntries(1, 2)
	got, err := pickWithFzf(all, all, true)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []*Entry{all[1], all[0]}) {
		t.Fatalf("unexpected selected indices: %v", timestampsOf(got))
	}
}

func TestPickWithFzfRejectsRowsOutsidePool(t *testing.T) {
	fakeFzf(t, `printf '1\n1  —  outside\n2  —  altered\n'
/bin/cat
`)
	all := []*Entry{{Raw: []string{"outside"}}, {Raw: []string{"inside"}}}
	got, err := pickWithFzf(all, all[1:], true)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, all[1:]) {
		t.Fatalf("fzf output selected entries outside the offered pool: %v", got)
	}
}

func TestPickWithFzfEmptyPool(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	got, err := pickWithFzf(makeEntries(1), nil, false)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty pool: got %v, %v", got, err)
	}
}

func TestPickWithFzfMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	all := makeEntries(1)
	if _, err := pickWithFzf(all, all, false); err == nil || !strings.Contains(err.Error(), "fzf not found") {
		t.Fatalf("missing fzf: got %v", err)
	}
}
