package main

import (
	"bufio"
	"fmt"
	"io"
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

	pool, err := selectOptionalEntries(entries, selArgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	selected, err := pickEntries(entries, pool, pickerOptions{multi: multi, path: histFile})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	w := bufio.NewWriter(os.Stdout)
	err = writeCommands(w, selected)
	if err == nil {
		err = w.Flush()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error writing output: %v\n", err)
		os.Exit(1)
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
