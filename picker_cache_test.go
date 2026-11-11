package main

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestPickerBoundedWrappingPreservesVisibleRows(t *testing.T) {
	for _, text := range []string{
		strings.Repeat("line\n", 1000), strings.Repeat("你好 é 👩‍💻 ", 100),
		"\tfirst\n\n\tsecond\tline\n", strings.Repeat("payload ", 1000),
		strings.Repeat("\x1b\t\u202e😀", 1000),
	} {
		for _, width := range []int{1, 4, 8, 28, 78} {
			full := wrapPickerText(text, []int{0, 5, 10}, width)
			for _, limit := range []int{1, 2, 6, len(full) + 1} {
				got := wrapPickerTextRows(text, []int{0, 5, 10}, width, limit)
				if !reflect.DeepEqual(got, full[:min(limit, len(full))]) {
					t.Fatalf("bounded wrapping changed visible rows at width %d, limit %d", width, limit)
				}
			}
		}
	}
}

func TestPickerWrappedTabsUseNewRowTabStop(t *testing.T) {
	lines := wrapPickerText("abcde\tx", nil, 6)
	if len(lines) != 2 || renderPickerGlyphs(lines[0].glyphs, 6, false, "") != "abcde" || renderPickerGlyphs(lines[1].glyphs, 6, false, "") != "    x" {
		t.Fatal("a tab crossing a wrap boundary used the preceding row's tab stop")
	}
}

func assertPickerCacheFrame(t *testing.T, m *pickerModel) {
	t.Helper()
	cold := *m
	cold.renderCache = pickerRenderCache{}
	for _, color := range []bool{false, true} {
		if got, want := m.render(color), cold.render(color); got != want {
			t.Fatal("cached rendering differs from a fresh layout")
		}
	}
}

func TestPickerCacheInvalidationAndInteractions(t *testing.T) {
	all := []*Entry{
		{Raw: []string{"git status", "echo cafe", strings.Repeat("wide 你好 ", 25)}},
		{Raw: []string{"git checkout main", "echo cafe", "done"}},
	}
	m := newPickerModel(all, all, pickerOptions{multi: true, path: "history"})
	searchPicker(t, m, "git")
	assertPickerCacheFrame(t, m)
	m.move(1)
	assertPickerCacheFrame(t, m)
	m.toggle()
	assertPickerCacheFrame(t, m)
	m.scrollPreview(2)
	assertPickerCacheFrame(t, m)
	m.width, m.height = 30, 10
	m.ensureVisible()
	assertPickerCacheFrame(t, m)
	m.scrollPreview(1000)
	assertPickerCacheFrame(t, m)
	m.width, m.height = 100, 32
	m.ensureVisible()
	assertPickerCacheFrame(t, m)
	m.key(pickerEvent{key: "help"})
	assertPickerCacheFrame(t, m)
	m.width = 40
	assertPickerCacheFrame(t, m)
	m.key(pickerEvent{key: "escape"})
	assertPickerCacheFrame(t, m)

	// A query is replaced before its asynchronous results arrive. Stale
	// alignments must be gone even if the new query has incomplete syntax.
	for _, query := range []string{"cafe", "git |", "!sudo", "gco"} {
		m.input, m.cursor = []rune(query), len([]rune(query))
		m.setQuery(query)
		if len(m.renderCache.positions) != 0 || m.renderCache.preview != nil {
			t.Fatal("query replacement kept stale rendering data")
		}
		assertPickerCacheFrame(t, m)
		searchPicker(t, m, query)
		assertPickerCacheFrame(t, m)
	}
}

func TestPickerAlignmentCacheIsBounded(t *testing.T) {
	all := make([]*Entry, 80)
	for i := range all {
		all[i] = &Entry{Raw: []string{fmt.Sprintf("git status %d", i)}}
	}
	m := newPickerModel(all, all, pickerOptions{})
	m.height = 14
	searchPicker(t, m, "git")
	for range all {
		assertPickerCacheFrame(t, m)
		if len(m.renderCache.positions) > 2*m.listRows() {
			t.Fatal("alignment cache grew beyond two result pages")
		}
		m.move(1)
	}
}

