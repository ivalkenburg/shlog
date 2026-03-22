package main

import (
	"fmt"
	"os"
)

func runClean(args []string, force, simulate, output bool, histFile string) {
	entries := loadHistory(histFile)

	// parse clean-specific flags
	keepOldest := false
	var selArgs []string
	for _, a := range args {
		if a == "--keep-oldest" {
			keepOldest = true
		} else {
			selArgs = append(selArgs, a)
		}
	}

	// optional selection narrows which entries are considered for deduplication;
	// entries outside the scope are always kept
	scope := entries
	if len(selArgs) > 0 {
		var err error
		scope, err = SelectEntries(entries, selArgs[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}

	_, removed := DeduplicateEntries(scope, keepOldest)
	remaining := InvertEntries(entries, removed)

	if len(removed) == 0 {
		fmt.Printf("No duplicates found. Total entries: %d.\n", len(entries))
		return
	}

	if output {
		printEntries(remaining)
		return
	}

	if simulate {
		for _, e := range removed {
			fmt.Println(e.String())
		}
		fmt.Printf("\n(simulate) Would remove %d duplicate entries of %d total.\n", len(removed), len(entries))
		return
	}

	if !confirm(force, fmt.Sprintf("About to remove %d duplicate entries of %d total. Confirm? [y/N] ", len(removed), len(entries))) {
		return
	}

	writeHistory(histFile, remaining)
	fmt.Printf("Removed %d duplicate entries. %d entries remaining.\n", len(removed), len(remaining))
}
