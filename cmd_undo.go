package main

import (
	"fmt"
	"os"
)

func runUndo(force, simulate, output bool, histFile string) {
	bakFile := histFile + ".bak"
	if _, err := os.Stat(bakFile); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "error: no backup found at %s\n", bakFile)
		os.Exit(exitError)
	}
	if output {
		data, err := os.ReadFile(bakFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading backup: %v\n", err)
			os.Exit(exitError)
		}
		if _, err := os.Stdout.Write(data); err != nil {
			fmt.Fprintf(os.Stderr, "error writing output: %v\n", err)
			os.Exit(exitError)
		}
		return
	}

	entries, err := ParseHistoryFile(bakFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading backup: %v\n", err)
		os.Exit(exitError)
	}

	if simulate {
		printEntries(entries)
		printOutput("\n(simulate) Would restore %d entries from %s.\n", len(entries), bakFile)
		return
	}

	if !confirm(force, fmt.Sprintf("Restore %d entries from %s? [y/N] ", len(entries), bakFile)) {
		os.Exit(exitCanceled)
	}

	if err := RestoreBackup(histFile); err != nil {
		fmt.Fprintf(os.Stderr, "error restoring backup: %v\n", err)
		os.Exit(exitError)
	}
	printOutput("Restored %d entries from %s.\n", len(entries), bakFile)
}
