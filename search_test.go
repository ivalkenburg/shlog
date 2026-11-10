package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestExtendedSearch(t *testing.T) {
	for _, tt := range []struct {
		query, text string
		want        bool
	}{
		{"", "anything", true}, {"gco", "git checkout", true}, {"gco", "git status", false},
		{"git main", "main; git status", true}, {"git main", "git status", false},
		{"git main | dev", "git switch dev", true}, {"git main | dev", "echo dev", false},
		{"git | hg status", "hg status", true}, {"git | hg status", "git log", false},
		{"'checkout", "git checkout", true}, {"'gco", "git checkout", false},
		{"'git'", "git-log", true}, {"'git'", "digital", false}, {"'git'", "git_git", false},
		{"^git", "git\necho done", true}, {"^git", "echo\ngit status", false},
		{"done$", "git\necho done", true}, {"done$", "done\ngit", false},
		{"^pwd$", "pwd", true}, {"^pwd$", "pwd\n", false}, {"^$", "", true},
		{"!sudo", "git status", true}, {"!sudo", "echo sudo", false},
		{"!^sudo", "echo sudo", true}, {"!log$", "git log", false},
		{"!'gco", "git checkout", false}, {"!'gco", "git status", true},
		{"git", "Git", true}, {"Git", "git", false}, {"Git main", "Git MAIN", true},
		{"cafe", "café", true}, {"café", "cafe", false}, {"café", "CAFÉ", true},
		{"Cafe", "Café", true}, {"Cafe", "café", false}, {"cafe", "cafe\u0301", true},
		{"'cafe", "cafe\u0301", true}, {"ΑΒ", "αβ", false}, {"αβ", "ΑΒ", true},
		{"你好", "echo 你好世界", true}, {"\U0001f600", "echo \U0001f600", true},
		{`\!sudo`, "echo !sudo", true}, {`\!sudo`, "sudo", false}, {`\|`, "a | b", true},
		{`\^git\$`, "echo ^git$", true}, {`'git\ checkout`, "git checkout", true},
		{`'git\ checkout`, "git clean checkout", false}, {`\\`, `echo \n`, true},
		{`\n`, `echo \n`, true}, {`\'quote`, "'quote", true},
		{"first second", "first\nsecond", true}, {`'first\ second`, "first\nsecond", false},
		{"\ufffdA\u0368", "\x8d", false}, {"\ufffdA\u0368", "\x8dA\u0368", true},
	} {
		t.Run(tt.query+"/"+tt.text, func(t *testing.T) {
			q, err := parseSearch(tt.query)
			if err != nil {
				t.Fatal(err)
			}
			text := newSearchText(tt.text)
			if got := q.matches(text); got != tt.want {
				t.Fatalf("matches(%q, %q) = %t; want %t", tt.query, tt.text, got, tt.want)
			}
			if tt.want {
				for _, p := range q.positions(text) {
					if p < 0 || p >= len([]rune(tt.text)) {
						t.Fatalf("invalid highlight position %d", p)
					}
				}
			}
		})
	}
}

func TestIncompleteSearch(t *testing.T) {
	for _, query := range []string{"|", "git |", "git | | main", "!", "'", "^", "$", "abc\\"} {
		if _, err := parseSearch(query); err == nil {
			t.Errorf("%q should require more input", query)
		}
	}
}

func TestSearchHighlightAlignment(t *testing.T) {
	for _, tt := range []struct {
		query, text string
		want        []int
	}{
		{"fb", "farawayBar fooBar", []int{11, 14}},
		{"gco", "git checkout", []int{0, 4, 9}},
		{"cafe", "cafe\u0301", []int{0, 1, 2, 3}},
		{"é", "café", []int{3}},
		{"'你好", "echo 你好", []int{5, 6}},
		{"main$", "main main", []int{5, 6, 7, 8}},
		{"!sudo", "git log", nil},
		{"git | hg", "hg status", []int{0, 1}},
	} {
		q, err := parseSearch(tt.query)
		if err != nil {
			t.Fatal(err)
		}
		if got := q.positions(newSearchText(tt.text)); !slices.Equal(got, tt.want) {
			t.Errorf("positions(%q, %q) = %v; want %v", tt.query, tt.text, got, tt.want)
		}
	}
}

func TestSearchLongCommand(t *testing.T) {
	text := newSearchText(strings.Repeat("x", 30_000) + "git checkout")
	q, _ := parseSearch(strings.Repeat("x", 100) + "gco")
	if !q.matches(text) || len(q.positions(text)) != 103 {
		t.Fatal("long-command fallback lost a matching term")
	}
}

func TestFilterCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, ok := filterPicker(ctx, []pickerItem{{text: newSearchText("echo")}}, searchQuery{}, pickerNewest); ok {
		t.Fatal("cancelled search completed")
	}
}

func TestSearchNormalizationViews(t *testing.T) {
	for _, tt := range []struct{ text, lower, plain, plainLower string }{
		{"GIT_STATUS=UPPER", "git_status=upper", "GIT_STATUS=UPPER", "git_status=upper"},
		{"Café RÉPERTOIRE 你好", "café répertoire 你好", "Cafe REPERTOIRE 你好", "cafe repertoire 你好"},
		{"e\u0301 Ж\u0301 ה\u05b0", "e\u0301 ж\u0301 ה\u05b0", "e Ж\u0301 ה\u05b0", "e ж\u0301 ה\u05b0"},
		{"1\u0301 a \u0301", "1\u0301 a \u0301", "1\u0301 a \u0301", "1\u0301 a \u0301"},
		{"Łódź Øresund İ", "łódź øresund i", "Lodz Oresund I", "lodz oresund i"},
	} {
		if got := newSearchText(tt.text); got != (searchText{tt.text, tt.lower, tt.plain, tt.plainLower}) {
			t.Errorf("normalization changed character/case semantics for %q: %+v", tt.text, got)
		}
	}
}

