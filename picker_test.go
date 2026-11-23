package main

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

func searchPicker(t *testing.T, m *pickerModel, text string) {
	t.Helper()
	previous := m.focused()
	m.input, m.cursor = []rune(text), len([]rune(text))
	m.setQuery(text)
	m.matches = nil
	if m.queryErr == nil {
		matches, _ := filterPicker(context.Background(), m.items, m.query, m.options.sort)
		m.applyMatches(matches, previous)
	}
}

func TestPickerScopeOrderAndHiddenMarks(t *testing.T) {
	all := []*Entry{{Raw: []string{"outside"}}, {Raw: []string{"old command"}}, {Raw: []string{"new command"}}}
	m := newPickerModel(all, all[1:], pickerOptions{multi: true})
	if m.items[0].index != 3 || m.items[1].index != 2 {
		t.Fatal("picker did not preserve full-history indices newest-first")
	}
	m.move(1)
	m.toggle() // Mark old first.
	m.move(-1)
	m.toggle()
	searchPicker(t, m, "no matches")
	got, accepted := m.accept()
	if !accepted || !slices.Equal(got, []*Entry{all[2], all[1]}) {
		t.Fatalf("hidden marks = %v, %t", got, accepted)
	}
	searchPicker(t, m, "new |")
	if _, accepted := m.accept(); accepted {
		t.Fatal("incomplete query accepted hidden marks")
	}
	searchPicker(t, m, "old")
	m.toggle()
	searchPicker(t, m, "new")
	m.toggle()
	if len(m.marked) != 0 {
		t.Fatal("marks did not survive query changes")
	}
	searchPicker(t, m, "no matches")
	if _, accepted := m.accept(); accepted {
		t.Fatal("accepted an empty result without marks")
	}
	searchPicker(t, m, "new")
	if got, ok := m.accept(); !ok || !slices.Equal(got, all[2:]) {
		t.Fatal("focused-entry fallback failed")
	}
	m.searching = true
	if _, ok := m.accept(); ok {
		t.Fatal("accepted stale search results")
	}
}

func TestPickerPoolPreservesIdentityAndScope(t *testing.T) {
	all := []*Entry{{Raw: []string{"old"}}, {Raw: []string{"middle"}}, {Raw: []string{"new"}}}
	outside := &Entry{Raw: []string{"outside"}}
	for _, tt := range []struct {
		name string
		pool []*Entry
		want []*Entry
	}{
		{"all", all, []*Entry{all[2], all[1], all[0]}},
		{"copied all", slices.Clone(all), []*Entry{all[2], all[1], all[0]}},
		{"reordered", []*Entry{all[2], all[0], all[1]}, []*Entry{all[2], all[1], all[0]}},
		{"duplicate", []*Entry{all[0], all[0], all[2]}, []*Entry{all[2], all[0]}},
		{"outside", []*Entry{outside, all[0], all[2]}, []*Entry{all[2], all[0]}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := newPickerModel(all, tt.pool, pickerOptions{})
			got := make([]*Entry, len(m.items))
			for i := range m.items {
				got[i] = m.items[i].entry
			}
			if !slices.Equal(got, tt.want) {
				t.Fatal("pool optimization changed scope, entry identity, or history order")
			}
		})
	}
}

