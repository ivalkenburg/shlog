package main

import (
	"context"
	"slices"
	"strings"
	"testing"
)

func TestExactSearchBestOccurrence(t *testing.T) {
	for _, tt := range []struct {
		query, text string
		want        []int
	}{
		{"'git", "xxgit git", []int{6, 7, 8}},
		{"'git'", "(git /git", []int{6, 7, 8}},
		{"'git | ^xx", "xxgit git", []int{6, 7, 8}},
		// Invalid bytes and literal replacement characters are distinct in
		// exact matching; rebuilding the string from runes loses that fact.
		{"'�É\u0301", "\xffÉ\u0301 �É\u0301", []int{4, 5, 6}},
	} {
		q, err := parseSearch(tt.query)
		if err != nil {
			t.Fatal(err)
		}
		if got := q.positions(newSearchText(tt.text)); !slices.Equal(got, tt.want) {
			t.Errorf("positions(%q, %q) = %v, want %v", tt.query, tt.text, got, tt.want)
		}
	}
}

func TestFuzzyScoreIgnoresIrrelevantSuffix(t *testing.T) {
	q, _ := parseSearch("gco")
	s := newSearchText("git checkout" + strings.Repeat("x", 700_000))
	var w searchWorkspace
	score, ok := q.score(context.Background(), s, &w)
	shortScore, _ := q.score(context.Background(), newSearchText("git checkout"), &w)
	if !ok || score != shortScore {
		t.Fatalf("suffix changed alignment score: got %d want %d", score, shortScore)
	}
}

func TestSearchInvalidUTF8Alignment(t *testing.T) {
	for _, query := range []string{"'é", "'\ufffdé", "'é$", "^é", "'é'", "é", "'a", "'\ufffd", "'\ufffd\u0301", "'\u0301", "^$"} {
		q, err := parseSearch(query)
		if err != nil {
			t.Fatal(err)
		}
		for _, text := range []string{"\xffé", "é\xffé", "\xff\u0301", "\xff", "\ufffdé", "\xffa", "\xff\ufffd\u0301", "a\xffé", "", "\xff\u0301é"} {
			s := newSearchText(text)
			if !q.matches(s) {
				continue
			}
			p := q.positions(s)
			for _, at := range p {
				if at < 0 || at >= len([]rune(text)) {
					t.Fatalf("query %q text %q positions %v", query, text, p)
				}
			}
		}
	}
}

// Enumerate every subsequence independently of the dynamic-programming code.
func referenceFuzzyScore(text, query string) (int, bool) {
	raw, needle := []rune(text), []rune(query)
	best, found := 0, false
	var visit func(int, int, int, int)
	visit = func(j, start, previous, score int) {
		if j == len(needle) {
			if !found || score > best {
				best, found = score, true
			}
			return
		}
		for i := start; i < len(raw); i++ {
			if raw[i] != needle[j] {
				continue
			}
			next := 16 + 2*alignmentBonus(raw, i) - i
			if j > 0 {
				next = score + 16 + alignmentBonus(raw, i) - 3*(i-previous)
				if i == previous+1 {
					next = score + 24 + alignmentBonus(raw, i)
				}
			}
			visit(j+1, i+1, i, next)
		}
	}
	visit(0, 0, -1, 0)
	return best, found
}

func TestFuzzyAlignmentAgainstReference(t *testing.T) {
	const alphabet = "ab -"
	var texts []string
	var generate func(string, int)
	generate = func(s string, n int) {
		texts = append(texts, s)
		if n > 0 {
			for _, r := range alphabet {
				generate(s+string(r), n-1)
			}
		}
	}
	generate("", 6)
	for _, query := range []string{"a", "b", "aa", "ab", "ba", "aaa", "aba", "aab", "abb"} {
		q, _ := parseSearch(query)
		var work searchWorkspace
		for _, text := range texts {
			want, found := referenceFuzzyScore(text, query)
			s := newSearchText(text)
			if got := q.matches(s); got != found {
				t.Fatalf("membership %q %q got %t want %t", query, text, got, found)
			}
			if !found {
				continue
			}
			p, got, ok := work.align(context.Background(), q.groups[0][0], s, true)
			if !ok || got != want || len(p) != len(query) {
				t.Fatalf("alignment %q %q = %v %d; want %d", query, text, p, got, want)
			}
		}
	}
}

