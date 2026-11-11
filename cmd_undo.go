package main

import (
	"fmt"
	"os"
)

func runUndo(force, simulate, output bool, histFile string) {
	bakFile := histFile + ".bak"
	backup, err := readHistoryBackup(bakFile)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "error: no backup found at %s\n", bakFile)
		} else {
			fmt.Fprintf(os.Stderr, "error reading backup: %v\n", err)
		}
		os.Exit(exitError)
	}
	if output {
		if _, err := os.Stdout.Write(backup.data); err != nil {
			fmt.Fprintf(os.Stderr, "error writing output: %v\n", err)
			os.Exit(exitError)
		}
		return
	}

	entries := parseHistoryData(backup.data)

	if simulate {
		printEntries(entries)
		printOutput("\n(simulate) Would restore %d entries from %s.\n", len(entries), bakFile)
		return
	}
	original, err := os.ReadFile(histFile)
	existed := err == nil
	if err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "error reading history: %v\n", err)
		os.Exit(exitError)
	}

	if !confirm(force, fmt.Sprintf("Restore %d entries from %s? [y/N] ", len(entries), bakFile)) {
		os.Exit(exitCanceled)
	}

	if err := restoreBackupIfUnchanged(histFile, original, existed, backup); err != nil {
		fmt.Fprintf(os.Stderr, "error restoring backup: %v\n", err)
		os.Exit(exitError)
	}
	printOutput("Restored %d entries from %s.\n", len(entries), bakFile)
}
