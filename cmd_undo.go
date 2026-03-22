package main

import (
	"fmt"
	"os"
)

func runUndo(force, simulate bool, histFile string) {
	bakFile := histFile + ".bak"
	if _, err := os.Stat(bakFile); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "error: no backup found at %s\n", bakFile)
		os.Exit(1)
	}

	entries, err := ParseHistoryFile(bakFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading backup: %v\n", err)
		os.Exit(1)
	}

	if simulate {
		printEntries(entries)
		fmt.Printf("\n(simulate) Would restore %d entries from %s.\n", len(entries), bakFile)
		return
	}

	if !confirm(force, fmt.Sprintf("Restore %d entries from %s? [y/N] ", len(entries), bakFile)) {
		return
	}

	if err := RestoreBackup(histFile); err != nil {
		fmt.Fprintf(os.Stderr, "error restoring backup: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Restored %d entries from %s.\n", len(entries), bakFile)
}