func scoreSearchPositions(raw []rune, p []int) int {
	score := 0
	for j, at := range p {
		if j == 0 {
			score = 16 + 2*alignmentBonus(raw, at) - at
		} else if at == p[j-1]+1 {
			score += 24 + alignmentBonus(raw, at)
		} else {
			score += 16 + alignmentBonus(raw, at) - 3*(at-p[j-1])
		}
	}
	return score
}

func TestFuzzyFallbackScoresItsAlignment(t *testing.T) {
	text := strings.Repeat("x", 30_000) + "git checkout"
	query := strings.Repeat("x", 100) + "gco"
	q, err := parseSearch(query)
	if err != nil {
		t.Fatal(err)
	}
	s := newSearchText(text)
	p := q.positions(s)
	if len(p) != len(query) {
		t.Fatalf("fallback lost positions: %v", p)
	}
	want := scoreSearchPositions([]rune(text), p)
	var work searchWorkspace
	got, ok := q.score(context.Background(), s, &work)
	if !ok || got != want || got == 0 {
		t.Fatalf("fallback score = %d, want %d", got, want)
	}
}

func TestExactAlignmentAgainstReference(t *testing.T) {
	const alphabet = "ab (-/"
	var work searchWorkspace
	var visit func(string, int)
	visit = func(text string, depth int) {
		s := newSearchText(text)
		raw := []rune(text)
		for _, query := range []string{"a", "aa", "ab", "ba"} {
			for _, mode := range []string{"substring", "word", "prefix", "suffix", "whole"} {
				term := searchTerm{text: query, sensitive: true, word: mode == "word", prefix: mode == "prefix" || mode == "whole", end: mode == "suffix" || mode == "whole"}
				needle := []rune(query)
				var want []int
				best, found := 0, false
				for i := 0; i+len(needle) <= len(raw); i++ {
					end := i + len(needle)
					if !slices.Equal(raw[i:end], needle) || term.prefix && i != 0 || term.end && end != len(raw) {
						continue
					}
					if term.word && (i > 0 && wordRune(raw[i-1]) || end < len(raw) && wordRune(raw[end])) {
						continue
					}
					p := make([]int, len(needle))
					for j := range p {
						p[j] = i + j
					}
					score := scoreSearchPositions(raw, p)
					if !found || score > best {
						want, best, found = p, score, true
					}
				}
				if got := term.matches(s); got != found {
					t.Fatalf("%s membership %q %q = %t want %t", mode, query, text, got, found)
				}
				if !found {
					continue
				}
				got, score, ok := work.align(context.Background(), term, s, true)
				if !ok || score != best || !slices.Equal(got, want) {
					t.Fatalf("%s alignment %q %q = %v/%d want %v/%d", mode, query, text, got, score, want, best)
				}
			}
		}
		if depth > 0 {
			for _, r := range alphabet {
				visit(text+string(r), depth-1)
			}
		}
	}
	visit("", 5)
}

