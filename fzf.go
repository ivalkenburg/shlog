package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// fzfAvailable returns true if the fzf binary is found in PATH.
func fzfAvailable() bool {
	_, err := exec.LookPath("fzf")
	return err == nil
}

// pickWithFzf presents pool entries in an fzf selector and returns the ones
// the user selected. all is the full history used to compute 1-based indices.
// If multi is true, fzf is invoked with -m so multiple entries can be chosen.
// Returns nil, nil when the user cancels without selecting anything.
func pickWithFzf(all []*Entry, pool []*Entry, multi bool) ([]*Entry, error) {
	if !fzfAvailable() {
		return nil, fmt.Errorf("fzf not found in PATH — install fzf to use --pick/pick (https://github.com/junegunn/fzf)")
	}

	// build a 1-based index map for the full history
	indexOf := make(map[*Entry]int, len(all))
	for i, e := range all {
		indexOf[e] = i + 1
	}
	width := len(fmt.Sprintf("%d", len(all)))

	// format each entry as "index  timestamp  command" for fzf
	var buf bytes.Buffer
	for _, e := range pool {
		var ts string
		if e.Timestamp == 0 {
			ts = "—"
		} else {
			ts = e.Time().Format(time.DateTime)
		}
		cmd := CommandText(e)
		if idx := strings.Index(cmd, "\n"); idx != -1 {
			cmd = cmd[:idx] + " ↵"
		}
		fmt.Fprintf(&buf, "%*d  %s  %s\n", width, indexOf[e], ts, cmd)
	}

	fzfArgs := []string{"--no-sort", "--reverse", "--height=40%"}
	if multi {
		fzfArgs = append(fzfArgs, "--multi")
	}

	cmd := exec.Command("fzf", fzfArgs...)
	cmd.Stdin = &buf
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		// exit code 130: user pressed Ctrl-C or Esc (no selection made)
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 130 {
			return nil, nil
		}
		return nil, fmt.Errorf("fzf: %v", err)
	}

	// build a lookup from 1-based index → entry
	byIndex := make(map[int]*Entry, len(all))
	for _, e := range all {
		byIndex[indexOf[e]] = e
	}

	var selected []*Entry
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		// first whitespace-delimited field is the padded index number
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		idx, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		if e, ok := byIndex[idx]; ok {
			selected = append(selected, e)
		}
	}
	return selected, nil
}
