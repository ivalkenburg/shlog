package main

import (
	"fmt"
	"os"
)

func runDel(args []string, force, simulate, output bool, histFile string) {
	// parse del-specific flags: --match <pattern>, --invert, --pick, or a plain selection
	var pattern, selection string
	invert := false
	pick := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--match":
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "error: --match requires a pattern argument")
				os.Exit(1)
			}
			pattern = args[i]
		case "--invert":
			invert = true
		case "--pick":
			pick = true
		default:
			selection = args[i]
		}
	}

	if !pick && pattern == "" && selection == "" {
		fmt.Fprintln(os.Stderr, "error: missing selection, --match, or --pick argument")
		printUsage()
		os.Exit(1)
	}
	if invert && pattern == "" {
		fmt.Fprintln(os.Stderr, "error: --invert requires --match")
		os.Exit(1)
	}
	if pick && pattern != "" {
		fmt.Fprintln(os.Stderr, "error: --pick and --match cannot be used together")
		os.Exit(1)
	}

	entries := loadHistory(histFile)

	var toDelete []*Entry
	if pick {
		pool := entries
		if selection != "" {
			var err error
			pool, err = SelectEntries(entries, selection)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
		}
		var err error
		toDelete, err = pickWithFzf(entries, pool, true)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if len(toDelete) == 0 {
			fmt.Println("No entries selected.")
			return
		}
	} else if pattern != "" {
		matched, err := MatchEntries(entries, pattern)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if invert {
			toDelete = InvertEntries(entries, matched)
		} else {
			toDelete = matched
		}
	} else {
		var err error
		toDelete, err = SelectEntries(entries, selection)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}

	remaining := InvertEntries(entries, toDelete)

	if len(toDelete) == 0 {
		fmt.Printf("No entries matched. Total entries: %d.\n", len(entries))
		return
	}

	if output {
		printEntries(remaining)
		return
	}

	if simulate {
		for _, e := range toDelete {
			fmt.Println(e.String())
		}
		fmt.Printf("\n(simulate) Would delete %d of %d entries.\n", len(toDelete), len(entries))
		return
	}

	if !confirm(force, fmt.Sprintf("About to delete %d of %d entries. Confirm? [y/N] ", len(toDelete), len(entries))) {
		return
	}

	writeHistory(histFile, remaining)
	fmt.Printf("Deleted %d entries. %d entries remaining.\n", len(toDelete), len(remaining))
}