func TestPickerPendingAcceptance(t *testing.T) {
	all := []*Entry{{Raw: []string{"git status"}}}
	m := newPickerModel(all, all, pickerOptions{multi: true})
	m.searching, m.matches = true, nil
	if _, accepted := m.accept(); accepted || !m.acceptPending {
		t.Fatal("Enter did not wait for the current search")
	}
	m.applyMatches([]int{0}, -1)
	if !m.acceptPending {
		t.Fatal("current results lost a pending Enter")
	}
	if entries, accepted := m.accept(); !accepted || !slices.Equal(entries, all) || m.acceptPending {
		t.Fatal("pending Enter did not accept current results exactly once")
	}

	for _, key := range []string{"up", "left", "help", "text", "paste"} {
		m.help, m.searching = false, true
		m.accept()
		m.key(pickerEvent{key: key, text: "x"})
		if m.acceptPending {
			t.Errorf("%s did not cancel pending acceptance", key)
		}
	}
	m.help, m.searching = false, true
	m.accept()
	m.mouse(pickerEvent{mouseButton: 64}, time.Now())
	if m.acceptPending {
		t.Fatal("mouse interaction did not cancel pending acceptance")
	}
	for _, query := range []string{"new query", "git |"} {
		m.setQuery("")
		m.searching = true
		m.accept()
		m.setQuery(query)
		if m.acceptPending {
			t.Fatal("pending acceptance survived a query generation change")
		}
	}

	m.setQuery("no match")
	m.marked[0] = true
	m.searching = true
	m.accept()
	m.applyMatches(nil, -1)
	if entries, accepted := m.accept(); !accepted || !slices.Equal(entries, all) {
		t.Fatal("pending Enter lost hidden marks after an empty result")
	}
}

func TestPickerEmptyPool(t *testing.T) {
	t.Setenv("TERM", "dumb")
	if got, err := pickEntries(makeEntries(1), nil, pickerOptions{}); len(got) != 0 || err != nil {
		t.Fatalf("empty pool should not need a terminal: %v, %v", got, err)
	}
}

func TestPickerEditingAndPaste(t *testing.T) {
	m := newPickerModel(nil, nil, pickerOptions{})
	m.key(pickerEvent{key: "paste", text: "café\nmain\tstatus\x1b\x03"})
	if string(m.input) != "café main status" {
		t.Fatalf("paste = %q", string(m.input))
	}
	m.key(pickerEvent{key: "ctrl-w"})
	m.key(pickerEvent{key: "ctrl-a"})
	m.key(pickerEvent{key: "delete"})
	m.key(pickerEvent{key: "text", text: "C"})
	m.key(pickerEvent{key: "end"})
	m.key(pickerEvent{key: "backspace"})
	if string(m.input) != "Café main" {
		t.Fatalf("editing = %q", string(m.input))
	}
	m.key(pickerEvent{key: "ctrl-u"})
	if len(m.input) != 0 || m.cursor != 0 {
		t.Fatal("ctrl-u did not clear the query")
	}
}

func TestPickerMouseAndPreview(t *testing.T) {
	all := []*Entry{{Raw: []string{"echo old"}}, {Raw: []string{"start", "", "", "needle", "end"}}}
	m := newPickerModel(all, all, pickerOptions{multi: true})
	m.height = 12
	searchPicker(t, m, "needle")
	if m.previewScroll != 2 {
		t.Fatalf("preview did not reveal matching line with context: %d", m.previewScroll)
	}
	m.mouse(pickerEvent{mouseButton: 0, x: 2, y: 3}, time.Now())
	if len(m.marked) != 1 {
		t.Fatal("selection marker did not toggle")
	}
	now := time.Now()
	e := pickerEvent{mouseButton: 0, x: 8, y: 3}
	if m.mouse(e, now) || !m.mouse(e, now.Add(100*time.Millisecond)) {
		t.Fatal("double click did not accept")
	}
	m.mouse(pickerEvent{mouseButton: 64, y: m.previewTop()}, now)
	if m.previewScroll != 0 {
		t.Fatal("wheel over preview did not scroll preview")
	}
	searchPicker(t, m, "")
	m.mouse(pickerEvent{mouseButton: 65, y: 3}, now)
	if m.focus != 1 {
		t.Fatal("wheel did not move through the results")
	}
}