func TestPickerGlyphClippingMatchesFullText(t *testing.T) {
	texts := []string{
		"x e\u0301z", "a♥️b", "👩‍💻x", "🇳🇱x", "\u0301x", "你好 café", "\xffx",
		"☝🏻", "a\u200db\u200dc", "a\u200d\x00", "👩\u200d\u202e", "a🏻", "⌚⏰✅",
		"\x1b[31m", "\u202ex", "\tline", strings.Repeat("payload ", 1000),
	}
	for _, text := range texts {
		positions := []int{0, 2, 3, 4}
		full := pickerGlyphs(text, positions)
		for width := 0; width <= 16; width++ {
			glyphs, complete := pickerGlyphsWithin(text, positions, width)
			if complete != (glyphWidth(full) <= width) {
				t.Fatalf("incorrect clipping boundary for %q at %d", text, width)
			}
			for _, base := range []string{"", "\x1b[7m"} {
				if got, want := renderPickerGlyphs(glyphs, width, true, base), renderPickerGlyphs(full, width, true, base); got != want {
					t.Fatalf("clipping changed visible clusters for %q at %d: %q != %q", text, width, got, want)
				}
			}
		}
	}
	if got := renderPickerGlyphs(pickerGlyphs("\xff", nil), 10, false, ""); got != "�" {
		t.Fatalf("invalid UTF-8 was forwarded to the terminal: %q", got)
	}
}

func TestPickerLongGraphemeClusters(t *testing.T) {
	marks := strings.Repeat("\u0301", 10_000)
	for _, tt := range []struct {
		text, want string
		width      int
	}{
		{"e" + marks, "e" + marks, 1},
		{marks, "◌" + marks, 1},
		{"\xff" + marks, "�" + marks, 1},
		{"\x01" + marks, "^A" + marks, 2},
		{"👩" + strings.Repeat("\u200d👩", 10_000), "👩" + strings.Repeat("\u200d👩", 10_000), 2},
	} {
		glyphs, complete := pickerGlyphsWithin(tt.text, []int{5_000}, 80)
		if !complete || len(glyphs) != 1 || glyphs[0].text != tt.want || glyphs[0].width != tt.width || !glyphs[0].matched {
			t.Fatalf("long cluster was corrupted: %d glyphs, complete=%t", len(glyphs), complete)
		}
		if clipped, complete := pickerGlyphsWithin(tt.text, nil, tt.width-1); complete || len(clipped) != 0 {
			t.Fatal("oversized cluster was split instead of clipped")
		}
	}
}

func FuzzPickerGlyphClipping(f *testing.F) {
	for _, text := range []string{"plain text", "a♥️b", "👩‍💻x", "🇳🇱x", "e\u0301", "\xffx", "\x1b[31m", "☝🏻", "a\u200db\u200dc", "a\u200d\x00", "👩\u200d\u202e", "a🏻"} {
		f.Add(text, uint8(3))
	}
	f.Fuzz(func(t *testing.T, text string, width uint8) {
		if len(text) > 2048 {
			t.Skip()
		}
		limit := int(width % 80)
		full := pickerGlyphs(text, nil)
		clipped, complete := pickerGlyphsWithin(text, nil, limit)
		if complete != (glyphWidth(full) <= limit) || renderPickerGlyphs(clipped, limit, false, "") != renderPickerGlyphs(full, limit, false, "") {
			t.Fatalf("width-limited preparation changed glyphs for %q at %d", text, limit)
		}
	})
}

func FuzzPickerBoundedWrapping(f *testing.F) {
	// A tab can shrink from two raw glyph cells to a one-cell tab stop.
	f.Add("000\t\x06000000000000000\x1400000\x1c000000\x01000000000000", uint8(129), uint8(0))
	for _, text := range []string{"abcde\tx", "你好 é 👩‍💻\n\tlast", "\x1b\t\u202e😀", "\xffx\n\nend"} {
		f.Add(text, uint8(28), uint8(3))
	}
	f.Fuzz(func(t *testing.T, text string, width, rows uint8) {
		if len(text) > 2048 {
			t.Skip()
		}
		columns, limit := 1+int(width%80), 1+int(rows%16)
		full := wrapPickerText(text, []int{0, 2, 5}, columns)
		bounded := wrapPickerTextRows(text, []int{0, 2, 5}, columns, limit)
		if !reflect.DeepEqual(bounded, full[:min(limit, len(full))]) {
			t.Fatalf("bounded wrapping changed visible rows at width %d, limit %d for %q", columns, limit, text)
		}
	})
}
