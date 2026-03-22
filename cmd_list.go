package main

import (
	"fmt"
	"os"
)

func runList(args []string, histFile string) {
	entries := loadHistory(histFile)

	toShow := entries
	if len(args) > 0 {
		var err error
		toShow, err = SelectEntries(entries, args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}

	if len(toShow) == 0 {
		fmt.Fprintf(os.Stderr, "No entries matched. Total entries: %d.\n", len(entries))
		return
	}

	printIndexed(entries, toShow, nil)
}
