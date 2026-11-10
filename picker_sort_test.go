package main

import (
	"context"
	"slices"
	"strings"
	"testing"
)

func TestPickerUniqueScopedRepresentatives(t *testing.T) {
	all := parseHistoryData([]byte(": 1000:0;echo first\n: 1001:0;echo same\n: 1002:0;echo same\n: 1003:0;echo first\n"))
	m := newPickerModel(all, all[1:], pickerOptions{unique: true, multi: true})
	if len(m.items) != 2 || m.items[0].entry != all[3] || m.items[0].count != 1 || m.items[1].entry != all[2] || m.items[1].count != 2 {
		t.Fatalf("unexpected groups: %+v", m.items)
	}
	m.toggle()
	m.move(1)
	m.toggle()
	searchPicker(t, m, "hidden")
	got, ok := m.accept()
	if !ok || !slices.Equal(got, []*Entry{all[3], all[2]}) {
		t.Fatal("unique selection lost representatives or hidden marks")
	}
	frame := pickerANSI.ReplaceAllString(m.render(false), "")
	if !strings.Contains(frame, "unique") {
		t.Fatal("unique mode missing from header")
	}
	searchPicker(t, m, "echo same")
	frame = pickerANSI.ReplaceAllString(m.render(false), "")
	if !strings.Contains(frame, "×2") || !strings.Contains(frame, "2 uses") || !strings.Contains(frame, "#3") {
		t.Fatalf("missing count/latest entry metadata: %q", frame)
	}
	assert := newPickerModel(all, all, pickerOptions{deleting: true})
	if len(assert.items) != 4 {
		t.Fatal("deletion collapsed individual entries")
	}
}

func TestPickerSortOrder(t *testing.T) {
	for _, tt := range []struct {
		name, history, query string
		order                pickerSort
		unique               bool
		want                 []int
	}{
		{"newest", "git checkout\ngiant careful operation\n", "gco", pickerNewest, false, []int{2, 1}},
		{"relevance", "git checkout\ngiant careful operation\n", "gco", pickerRelevance, false, []int{1, 2}},
		{"exact", "git status\necho git status\n", "'git", pickerRelevance, false, []int{1, 2}},
		{"shorter", "git\ngit status\n", "git", pickerRelevance, false, []int{1, 2}},
		{"recency tie", "git a\ngit b\n", "git", pickerRelevance, false, []int{2, 1}},
		{"empty relevance", "long old command\nx\n", "", pickerRelevance, false, []int{2, 1}},
		{"negative only", "long old command\nx\n", "!sudo", pickerRelevance, false, []int{2, 1}},
		{"frequency", "git\ngit\npwd\n", "", pickerFrequency, false, []int{2, 1, 3}},
		{"unique frequency", "git\ngit\npwd\n", "", pickerFrequency, true, []int{2, 3}},
		{"frequency tie", "git\npwd\ngit\npwd\n", "", pickerFrequency, true, []int{4, 3}},
		{"normalized relevance", "café\necho cafe\n", "cafe", pickerRelevance, false, []int{1, 2}},
		{"negative OR", "git\npwd\nsudo\n", "git | !sudo", pickerRelevance, false, []int{1, 2}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			all := parseHistoryData([]byte(tt.history))
			m := newPickerModel(all, all, pickerOptions{sort: tt.order, unique: tt.unique})
			searchPicker(t, m, tt.query)
			got := make([]int, len(m.matches))
			for i, at := range m.matches {
				got[i] = m.items[at].index
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("order = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPickerInteractiveSortPreservesMarksAndFocus(t *testing.T) {
	all := parseHistoryData([]byte("git checkout\ngiant careful operation\ngit checkout\npwd\n"))
	m := newPickerModel(all, all, pickerOptions{multi: true})
	searchPicker(t, m, "gco")
	m.toggle()
	focused := m.focused()
	if !m.key(pickerEvent{key: "ctrl-s"}) || m.options.sort != pickerRelevance {
		t.Fatal("Ctrl-S did not change sorting")
	}
	matches, ok := filterPicker(context.Background(), m.items, m.query, m.options.sort)
	if !ok {
		t.Fatal("filter canceled")
	}
	m.applyMatches(matches, focused)
	if m.focused() != focused || !m.marked[focused] {
		t.Fatal("sort lost focus or marks")
	}
	badge, start := m.sortControl()
	if badge != "[ relevance ]" || !m.clickedSort(pickerEvent{key: "mouse", x: start, y: 0}) || m.clickedSort(pickerEvent{x: start - 1, y: 0}) {
		t.Fatal("invalid sort control hit target")
	}
	m.cycleSort()
	if m.options.sort != pickerFrequency {
		t.Fatal("sort did not cycle to frequency")
	}
	m.cycleSort()
	if m.options.sort != pickerNewest {
		t.Fatal("sort did not wrap to newest")
	}
	m.help = true
	if m.clickedSort(pickerEvent{x: start, y: 0}) {
		t.Fatal("help accepted sort click")
	}
}

func TestPickerSortControlFits(t *testing.T) {
	all := parseHistoryData([]byte("git\ngit\npwd\n"))
	for _, width := range []int{30, 40, 65, 80} {
		for _, order := range []pickerSort{pickerNewest, pickerRelevance, pickerFrequency} {
			m := newPickerModel(all, all, pickerOptions{unique: true, multi: true, sort: order, path: "history"})
			m.width = width
			badge, _ := m.sortControl()
			for _, row := range pickerFrameRows(m.render(false)) {
				plain := pickerANSI.ReplaceAllString(row, "")
				if glyphWidth(pickerGlyphs(plain, nil)) >= width {
					t.Fatalf("%s at %d overflows: %q", order, width, plain)
				}
			}
			first := pickerANSI.ReplaceAllString(pickerFrameRows(m.render(false))[0], "")
			if !strings.Contains(first, badge) {
				t.Fatalf("sort control truncated at width %d: %q", width, first)
			}
		}
	}
}
