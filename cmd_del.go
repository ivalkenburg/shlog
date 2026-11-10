package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

func runDel(args []string, force, simulate, output bool, histFile string) {
	// Parse command options without interpreting negative selections as flags.
	var pattern string
	var selections []string
	patternSet := false
	invert := false
	pick := false
	options := pickerOptions{multi: true, deleting: true, path: histFile}
	literal := false
	for i := 0; i < len(args); i++ {
		if literal {
			selections = append(selections, args[i])
			continue
		}
		switch args[i] {
		case "--":
			literal = true
		case "--sort":
			value, err := pickerSortArgument(args, &i)
			if err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				os.Exit(exitError)
			}
			options.sort = value
		case "--match":
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "error: --match requires a pattern argument")
				os.Exit(exitError)
			}
			pattern = args[i]
			patternSet = true
		case "--invert":
			invert = true
		case "--pick":
			pick = true
		default:
			if strings.HasPrefix(args[i], "--sort=") {
				value, err := pickerSortArgument(args, &i)
				if err != nil {
					fmt.Fprintln(os.Stderr, "error:", err)
					os.Exit(exitError)
				}
				options.sort = value
				continue
			}
			selections = append(selections, args[i])
		}
	}
	if options.sort != "" && !pick {
		fmt.Fprintln(os.Stderr, "error: --sort requires --pick")
		os.Exit(exitError)
	}
	if len(selections) > 1 {
		fmt.Fprintln(os.Stderr, "error: del accepts at most one selection")
		os.Exit(exitError)
	}
	if !pick && !patternSet && len(selections) == 0 {
		fmt.Fprintln(os.Stderr, "error: missing selection, --match, or --pick argument")
		printUsage()
		os.Exit(exitError)
	}
	if invert && !patternSet {
		fmt.Fprintln(os.Stderr, "error: --invert requires --match")
		os.Exit(exitError)
	}
	if pick && patternSet {
		fmt.Fprintln(os.Stderr, "error: --pick and --match cannot be used together")
		os.Exit(exitError)
	}
	if patternSet && len(selections) != 0 {
		fmt.Fprintln(os.Stderr, "error: --match cannot be used with a selection")
		os.Exit(exitError)
	}

	entries, original := loadHistorySnapshot(histFile)

	var toDelete []*Entry
	if pick {
		pool, err := selectOptionalEntries(entries, selections)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(exitError)
		}
		toDelete, err = pickEntries(entries, pool, options)
		if errors.Is(err, errPickerCanceled) {
			os.Exit(exitCanceled)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(exitError)
		}
	} else if patternSet {
		matched, err := MatchEntries(entries, pattern)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(exitError)
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
			os.Exit(exitError)
		}
	}

	remaining := InvertEntries(entries, toDelete)

	if output {
		printEntries(remaining)
		return
	}

	if len(toDelete) == 0 {
		fmt.Fprintf(os.Stderr, "No entries matched. Total entries: %d.\n", len(entries))
		os.Exit(exitNoMatch)
	}

	if simulate {
		printEntries(toDelete)
		printOutput("\n(simulate) Would delete %d of %d entries.\n", len(toDelete), len(entries))
		return
	}

	if !confirm(force, fmt.Sprintf("About to delete %d of %d entries. Confirm? [y/N] ", len(toDelete), len(entries))) {
		os.Exit(exitCanceled)
	}

	writeHistory(histFile, original, remaining)
	printOutput("Deleted %d entries. %d entries remaining.\n", len(toDelete), len(remaining))
}