func BenchmarkPickerSearch100K(b *testing.B) {
	items := make([]pickerItem, 100_000)
	for i := range items {
		items[i].text = newSearchText(fmt.Sprintf("git checkout feature/%d && go test ./... # café", i))
	}
	for _, query := range []string{"gco", "git feature !sudo", "^git 'checkout 999 | 123", "cafe", "no-such-command"} {
		b.Run(query, func(b *testing.B) {
			q, _ := parseSearch(query)
			b.ReportAllocs()
			for b.Loop() {
				filterPicker(context.Background(), items, q, pickerNewest)
			}
		})
	}
}

func BenchmarkPickerUpdate100K(b *testing.B) {
	entries := make([]*Entry, 100_000)
	for i := range entries {
		entries[i] = &Entry{Raw: []string{fmt.Sprintf("git checkout feature/%d && go test ./...", i)}}
	}
	m := newPickerModel(entries, entries, pickerOptions{multi: true, path: "history"})
	m.width, m.height = 100, 32
	m.setQuery("gco")
	b.ReportAllocs()
	for b.Loop() {
		m.setQuery("gco")
		matches, _ := filterPicker(context.Background(), m.items, m.query, m.options.sort)
		m.applyMatches(matches, -1)
		m.render(true)
	}
}

var benchmarkPreparedText searchText

func BenchmarkSearchPreparation(b *testing.B) {
	for _, item := range []struct{ name, text string }{
		{"ASCII", "GIT_STATUS=UPPER git checkout feature/123 && go test ./..."},
		{"Unicode", "echo café répertoire 你好 עולם && git checkout main"},
	} {
		b.Run(item.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				benchmarkPreparedText = newSearchText(item.text)
			}
		})
	}
}

func BenchmarkPickerStartup100K(b *testing.B) {
	entries := make([]*Entry, 100_000)
	for i := range entries {
		entries[i] = &Entry{Raw: []string{fmt.Sprintf("GIT_STATUS=UPPER git checkout feature/%d && go test ./...", i)}}
	}
	b.ReportAllocs()
	for b.Loop() {
		newPickerModel(entries, entries, pickerOptions{})
	}
}

func BenchmarkPickerCancelledSearch100K(b *testing.B) {
	items := make([]pickerItem, 100_000)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b.ReportAllocs()
	for b.Loop() {
		filterPicker(ctx, items, searchQuery{}, pickerNewest)
	}
}

func FuzzExtendedSearch(f *testing.F) {
	for _, seed := range [][2]string{{"gco", "git checkout"}, {"^git !sudo", "git main"}, {"cafe", "cafe\u0301"}, {"αβ | 你好", "ΑΒ你好"}, {`\\ \!`, "\\!"}} {
		f.Add(seed[0], seed[1])
	}
	f.Fuzz(func(t *testing.T, query, text string) {
		if len(query) > 256 || len(text) > 512 {
			t.Skip()
		}
		q, err := parseSearch(query)
		if err != nil {
			return
		}
		s := newSearchText(text)
		if q.matches(s) {
			previous := -1
			for _, p := range q.positions(s) {
				if p <= previous || p < 0 || p >= len([]rune(text)) {
					t.Fatalf("invalid highlight indices for query %q, text %q", query, text)
				}
				previous = p
			}
		}
	})
}

func TestRelevanceScoreMatchesHighlightAlignment(t *testing.T) {
	var work searchWorkspace
	for _, query := range []string{"gco", "git | hg", "'git", "git$", "^git", "'git'", "^git$", "cafe", "é", "你好", "!sudo", "git main | dev", "^$", strings.Repeat("x", 100) + "gco"} {
		q, err := parseSearch(query)
		if err != nil {
			t.Fatal(err)
		}
		for _, text := range []string{"git checkout", "echo git", "git", "hg status", "main; git status", "git dev", "café", "cafe\u0301", "echo 你好", "", strings.Repeat("x", 30_000) + "git checkout"} {
			s := newSearchText(text)
			for _, group := range q.groups {
				for _, term := range group {
					if term.negative || !term.matches(s) {
						continue
					}
					_, want := term.positions(s)
					positions, got, ok := work.align(context.Background(), term, s, false)
					if !ok || positions != nil || got != want {
						t.Fatalf("query %q score on %q = %d, want %d", query, commandPreview(text[:min(len(text), 50)]), got, want)
					}
				}
			}
		}
	}
}

type cancelScoringContext struct {
	context.Context
	cancel context.CancelFunc
	checks int
}

func (c *cancelScoringContext) Err() error {
	c.checks++
	if c.checks == 20 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestRelevanceCancellationDuringAlignment(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &cancelScoringContext{Context: parent, cancel: cancel}
	text := strings.Repeat("x", 30_000) + "git checkout"
	q, _ := parseSearch(strings.Repeat("x", 55) + "gco")
	items := []pickerItem{{text: newSearchText(text)}}
	matches, ok := filterPicker(ctx, items, q, pickerRelevance)
	if ok || matches != nil || ctx.checks < 20 {
		t.Fatalf("in-progress scoring ignored cancellation: ok=%t checks=%d", ok, ctx.checks)
	}
}
