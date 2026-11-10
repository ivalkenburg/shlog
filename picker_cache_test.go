package main

import (
	"fmt"
	"strings"
	"testing"
)

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
