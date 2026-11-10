package main

import (
	"fmt"
	"os"
)

func runList(args []string, histFile string) {
	entries := loadHistory(histFile)

	toShow, err := selectOptionalEntries(entries, args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(exitError)
	}

	if len(toShow) == 0 {
		fmt.Fprintf(os.Stderr, "No entries matched. Total entries: %d.\n", len(entries))
		os.Exit(exitNoMatch)
	}

	printIndexed(entries, toShow, nil)
}