func FuzzFuzzyAlignment(f *testing.F) {
	for _, s := range [][2]string{{"ab", "a -ab"}, {"aa", "aaa"}, {"gco", "git checkout"}, {"é", "é é"}, {"\ufffd", "\xff"}, {"你好", "你好世界"}} {
		f.Add(s[0], s[1])
	}
	f.Fuzz(func(t *testing.T, query, text string) {
		// Exhaustive enumeration remains small enough to be an independent oracle.
		needle, raw := []rune(query), []rune(text)
		if len(needle) == 0 || len(needle) > 4 || len(raw) > 10 {
			t.Skip()
		}
		query = string(needle)
		term := searchTerm{text: query, fuzzy: true, sensitive: true}
		s := newSearchText(text)
		want, found := referenceFuzzyScore(text, query)
		if got := term.matches(s); got != found {
			t.Fatalf("membership %q %q = %t want %t", query, text, got, found)
		}
		if !found {
			return
		}
		var work searchWorkspace
		p, got, ok := work.align(context.Background(), term, s, true)
		if !ok || got != want || len(p) != len(needle) {
			t.Fatalf("alignment %q %q = %v/%d want %d", query, text, p, got, want)
		}
		previous := -1
		for i, at := range p {
			if at <= previous || at >= len(raw) || raw[at] != needle[i] {
				t.Fatalf("invalid alignment %q %q = %v", query, text, p)
			}
			previous = at
		}
		if actual := scoreSearchPositions(raw, p); actual != got {
			t.Fatalf("traceback score %d differs from DP score %d", actual, got)
		}
		p, score, ok := work.align(context.Background(), term, s, false)
		if !ok || p != nil || score != got {
			t.Fatalf("score-only alignment = %v/%d want nil/%d", p, score, got)
		}
	})
}

func TestExactAlignmentCancellationAfterPreparation(t *testing.T) {
	q, err := parseSearch("'" + strings.Repeat("a", 16))
	if err != nil {
		t.Fatal(err)
	}
	term := q.groups[0][0]
	text := newSearchText(strings.Repeat("a", 500))
	var work searchWorkspace
	if _, _, ok := work.align(context.Background(), term, text, false); !ok {
		t.Fatal("initial alignment cancelled")
	}
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &cancelScoringContext{Context: parent, cancel: cancel}
	if _, _, ok := work.align(ctx, term, text, false); ok || ctx.checks < 20 {
		t.Fatalf("exact alignment ignored cancellation: ok=%t checks=%d", ok, ctx.checks)
	}
}

func TestLongLiteralOverlappingMatches(t *testing.T) {
	for _, needle := range []string{strings.Repeat("a", 80), strings.Repeat("ab", 40), strings.Repeat("ab", 40) + "ac", strings.Repeat("é", 40)} {
		q, err := parseSearch("'" + needle)
		if err != nil {
			t.Fatal(err)
		}
		term := q.groups[0][0]
		text := "\xff" + strings.Repeat(needle, 3) + " " + needle
		var got, want []int
		ok := term.visitLiteral(context.Background(), text, func(at int) bool { got = append(got, at); return true })
		for offset := 0; offset < len(text); {
			at := strings.Index(text[offset:], needle)
			if at < 0 {
				break
			}
			at += offset
			want = append(want, at)
			offset = at + 1
		}
		if !ok || !slices.Equal(got, want) {
			t.Fatalf("long literal %q occurrences = %v want %v", needle, got, want)
		}
		word, err := parseSearch("'" + needle + "'")
		if err != nil {
			t.Fatal(err)
		}
		s := newSearchText(strings.Repeat(needle, 3) + " " + needle)
		if !word.matches(s) {
			t.Fatalf("long word %q lost its final occurrence", needle)
		}
		p := word.positions(s)
		if len(p) != len([]rune(needle)) || p[0] != 3*len([]rune(needle))+1 {
			t.Fatalf("long word highlight = %v", p)
		}
		if word.matches(newSearchText(strings.Repeat(needle, 3))) {
			t.Fatalf("long word %q accepted embedded occurrences", needle)
		}
	}
}

func TestLongLiteralCancellation(t *testing.T) {
	q, err := parseSearch("'" + strings.Repeat("a", 80) + "b")
	if err != nil {
		t.Fatal(err)
	}
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &cancelScoringContext{Context: parent, cancel: cancel}
	if ok := q.groups[0][0].visitLiteral(ctx, strings.Repeat("a", 100_000), func(int) bool { return true }); ok || ctx.checks < 20 {
		t.Fatalf("literal scan ignored cancellation: ok=%t checks=%d", ok, ctx.checks)
	}
}
