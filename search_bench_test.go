package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func BenchmarkPickerRepeatedRelevance10K(b *testing.B) {
	for _, distinct := range []int{1, 100, 1000} {
		b.Run(fmt.Sprint(distinct), func(b *testing.B) {
			entries := make([]*Entry, 10_000)
			for i := range entries {
				entries[i] = &Entry{Raw: []string{fmt.Sprintf("git checkout feature/%d && go test ./... # café", i%distinct)}}
			}
			model := newPickerModel(entries, entries, pickerOptions{})
			query, _ := parseSearch("gco cafe")
			b.ReportAllocs()
			for b.Loop() {
				filterPicker(context.Background(), model.items, query, pickerRelevance)
			}
		})
	}
}

func BenchmarkSearchPathologicalAlignment(b *testing.B) {
	for _, tt := range []struct{ name, query, text string }{
		{"fuzzy suffix", "gco", "git checkout" + strings.Repeat("x", 700_000)},
		{"fuzzy fallback", strings.Repeat("x", 100) + "gco", strings.Repeat("x", 30_000) + "git checkout"},
		{"exact overlap", "'" + strings.Repeat("a", 1000), strings.Repeat("a", 100_000)},
	} {
		b.Run(tt.name, func(b *testing.B) {
			query, _ := parseSearch(tt.query)
			text := newSearchText(tt.text)
			var work searchWorkspace
			b.ReportAllocs()
			for b.Loop() {
				query.score(context.Background(), text, &work)
			}
		})
	}
}

func BenchmarkPickerSparseRelevance100K(b *testing.B) {
	items := make([]pickerItem, 100_000)
	text := newSearchText("echo nope")
	for i := range items {
		items[i] = pickerItem{text: text, index: i + 1}
	}
	items[len(items)-1].text = newSearchText("git checkout")
	query, _ := parseSearch("gco")
	b.ReportAllocs()
	for b.Loop() {
		filterPicker(context.Background(), items, query, pickerRelevance)
	}
}

// BenchmarkFuzzySearch measures a complete query over an already loaded history.
// Corpus preparation and query parsing are outside the timed loop. Commands are
// distinct so the duplicate-score cache cannot hide the cost of relevance work.
func BenchmarkFuzzySearch(b *testing.B) {
	commands := []string{
		"git checkout feature/%06d && go test ./...",
		"git status --short # worktree-%06d",
		"docker compose -p app-%06d up -d",
		"kubectl logs deployment/api-%06d --tail=100",
		"go test ./... -run TestCase%06d -count=1",
		"npm run build -- --output-path dist/%06d",
		"rg --line-number TODO src/module-%06d",
		"cd /tmp/café/project-%06d && pwd",
		"ssh deploy@host-%06d.example.com uptime",
		"find ./archive-%06d -type f -name '*.log'",
		"printf 'first line\\n' # %06d\necho second line",
		"sudo systemctl restart service-%06d",
	}
	queries := []struct{ name, text string }{
		{"gco", "gco"},
		{"multi_term", "git feature !sudo"},
		{"accent_fold", "cafe"},
		{"no_match", "zzzz-no-such-command"},
	}
	ctx := context.Background()
	for _, size := range []int{1_000, 10_000, 100_000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			entries := make([]*Entry, size)
			for i := range entries {
				entries[i] = &Entry{Raw: []string{fmt.Sprintf(commands[i%len(commands)], i)}}
			}
			model := newPickerModel(entries, entries, pickerOptions{})
			for _, query := range queries {
				b.Run(query.name, func(b *testing.B) {
					parsed, err := parseSearch(query.text)
					if err != nil {
						b.Fatal(err)
					}
					for _, order := range []pickerSort{pickerNewest, pickerRelevance} {
						b.Run(string(order), func(b *testing.B) {
							initial, ok := filterPicker(ctx, model.items, parsed, order)
							if !ok {
								b.Fatal("initial search cancelled")
							}
							wantMatches := len(initial)
							b.ReportAllocs()
							for b.Loop() {
								matches, ok := filterPicker(ctx, model.items, parsed, order)
								if !ok || len(matches) != wantMatches {
									b.Fatal("search cancelled or changed its result count")
								}
							}
							b.ReportMetric(float64(wantMatches), "matches/op")
							b.ReportMetric(float64(size)*float64(b.N)/b.Elapsed().Seconds()/1e6, "Mentries/s")
						})
					}
				})
			}
		})
	}
}
