package main

import (
	"fmt"
	"os"
	"strings"
)

func runStats(args []string, histFile string) {
	entries := loadHistory(histFile)

	scope := entries
	if len(args) > 0 {
		var err error
		scope, err = SelectEntries(entries, args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}

	if len(scope) == 0 {
		fmt.Fprintf(os.Stderr, "No entries matched. Total entries: %d.\n", len(entries))
		return
	}

	stats := ComputeStats(scope, 10)

	if scope[0].Timestamp != 0 {
		first := scope[0].Time().Format("2006-01-02")
		last := scope[len(scope)-1].Time().Format("2006-01-02")
		fmt.Printf("Entries: %d  Unique: %d  Date range: %s to %s\n\n", stats.Total, stats.Unique, first, last)
	} else {
		fmt.Printf("Entries: %d  Unique: %d\n\n", stats.Total, stats.Unique)
	}

	if len(stats.TopN) == 0 {
		return
	}
	countWidth := len(fmt.Sprintf("%d", stats.TopN[0].Count))
	rankWidth := len(fmt.Sprintf("%d", len(stats.TopN)))
	fmt.Printf("Top %d commands:\n", len(stats.TopN))
	for i, cc := range stats.TopN {
		cmd := cc.Command
		if idx := strings.Index(cmd, "\n"); idx != -1 {
			cmd = cmd[:idx] + " ↵"
		}
		fmt.Printf("  %*d.  %*d  %s\n", rankWidth, i+1, countWidth, cc.Count, cmd)
	}
}
