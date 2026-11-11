package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestPickerFrequencyGroupsExactCommandsWithinScope(t *testing.T) {
	all := parseHistoryData([]byte("git\ngit\npwd\ngit\ngit\ncafé\ncafe\n"))
	m := newPickerModel(all, all[1:], pickerOptions{sort: pickerFrequency, deleting: true, multi: true})
	if len(m.items) != 6 || len(m.matches) != 4 || m.commands != 4 {
		t.Fatal("frequency must group the display without losing individual scoped entries")
	}
	if item := m.items[m.focused()]; item.entry != all[4] || item.count != 3 || item.index != 5 {
		t.Fatalf("group did not use latest scoped metadata and counts: %+v", item)
	}
	searchPicker(t, m, "git")
	if len(m.matches) != 1 || m.items[m.focused()].count != 3 {
		t.Fatal("search changed the group's count or exposed duplicate rows")
	}
	got, ok := m.accept()
	if !ok || !slices.Equal(got, []*Entry{all[4], all[3], all[1]}) {
		t.Fatal("focused-group deletion lost an occurrence or included one outside the selection")
	}
	searchPicker(t, m, "cafe")
	if len(m.matches) != 2 {
		t.Fatal("search normalization must not merge distinct command text")
	}
}

func TestPickerDuplicateSearchViewsPreserveEntryIdentity(t *testing.T) {
	all := parseHistoryData([]byte(": 1000:0;echo Café\n: 1001:0;echo Cafe\n: 1002:0;echo café\n: 1003:0;echo Café\n"))
	for _, unique := range []bool{false, true} {
		m := newPickerModel(all, all, pickerOptions{unique: unique})
		if m.commands != 3 || m.items[0].count != 2 || m.items[0].entry != all[3] {
			t.Fatal("shared normalization merged distinct commands or lost latest metadata")
		}
		for _, item := range m.items {
			if item.text != newSearchText(CommandText(item.entry)) {
				t.Fatal("shared search views changed case or accent behavior")
			}
		}
		searchPicker(t, m, "Café")
		want := 2
		if unique {
			want = 1
		}
		if len(m.matches) != want || m.items[m.matches[0]].entry != all[3] {
			t.Fatal("case-sensitive accented search lost occurrence identity")
		}
		if !unique && m.items[m.matches[1]].entry != all[0] {
			t.Fatal("duplicate search views reused the latest entry pointer")
		}
	}
}

func TestPickerGroupedSelectionCountFitsNarrowTerminal(t *testing.T) {
	all := make([]*Entry, 10_000)
	for i := range all {
		all[i] = &Entry{Raw: []string{fmt.Sprint("command ", i)}}
	}
	m := newPickerModel(all, all, pickerOptions{multi: true, sort: pickerFrequency})
	m.width, m.height = 30, 10
	for i := range m.items {
		m.marked[i] = true
	}
	row := pickerANSI.ReplaceAllString(pickerFrameRows(m.render(false))[1], "")
	if glyphWidth(pickerGlyphs(row, nil)) >= m.width || !strings.Contains(row, "10000c/10000e") {
		t.Fatalf("group selection counts overflowed or disappeared: %q", row)
	}
}

func TestPickerFrequencyPartialMarksAndSortChanges(t *testing.T) {
	all := parseHistoryData([]byte("git\npwd\ngit\ngit\nnew\n"))
	m := newPickerModel(all, all, pickerOptions{multi: true, deleting: true})
	m.focus = 4 // Oldest git occurrence.
	m.toggle()
	m.focus = 3 // pwd.
	m.toggle()
	m.focus = 4
	m.options.sort = pickerFrequency
	searchPicker(t, m, "")
	if m.focused() != 1 || len(m.marked) != 2 || m.rowMark(m.focused()) != '-' {
		t.Fatal("grouping lost focus, changed individual marks, or hid partial selection")
	}
	frame := pickerANSI.ReplaceAllString(m.render(false), "")
	if !strings.Contains(frame, ">[-]") || !strings.Contains(frame, "2 commands · 2 entries selected") {
		t.Fatalf("partial selection or command/entry counts are missing: %q", frame)
	}
	got, ok := m.accept()
	if !ok || !slices.Equal(got, []*Entry{all[1], all[0]}) {
		t.Fatal("accepting existing partial marks expanded the deletion")
	}
	m.toggle() // A partial group selects all its occurrences.
	if len(m.marked) != 4 || m.selectedCommands() != 2 || m.rowMark(m.focused()) != 'x' {
		t.Fatal("toggling a partial group failed to select its remaining occurrences")
	}
	m.options.sort = pickerNewest
	searchPicker(t, m, "")
	if len(m.matches) != 5 || len(m.marked) != 4 {
		t.Fatal("leaving frequency mode failed to restore occurrences and their marks")
	}
	m.options.sort = pickerFrequency
	searchPicker(t, m, "")
	m.toggle() // A full group clears all its occurrences, leaving pwd marked.
	if len(m.marked) != 1 || !m.marked[3] || m.rowMark(m.focused()) != ' ' {
		t.Fatal("clearing a group changed another command's selection")
	}
	searchPicker(t, m, "no matches")
	if got, ok := m.accept(); !ok || !slices.Equal(got, []*Entry{all[1]}) {
		t.Fatal("frequency grouping lost query-hidden individual marks")
	}
}

func TestPickerFrequencyMultiOutputAndUniqueModes(t *testing.T) {
	all := parseHistoryData([]byte("git\npwd\ngit\ngit\nnew\n"))
	for _, unique := range []bool{false, true} {
		m := newPickerModel(all, all, pickerOptions{multi: true, unique: unique, sort: pickerFrequency})
		m.toggle() // git, the most frequent command.
		m.move(1)
		m.toggle() // new, the newest singleton.
		searchPicker(t, m, "hidden")
		if got, ok := m.accept(); !ok || !slices.Equal(got, []*Entry{all[4], all[3]}) {
			t.Fatal("frequency multi-output did not emit distinct selected commands newest-first")
		}
		m.options.sort = pickerNewest
		searchPicker(t, m, "")
		wantRows := 5
		want := []*Entry{all[4], all[3], all[2], all[0]}
		if unique {
			wantRows = 3
			want = []*Entry{all[4], all[3]}
		}
		if len(m.matches) != wantRows {
			t.Fatal("newest mode failed to honor the explicit unique option")
		}
		if got, ok := m.accept(); !ok || !slices.Equal(got, want) {
			t.Fatal("non-frequency output failed to preserve its existing occurrence semantics")
		}
	}
}
