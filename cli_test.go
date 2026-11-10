package main

import (
	"slices"
	"testing"
)

func TestParseCLI(t *testing.T) {
	t.Setenv("HISTFILE", "/default/history")
	for _, tt := range []struct {
		name                     string
		args, want               []string
		file                     string
		force, dry, output, help bool
	}{
		{name: "globals before", args: []string{"-f", "--histfile", "/other", "-s", "-o", "del", "-20"}, want: []string{"del", "-20"}, file: "/other", force: true, dry: true, output: true},
		{name: "globals after", args: []string{"del", "-20", "-f", "--histfile", "/other", "--dry-run", "-o"}, want: []string{"del", "-20"}, file: "/other", force: true, dry: true, output: true},
		{name: "interleaved", args: []string{"grep", "git", "--histfile=/other", "-1h"}, want: []string{"grep", "git", "-1h"}, file: "/other"},
		{name: "literal regex", args: []string{"del", "--match", "-o", "--dry-run"}, want: []string{"del", "--match", "-o"}, dry: true},
		{name: "literal path", args: []string{"list", "--histfile", "--help"}, want: []string{"list"}, file: "--help"},
		{name: "sort value", args: []string{"pick", "--sort", "frequency", "-s"}, want: []string{"pick", "--sort", "frequency"}, dry: true},
		{name: "grep terminator", args: []string{"grep", "--", "-f"}, want: []string{"grep", "-f"}},
		{name: "del terminator", args: []string{"del", "--", "-1"}, want: []string{"del", "--", "-1"}},
		{name: "leading terminator", args: []string{"--", "pick", "--unique"}, want: []string{"pick", "--", "--unique"}},
		{name: "empty terminator", args: []string{"--"}},
		{name: "help after command", args: []string{"pick", "--help"}, want: []string{"pick"}, help: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCLI(tt.args)
			if err != nil {
				t.Fatal(err)
			}
			file := tt.file
			if file == "" {
				file = "/default/history"
			}
			if !slices.Equal(got.args, tt.want) || got.histFile != file || got.force != tt.force || got.simulate != tt.dry || got.output != tt.output || got.help != tt.help {
				t.Fatalf("parseCLI(%q) = %+v", tt.args, got)
			}
		})
	}
	for _, args := range [][]string{{"--histfile"}, {"list", "--histfile"}, {"del", "--match"}, {"pick", "--sort"}, {"del", "--pick", "--sort"}} {
		if _, err := parseCLI(args); err == nil {
			t.Errorf("parseCLI(%q) accepted missing value", args)
		}
	}
}