func TestPickerDoubleClickRequiresConsecutiveRowClicks(t *testing.T) {
	actions := map[string]func(*pickerModel){
		"key":          func(m *pickerModel) { m.key(pickerEvent{key: "left"}) },
		"query":        func(m *pickerModel) { searchPicker(t, m, "target") },
		"sort":         func(m *pickerModel) { m.cycleSort() },
		"resize":       func(m *pickerModel) { m.resize(80, 24) },
		"wheel":        func(m *pickerModel) { m.mouse(pickerEvent{mouseButton: 64}, time.Now()) },
		"header":       func(m *pickerModel) { m.mouse(pickerEvent{y: 0}, time.Now()) },
		"other button": func(m *pickerModel) { m.mouse(pickerEvent{mouseButton: 2, y: 3}, time.Now()) },
		"motion":       func(m *pickerModel) { m.mouse(pickerEvent{mouseButton: 32, y: 3}, time.Now()) },
		"help": func(m *pickerModel) {
			m.key(pickerEvent{key: "help"})
			m.key(pickerEvent{key: "escape"})
		},
	}
	for name, action := range actions {
		t.Run(name, func(t *testing.T) {
			all := []*Entry{{Raw: []string{"target"}}}
			m := newPickerModel(all, all, pickerOptions{})
			e := pickerEvent{key: "mouse", x: 8, y: 3}
			now := time.Now()
			if m.mouse(e, now) {
				t.Fatal("first click accepted a command")
			}
			action(m)
			if m.mouse(e, now.Add(100*time.Millisecond)) {
				t.Fatal("intervening action left a stale double-click candidate")
			}
			m.mouse(pickerEvent{mouseRelease: true}, now.Add(150*time.Millisecond))
			if !m.mouse(e, now.Add(200*time.Millisecond)) {
				t.Fatal("consecutive row clicks with a release no longer accept")
			}
		})
	}
}

func TestPickerSafeRendering(t *testing.T) {
	text := "echo \x1b]52;c;secret\a\r\u202efake\n你好 cafe\u0301 👩‍💻"
	lines := wrapPickerText(text, []int{5, 6}, 20)
	for _, line := range lines {
		plain := renderPickerGlyphs(line.glyphs, 20, false, "")
		// Highlight escape sequences are trusted; source ESC/CR/BEL/bidi are text.
		plain = strings.ReplaceAll(plain, "\x1b[0m", "")
		plain = strings.ReplaceAll(plain, "\x1b[1;4m", "")
		if strings.ContainsAny(plain, "\x1b\a\r\u202e") || glyphWidth(line.glyphs) > 20 {
			t.Fatalf("unsafe or overwide preview: %q", plain)
		}
	}
	if got := glyphWidth(pickerGlyphs("你好 cafe\u0301 👩‍💻 🇳🇱", nil)); got != 15 {
		t.Fatalf("Unicode cell width = %d; want 15", got)
	}
	all := []*Entry{{Raw: []string{text}}}
	m := newPickerModel(all, all, pickerOptions{multi: true, path: "/tmp/history"})
	for _, size := range [][2]int{{30, 10}, {80, 24}, {160, 48}} {
		m.width, m.height = size[0], size[1]
		for _, help := range []bool{false, true} {
			m.help = help
			frame := m.render(false)
			if strings.Contains(frame, "\x1b[36m") || strings.Contains(frame, "\x1b]52") {
				t.Fatal("NO_COLOR or control-byte rendering was bypassed")
			}
		}
	}
}

var pickerRowStart = regexp.MustCompile("\x1b\\[[0-9]+;1H\x1b\\[2K")
var pickerANSI = regexp.MustCompile("\x1b\\[[0-9;?]*[A-Za-z]")

func pickerFrameRows(frame string) []string {
	starts := pickerRowStart.FindAllStringIndex(frame, -1)
	rows := make([]string, len(starts))
	for i, start := range starts {
		end := len(frame)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		rows[i] = frame[start[1]:end]
	}
	return rows
}

