package main

import (
	"fmt"
	"os"
)

func runClean(args []string, force, simulate, output bool, histFile string) {
	entries, original := loadHistorySnapshot(histFile)

	// parse clean-specific flags
	keepOldest := false
	var selArgs []string
	literal := false
	for _, a := range args {
		if !literal && a == "--" {
			literal = true
		} else if !literal && a == "--keep-oldest" {
			keepOldest = true
		} else {
			selArgs = append(selArgs, a)
		}
	}

	// optional selection narrows which entries are considered for deduplication;
	// entries outside the scope are always kept
	scope, err := selectOptionalEntries(entries, selArgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(exitError)
	}

	remaining, removed := DeduplicateEntries(scope, keepOldest)
	if len(selArgs) > 0 {
		remaining = InvertEntries(entries, removed)
	}

	if output {
		printEntries(remaining)
		return
	}

	if len(removed) == 0 {
		printOutput("No duplicates found. Total entries: %d.\n", len(entries))
		return
	}

	if simulate {
		printEntries(removed)
		printOutput("\n(simulate) Would remove %d duplicate entries of %d total.\n", len(removed), len(entries))
		return
	}

	if !confirm(force, fmt.Sprintf("About to remove %d duplicate entries of %d total. Confirm? [y/N] ", len(removed), len(entries))) {
		os.Exit(exitCanceled)
	}

	writeHistory(histFile, original, remaining)
	printOutput("Removed %d duplicate entries. %d entries remaining.\n", len(removed), len(remaining))
}
