package main

import (
	"fmt"
	"os"
	"time"
)

func runStats(args []string, histFile string) {
	entries := loadHistory(histFile)

	scope, err := selectOptionalEntries(entries, args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(exitError)
	}

	if len(scope) == 0 {
		fmt.Fprintf(os.Stderr, "No entries matched. Total entries: %d.\n", len(entries))
		os.Exit(exitNoMatch)
	}

	stats := ComputeStats(scope, 10)

	var firstTS, lastTS int64
	for _, e := range scope {
		if e.Timestamp == 0 {
			continue
		}
		if firstTS == 0 || e.Timestamp < firstTS {
			firstTS = e.Timestamp
		}
		if lastTS == 0 || e.Timestamp > lastTS {
			lastTS = e.Timestamp
		}
	}
	if firstTS != 0 {
		first := time.Unix(firstTS, 0).Format("2006-01-02")
		last := time.Unix(lastTS, 0).Format("2006-01-02")
		printOutput("Entries: %d  Unique: %d  Date range: %s to %s\n\n", stats.Total, stats.Unique, first, last)
	} else {
		printOutput("Entries: %d  Unique: %d\n\n", stats.Total, stats.Unique)
	}

	countWidth := len(fmt.Sprintf("%d", stats.TopN[0].Count))
	rankWidth := len(fmt.Sprintf("%d", len(stats.TopN)))
	printOutput("Top %d commands:\n", len(stats.TopN))
	for i, cc := range stats.TopN {
		printOutput("  %*d.  %*d  %s\n", rankWidth, i+1, countWidth, cc.Count, commandPreview(cc.Command))
	}
}
