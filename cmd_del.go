package main

import (
	"fmt"
	"os"
)

func runDel(args []string, force, simulate, output bool, histFile string) {
	// parse del-specific flags: --match <pattern>, --invert, --pick, or a plain selection
	var pattern string
	var selections []string
	patternSet := false
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
			patternSet = true
		case "--invert":
			invert = true
		case "--pick":
			pick = true
		default:
			selections = append(selections, args[i])
		}
	}
	if len(selections) > 1 {
		fmt.Fprintln(os.Stderr, "error: del accepts at most one selection")
		os.Exit(1)
	}
	if !pick && !patternSet && len(selections) == 0 {
		fmt.Fprintln(os.Stderr, "error: missing selection, --match, or --pick argument")
		printUsage()
		os.Exit(1)
	}
	if invert && !patternSet {
		fmt.Fprintln(os.Stderr, "error: --invert requires --match")
		os.Exit(1)
	}
	if pick && patternSet {
		fmt.Fprintln(os.Stderr, "error: --pick and --match cannot be used together")
		os.Exit(1)
	}
	if patternSet && len(selections) != 0 {
		fmt.Fprintln(os.Stderr, "error: --match cannot be used with a selection")
		os.Exit(1)
	}

	entries, original := loadHistorySnapshot(histFile)

	var toDelete []*Entry
	if pick {
		pool, err := selectOptionalEntries(entries, selections)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		toDelete, err = pickWithFzf(entries, pool, true)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if len(toDelete) == 0 && !output {
			fmt.Println("No entries selected.")
			return
		}
	} else if patternSet {
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
		toDelete, err = selectOptionalEntries(entries, selections)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}

	remaining := InvertEntries(entries, toDelete)

	if output {
		printEntries(remaining)
		return
	}

	if len(toDelete) == 0 {
		fmt.Printf("No entries matched. Total entries: %d.\n", len(entries))
		return
	}

	if simulate {
		printEntries(toDelete)
		fmt.Printf("\n(simulate) Would delete %d of %d entries.\n", len(toDelete), len(entries))
		return
	}

	if !confirm(force, fmt.Sprintf("About to delete %d of %d entries. Confirm? [y/N] ", len(toDelete), len(entries))) {
		return
	}

	writeHistory(histFile, original, remaining)
	fmt.Printf("Deleted %d entries. %d entries remaining.\n", len(toDelete), len(remaining))
}