func TestPickerStatesFitTerminal(t *testing.T) {
	all := []*Entry{{Raw: []string{"git\nmain"}}}
	for _, size := range [][2]int{{30, 10}, {30, 11}, {40, 14}, {80, 24}, {160, 48}} {
		for _, state := range []string{"results", "hidden marks", "incomplete query", "searching", "help"} {
			m := newPickerModel(all, all, pickerOptions{multi: true, path: "/tmp/history"})
			m.width, m.height = size[0], size[1]
			switch state {
			case "hidden marks":
				m.toggle()
				searchPicker(t, m, "no matches")
			case "incomplete query":
				searchPicker(t, m, "git |")
			case "searching":
				m.matches, m.searching = nil, true
			case "help":
				m.help = true
			}
			rows := pickerFrameRows(m.render(false))
			if len(rows) != m.height {
				t.Fatalf("%v %s: rendered %d rows", size, state, len(rows))
			}
			for i, row := range rows {
				plain := pickerANSI.ReplaceAllString(row, "")
				if width := glyphWidth(pickerGlyphs(plain, nil)); width >= m.width {
					t.Errorf("%v %s row %d: %d cells would wrap: %q", size, state, i, width, plain)
				}
			}
			if !m.help && pickerANSI.ReplaceAllString(rows[m.previewTop()-2], "") != strings.Repeat("─", m.width-1) {
				t.Errorf("%v %s: details heading overwrote pane divider", size, state)
			}
		}
	}
}

func TestPickerMultilineMatchDoesNotHighlightPreviewMarker(t *testing.T) {
	for _, text := range []string{"git\nmain", "café\nmain"} {
		all := []*Entry{{Raw: []string{text}}}
		m := newPickerModel(all, all, pickerOptions{})
		searchPicker(t, m, "m")
		rows := pickerFrameRows(m.render(true))
		if strings.Contains(rows[3], "\x1b[1;4m") {
			t.Fatalf("continuation-only match highlighted the list marker: %q", rows[3])
		}
		if !strings.Contains(strings.Join(rows[m.previewTop():m.previewTop()+m.previewRows()], ""), "\x1b[1;4m") {
			t.Fatal("continuation match was not highlighted in command details")
		}
	}
}

func TestPickerDecoderCoalescesTextWithoutReorderingControls(t *testing.T) {
	text := strings.Repeat("git café ", 256)
	d := pickerDecoder{}
	events := d.feed([]byte(text+"\tx"), false)
	if len(events) != 3 || events[0].key != "text" || events[0].text != text || events[1].key != "tab" || events[2].text != "x" {
		t.Fatal("text burst was split or reordered around a control key")
	}
	d = pickerDecoder{}
	events = d.feed([]byte("a\xc3"), false)
	events = append(events, d.feed([]byte("\xa9b"), false)...)
	if len(events) != 2 || events[0].text != "a" || events[1].text != "éb" {
		t.Fatalf("coalescing corrupted split UTF-8: %+v", events)
	}
}

func BenchmarkPickerDecode(b *testing.B) {
	for _, item := range []struct{ name, input string }{
		{"arrow", "\x1b[A"}, {"control", "\x10"}, {"text burst", strings.Repeat("git status ", 256)},
	} {
		b.Run(item.name, func(b *testing.B) {
			data := []byte(item.input)
			b.ReportAllocs()
			for b.Loop() {
				d := pickerDecoder{}
				d.feed(data, false)
			}
		})
	}
}

