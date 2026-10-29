package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// pickWithFzf presents pool entries in an fzf selector and returns the ones
// the user selected. all is the full history used to compute 1-based indices.
// If multi is true, fzf is invoked with -m so multiple entries can be chosen.
// Returns nil, nil when the user cancels without selecting anything.
func pickWithFzf(all []*Entry, pool []*Entry, multi bool) ([]*Entry, error) {
	if len(pool) == 0 {
		return nil, nil
	}
	fzfPath, err := exec.LookPath("fzf")
	if err != nil {
		return nil, fmt.Errorf("fzf not found in PATH — install fzf to use --pick/pick (https://github.com/junegunn/fzf)")
	}

	var buf strings.Builder
	if err := writeIndexed(&buf, all, pool, nil, false); err != nil {
		return nil, err
	}
	// fzf defaults can print extra lines such as the query. Accept only exact
	// rows offered in this pool, so those lines cannot select other entries.
	byRow := make(map[string]*Entry, len(pool))
	input := buf.String()
	for _, e := range pool {
		row, rest, _ := strings.Cut(input, "\n")
		byRow[row] = e
		input = rest
	}

	fzfArgs := []string{"--no-sort", "--reverse", "--height=40%"}
	if multi {
		fzfArgs = append(fzfArgs, "--multi")
	}

	cmd := exec.Command(fzfPath, fzfArgs...)
	cmd.Stdin = strings.NewReader(buf.String())
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// 1 means no match; 130 means the user cancelled.
			if code := exitErr.ExitCode(); code == 1 || code == 130 {
				return nil, nil
			}
		}
		return nil, fmt.Errorf("fzf: %v", err)
	}

	var selected []*Entry
	for line := range strings.SplitSeq(string(out), "\n") {
		if e, ok := byRow[line]; ok {
			selected = append(selected, e)
			delete(byRow, line)
		}
	}
	return selected, nil
}
