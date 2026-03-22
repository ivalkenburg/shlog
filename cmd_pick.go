package main

import (
	"fmt"
	"os"
)

func runPick(args []string, histFile string) {
	multi := false
	var selArgs []string
	for _, a := range args {
		if a == "--multi" {
			multi = true
		} else {
			selArgs = append(selArgs, a)
		}
	}

	entries := loadHistory(histFile)

	pool := entries
	if len(selArgs) > 0 {
		var err error
		pool, err = SelectEntries(entries, selArgs[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}

	selected, err := pickWithFzf(entries, pool, multi)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	for _, e := range selected {
		fmt.Println(CommandText(e))
	}
}