func TestPickerDecoderChunkBoundaries(t *testing.T) {
	for _, tt := range []struct {
		sequence string
		key      string
		text     string
	}{
		{"\x1b[A", "up", ""}, {"\x1bOP", "help", ""}, {"\x1b[[A", "help", ""},
		{"\x1b[1;3B", "alt-down", ""}, {"\x1b\x1b[A", "alt-up", ""},
		{"é", "text", "é"}, {"\x1b[200~hello\n\x1b[A\x03\x1b[201~", "paste", "hello\n\x1b[A\x03"},
		{"\x1b[<65;10;20M", "mouse", ""},
	} {
		for split := 0; split <= len(tt.sequence); split++ {
			d := pickerDecoder{}
			got := d.feed([]byte(tt.sequence[:split]), false)
			got = append(got, d.feed([]byte(tt.sequence[split:]), false)...)
			if len(got) != 1 || got[0].key != tt.key || got[0].text != tt.text {
				t.Fatalf("decode %q split %d: %+v", tt.sequence, split, got)
			}
			if tt.key == "mouse" && (got[0].x != 9 || got[0].y != 19 || got[0].mouseButton != 65) {
				t.Fatalf("bad mouse coordinates: %+v", got[0])
			}
		}
	}
	d := pickerDecoder{}
	if len(d.feed([]byte("\x1b"), false)) != 0 || d.feed(nil, true)[0].key != "escape" {
		t.Fatal("standalone Escape did not time out")
	}
	if events := d.feed([]byte("\x1b[<0;1;1m"), false); len(events) != 1 || !events[0].mouseRelease {
		t.Fatal("mouse release must be distinguished from a click")
	}
	for _, text := range []string{"git", "é"} {
		d := pickerDecoder{}
		events := d.feed([]byte("\x1b"+text), false)
		if len(events) < 2 || events[0].key != "escape" || events[1].key != "text" {
			t.Fatalf("Escape followed by %q lost input: %+v", text, events)
		}
		var typed strings.Builder
		for _, e := range events[1:] {
			typed.WriteString(e.text)
		}
		if typed.String() != text {
			t.Fatalf("Escape followed by %q became %q", text, typed.String())
		}
	}
}

func TestPickerNoOpEditsDoNotTriggerSearch(t *testing.T) {
	m := newPickerModel(nil, nil, pickerOptions{})
	for _, key := range []string{"ctrl-u", "ctrl-w", "backspace", "delete"} {
		if m.key(pickerEvent{key: key}) {
			t.Errorf("%s on empty input reported query change", key)
		}
	}
	m.key(pickerEvent{key: "text", text: "git"})
	m.key(pickerEvent{key: "home"})
	if m.key(pickerEvent{key: "ctrl-w"}) {
		t.Error("ctrl-w at cursor 0 reported query change")
	}
	if m.key(pickerEvent{key: "backspace"}) {
		t.Error("backspace at cursor 0 reported query change")
	}
	m.key(pickerEvent{key: "end"})
	if m.key(pickerEvent{key: "delete"}) {
		t.Error("delete at end of input reported query change")
	}
	if !m.key(pickerEvent{key: "ctrl-w"}) || len(m.input) != 0 {
		t.Error("ctrl-w with word before cursor did not report query change")
	}
	m.key(pickerEvent{key: "text", text: "git"})
	if !m.key(pickerEvent{key: "ctrl-u"}) || len(m.input) != 0 {
		t.Error("ctrl-u with non-empty input did not report query change")
	}
}

func TestPickerKeyNavigationAndHelp(t *testing.T) {
	all := []*Entry{
		{Raw: []string{"one"}},
		{Raw: []string{"two"}},
		{Raw: []string{"line1", "line2", "line3", "line4", "line5", "line6"}},
	}
	m := newPickerModel(all, all, pickerOptions{multi: true})
	m.width, m.height = 80, 14

	// Preview scroll and clicking the already-focused row preserves previewScroll.
	m.key(pickerEvent{key: "alt-down"})
	if m.previewScroll != 1 {
		t.Fatalf("alt-down previewScroll = %d, want 1", m.previewScroll)
	}
	m.mouse(pickerEvent{mouseButton: 0, x: 2, y: 3}, time.Now())
	if m.previewScroll != 1 {
		t.Fatalf("clicking already-focused row reset previewScroll to %d", m.previewScroll)
	}
	m.key(pickerEvent{key: "alt-up"})
	if m.previewScroll != 0 {
		t.Fatalf("alt-up previewScroll = %d, want 0", m.previewScroll)
	}

	// Page navigation and back-tab.
	m.key(pickerEvent{key: "page-down"})
	if m.focus != 2 {
		t.Fatalf("page-down focus = %d, want 2", m.focus)
	}
	m.key(pickerEvent{key: "back-tab"})
	if m.focus != 1 || !m.marked[2] {
		t.Fatalf("back-tab focus=%d marked=%v", m.focus, m.marked)
	}
	m.key(pickerEvent{key: "page-up"})
	if m.focus != 0 {
		t.Fatalf("page-up focus = %d, want 0", m.focus)
	}

	// Cursor movement keys.
	m.key(pickerEvent{key: "text", text: "ab"})
	m.key(pickerEvent{key: "home"})
	m.key(pickerEvent{key: "right"})
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.cursor)
	}
	m.key(pickerEvent{key: "ctrl-e"})
	if m.cursor != 2 {
		t.Fatalf("ctrl-e cursor = %d, want 2", m.cursor)
	}

	// Help mode scrolling and toggle.
	m.key(pickerEvent{key: "help"})
	if !m.help {
		t.Fatal("help key did not open help")
	}
	m.key(pickerEvent{key: "down"})
	if m.helpScroll != 1 {
		t.Fatalf("help down scroll = %d, want 1", m.helpScroll)
	}
	m.key(pickerEvent{key: "up"})
	if m.helpScroll != 0 {
		t.Fatalf("help up scroll = %d, want 0", m.helpScroll)
	}
	m.key(pickerEvent{key: "page-down"})
	if m.helpScroll == 0 {
		t.Fatal("help page-down did not scroll")
	}
	m.key(pickerEvent{key: "page-up"})
	if m.helpScroll != 0 {
		t.Fatalf("help page-up scroll = %d, want 0", m.helpScroll)
	}
	m.key(pickerEvent{key: "help"})
	if m.help {
		t.Fatal("help key did not close help")
	}
}

