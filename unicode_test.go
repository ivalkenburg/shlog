package main

import (
	"strings"
	"testing"
)

func TestPickerKnownRuneWidths(t *testing.T) {
	for _, tt := range []struct {
		text string
		want int
	}{
		{"⌚", 2}, {"⏰", 2}, {"✅", 2}, {"☕", 2}, {"🚀", 2}, {"你", 2},
		{"a", 1}, {"é", 1}, {"\u0301", 0}, {"\u200d", 0}, {"🏻", 2},
	} {
		for _, r := range tt.text {
			if got := pickerRuneWidth(r); got != tt.want {
				t.Errorf("%U has %d cells; want %d", r, got, tt.want)
			}
		}
	}
}

func TestPickerEmojiSequenceWidths(t *testing.T) {
	for _, tt := range []struct {
		text string
		want int
	}{
		{"a\u200db\u200dc", 3}, {"a\u200d\x00", 3}, {"👩\u200d\u202e", 8},
		{"a🏻", 3}, {"🏻", 2}, {"👩🏻", 2}, {"☝🏻", 2}, {"🏋🏻", 2}, {"👩🏻‍💻", 2}, {"👩‍👩‍👧‍👦", 2},
		{"a🇳🇱", 3}, {"🇳🇱", 2}, {"1️⃣", 2}, {"1\u20e3", 2},
		{"a\ufe0f", 1}, {"a\u20e3", 1}, {"©️", 2}, {"♥️", 2}, {"你\u200d好", 4},
	} {
		if got := glyphWidth(pickerGlyphs(tt.text, nil)); got != tt.want {
			t.Errorf("%q has %d cells; want %d", tt.text, got, tt.want)
		}
	}
}

func TestPickerWideEmojiRowsDoNotWrap(t *testing.T) {
	for _, symbol := range []rune{'⌚', '⏰', '✅'} {
		all := []*Entry{{Raw: []string{strings.Repeat(string(symbol), 40)}}}
		m := newPickerModel(all, all, pickerOptions{multi: true})
		m.width, m.height = 30, 10
		row := pickerANSI.ReplaceAllString(pickerFrameRows(m.render(false))[3], "")
		// This fixture has only ASCII and the known two-cell symbol. Count
		// independently of the renderer's width lookup to catch table gaps.
		width := 0
		for _, r := range row {
			if r == symbol {
				width += 2
			} else {
				width++
			}
		}
		if width >= m.width {
			t.Errorf("%q row wraps at %d columns: %q (%d cells)", string(symbol), m.width, row, width)
		}
	}
}

func TestUnicodeRangesAreOrdered(t *testing.T) {
	for _, table := range [][][2]rune{wideRunes, pictographicRunes, modifierBaseRunes} {
		for i, interval := range table {
			if interval[0] > interval[1] || i > 0 && table[i-1][1] >= interval[0] {
				t.Fatalf("Unicode table has overlapping or unordered ranges at %U", interval[0])
			}
		}
	}
}
