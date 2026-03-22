package main

import (
	"fmt"
	"os"
	"regexp"
)

func runGrep(args []string, histFile string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "error: grep requires a pattern argument")
		os.Exit(1)
	}

	pattern := args[0]
	re, err := regexp.Compile(pattern)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid pattern %q: %v\n", pattern, err)
		os.Exit(1)
	}

	entries := loadHistory(histFile)

	// optional selection narrows the search window
	pool := entries
	if len(args) > 1 {
		pool, err = SelectEntries(entries, args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}

	matched := MatchEntriesRe(pool, re)

	if len(matched) == 0 {
		fmt.Fprintf(os.Stderr, "No entries matched. Total entries: %d.\n", len(entries))
		return
	}

	printIndexed(entries, matched, re)
}
