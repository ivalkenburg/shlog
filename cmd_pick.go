package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

func runPick(args []string, histFile string) {
	options := pickerOptions{path: histFile}
	var selArgs []string
	literal := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if literal {
			selArgs = append(selArgs, a)
			continue
		}
		switch {
		case a == "--":
			literal = true
		case a == "--multi":
			options.multi = true
		case a == "--unique":
			options.unique = true
		case a == "--sort" || strings.HasPrefix(a, "--sort="):
			value, err := pickerSortArgument(args, &i)
			if err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				os.Exit(exitError)
			}
			options.sort = value
		default:
			selArgs = append(selArgs, a)
		}
	}

	entries := loadHistory(histFile)

	pool, err := selectOptionalEntries(entries, selArgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(exitError)
	}

	selected, err := pickEntries(entries, pool, options)
	if errors.Is(err, errPickerCanceled) {
		os.Exit(exitCanceled)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(exitError)
	}
	if len(selected) == 0 {
		fmt.Fprintln(os.Stderr, "No entries matched.")
		os.Exit(exitNoMatch)
	}

	w := bufio.NewWriter(os.Stdout)
	err = writeCommands(w, selected)
	if err == nil {
		err = w.Flush()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error writing output: %v\n", err)
		os.Exit(exitError)
	}
}

func writeCommands(w io.Writer, entries []*Entry) error {
	for _, e := range entries {
		if _, err := fmt.Fprintln(w, CommandText(e)); err != nil {
			return err
		}
	}
	return nil
}