func TestPickerDecoderAllControlsAndSequences(t *testing.T) {
	controls := map[byte]string{
		1: "ctrl-a", 3: "cancel", 4: "delete", 5: "ctrl-e",
		8: "backspace", 9: "tab", 10: "enter", 12: "redraw",
		13: "enter", 14: "ctrl-n", 16: "ctrl-p", 19: "ctrl-s",
		21: "ctrl-u", 23: "ctrl-w", 26: "suspend", 127: "backspace",
		2: "", // unmapped control byte
	}
	d := pickerDecoder{}
	for b, want := range controls {
		events := d.feed([]byte{b}, false)
		if want == "" {
			if len(events) != 0 {
				t.Errorf("control byte %d produced unexpected events: %+v", b, events)
			}
		} else if len(events) != 1 || events[0].key != want {
			t.Errorf("control byte %d = %+v, want %q", b, events, want)
		}
	}

	sequences := map[string]string{
		"\x1b[A": "up", "\x1bOA": "up",
		"\x1b[B": "down", "\x1bOB": "down",
		"\x1b[C": "right", "\x1bOC": "right",
		"\x1b[D": "left", "\x1bOD": "left",
		"\x1b[H": "home", "\x1bOH": "home", "\x1b[1~": "home", "\x1b[7~": "home",
		"\x1b[F": "end", "\x1bOF": "end", "\x1b[4~": "end", "\x1b[8~": "end",
		"\x1b[3~": "delete", "\x1b[5~": "page-up", "\x1b[6~": "page-down",
		"\x1b[Z": "back-tab",
		"\x1bOP": "help", "\x1b[11~": "help", "\x1b[[A": "help",
		"\x1b[1;3A": "alt-up", "\x1b[1;3B": "alt-down",
		"\x1b[999~": "", "\x1b[<0;1M": "", "\x1b[<;;M": "", "\x1b[<0;0;1M": "",
	}
	for seq, want := range sequences {
		events := d.feed([]byte(seq), false)
		if want == "" {
			if len(events) != 0 {
				t.Errorf("sequence %q produced unexpected events: %+v", seq, events)
			}
		} else if len(events) != 1 || events[0].key != want {
			t.Errorf("sequence %q = %+v, want %q", seq, events, want)
		}
	}

	// Overlong incomplete CSI sequence is discarded rather than stalling.
	if events := d.feed([]byte("\x1b["+strings.Repeat("1;", 80)), false); len(events) != 0 || len(d.pending) != 0 {
		t.Fatalf("overlong CSI was not discarded: events=%+v pending=%d", events, len(d.pending))
	}
}
