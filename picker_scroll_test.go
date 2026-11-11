package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func scrollbarPicker(count int) *pickerModel {
	all := make([]*Entry, count)
	for i := range all {
		all[i] = &Entry{Raw: []string{fmt.Sprintf("command %d", i), strings.Repeat("preview line\n", 29)}}
	}
	return newPickerModel(all, all, pickerOptions{multi: true})
}

func pickerMouse(m *pickerModel, button, x, y int) bool {
	e := pickerEvent{key: "mouse", mouseButton: button, x: x, y: y}
	accepted := m.mouse(e, time.Now())
	m.key(e) // The event loop also forwards mouse events to key.
	return accepted
}

func TestPickerScrollbarGeometry(t *testing.T) {
	for _, tt := range []struct {
		rows, total, offset, thumbRows, thumbStart int
	}{
		{10, 0, 0, 0, 0}, {10, 10, 0, 0, 0},
		{10, 11, 0, 9, 0}, {10, 11, 1, 9, 1},
		{10, 100, 0, 1, 0}, {10, 100, 45, 1, 5}, {10, 100, 90, 1, 9},
		{10, 20, 5, 5, 3}, {10, 20, 999, 5, 5},
		{1, 100, 0, 1, 0},
	} {
		b := newPickerScrollbar(pickerResultsPanel, 3, tt.rows, tt.total, tt.offset)
		if b.thumbRows != tt.thumbRows || b.thumbStart != tt.thumbStart {
			t.Errorf("%+v: thumb size=%d start=%d", tt, b.thumbRows, b.thumbStart)
		}
	}
}

func TestPickerScrollbarJumpDragAndRelease(t *testing.T) {
	for _, panel := range []pickerPanel{pickerResultsPanel, pickerPreviewPanel, pickerHelpPanel} {
		t.Run(fmt.Sprint(panel), func(t *testing.T) {
			m := scrollbarPicker(100)
			m.help = panel == pickerHelpPanel
			b := m.scrollbar(panel)
			x, y := m.width-2, b.top+b.rows/2
			if pickerMouse(m, 0, x, y) || len(m.marked) != 0 || m.drag.panel != panel {
				t.Fatal("rail click accepted, marked, or failed to capture the panel")
			}
			jumped := m.scrollbar(panel)
			if jumped.offset == 0 || jumped.offset == jumped.maxOffset() {
				t.Fatal("middle rail click did not jump inside the content")
			}
			if jumped.top+jumped.thumbStart+jumped.thumbRows/2 != y {
				t.Fatal("rail jump did not center the thumb at the pointer")
			}
			pickerMouse(m, 32, 0, m.height+100) // Keep capture outside the panel.
			bottom := m.scrollbar(panel)
			if bottom.offset != bottom.maxOffset() || bottom.thumbStart+bottom.thumbRows != bottom.rows {
				t.Fatal("dragging beyond the bottom did not reach the end")
			}
			if panel == pickerResultsPanel && m.focus != m.offset {
				t.Fatal("focused command did not move to the nearest visible result")
			}
			if panel != pickerResultsPanel && m.focus != 0 {
				t.Fatal("preview/help drag changed result focus")
			}
			pickerMouse(m, 32, 0, -100)
			if m.scrollbar(panel).offset != 0 {
				t.Fatal("dragging beyond the top did not reach the beginning")
			}
			if m.mouse(pickerEvent{mouseRelease: true, x: 0, y: 0}, time.Now()) || m.drag.panel != pickerNoPanel {
				t.Fatal("release accepted a result or failed to end dragging")
			}
			pickerMouse(m, 32, x, m.height+100)
			if m.scrollbar(panel).offset != 0 {
				t.Fatal("motion after release kept dragging")
			}
		})
	}
}

func TestPickerScrollbarThumbGrabAndFocus(t *testing.T) {
	m := scrollbarPicker(20)
	m.focus, m.offset = 5, 2
	m.previewScroll = 7
	b := m.scrollbar(pickerResultsPanel)
	y := b.top + b.thumbStart + b.thumbRows - 1
	pickerMouse(m, 0, m.width-2, y)
	pickerMouse(m, 32, 0, y) // Horizontal movement must not quantize the offset.
	if m.offset != 2 || m.focus != 5 || m.previewScroll != 7 {
		t.Fatal("grabbing the thumb changed the viewport, focus, or preview")
	}
	pickerMouse(m, 32, 0, y+1)
	if m.offset != 4 || m.focus != 5 || m.previewScroll != 7 {
		t.Fatalf("grab offset or visible focus was lost: offset=%d focus=%d preview=%d", m.offset, m.focus, m.previewScroll)
	}
	pickerMouse(m, 32, 0, 100)
	if m.offset != 9 || m.focus != 9 || m.previewScroll != 0 {
		t.Fatal("scrolling focus offscreen did not move it and reset the preview")
	}
}

func TestPickerScrollbarSingleRowDrag(t *testing.T) {
	m := scrollbarPicker(100)
	m.resize(30, 10)
	pickerMouse(m, 0, m.width-2, 3)
	pickerMouse(m, 32, 0, 4)
	if m.offset != 99 || m.focus != 99 {
		t.Fatal("dragging below a one-row rail did not reach the last result")
	}
	pickerMouse(m, 32, 0, 2)
	if m.offset != 0 || m.focus != 0 {
		t.Fatal("dragging above a one-row rail did not reach the first result")
	}
}

func TestPickerScrollbarCannotAcceptOrMark(t *testing.T) {
	for _, count := range []int{20, 100} {
		m := scrollbarPicker(count)
		x, y := m.width-2, 3
		pickerMouse(m, 0, 8, y) // A row click followed by a rail click isn't a double click.
		for range 2 {
			if pickerMouse(m, 0, x, y) || len(m.marked) != 0 {
				t.Fatal("rail click accepted or marked a command")
			}
		}
		if pickerMouse(m, 0, 8, y) {
			t.Fatal("rail interaction left a stale double-click candidate")
		}
	}
	// Without overflow, the former rail column is ordinary clickable content.
	fitting := scrollbarPicker(2)
	if pickerMouse(fitting, 0, fitting.width-2, 4) || fitting.focus != 1 || fitting.drag.panel != pickerNoPanel {
		t.Fatal("fitting content kept an invisible scrollbar hit target")
	}
	m := scrollbarPicker(100)
	_, start := m.sortControl()
	if m.clickedSort(pickerEvent{mouseRelease: true, x: start, y: 0}) {
		t.Fatal("releasing over the sort control would change sorting")
	}
	for _, button := range []int{66, 67} {
		pickerMouse(m, button, 10, 3)
		if m.focus != 0 || m.offset != 0 {
			t.Fatal("horizontal wheel moved the vertical viewport")
		}
	}
}

func TestPickerScrollbarDragCancellation(t *testing.T) {
	actions := map[string]func(*pickerModel){
		"key":     func(m *pickerModel) { m.key(pickerEvent{key: "left"}) },
		"help":    func(m *pickerModel) { m.key(pickerEvent{key: "help"}) },
		"query":   func(m *pickerModel) { m.setQuery("command") },
		"sort":    func(m *pickerModel) { m.cycleSort() },
		"results": func(m *pickerModel) { m.applyMatches(m.matches, m.focused()) },
		"resize":  func(m *pickerModel) { m.resize(50, 14) },
		"wheel":   func(m *pickerModel) { pickerMouse(m, 65, 10, 3) },
		"click":   func(m *pickerModel) { pickerMouse(m, 0, 10, 3) },
	}
	for name, action := range actions {
		t.Run(name, func(t *testing.T) {
			m := scrollbarPicker(100)
			pickerMouse(m, 0, m.width-2, 3)
			action(m)
			if m.drag.panel != pickerNoPanel {
				t.Fatal("interaction did not end dragging")
			}
			before := m.offset
			pickerMouse(m, 32, 0, 100)
			if m.offset != before {
				t.Fatal("canceled drag resumed on motion")
			}
		})
	}
}

func TestPickerScrollbarRenderingAndSynchronization(t *testing.T) {
	for _, size := range [][2]int{{30, 10}, {40, 14}, {80, 24}, {160, 48}} {
		m := scrollbarPicker(100)
		m.resize(size[0], size[1])
		for _, panel := range []pickerPanel{pickerResultsPanel, pickerPreviewPanel, pickerHelpPanel} {
			m.help = panel == pickerHelpPanel
			m.setScroll(panel, 0)
			for _, end := range []bool{false, true} {
				if end {
					switch panel {
					case pickerResultsPanel:
						m.key(pickerEvent{key: "page-down"})
						m.move(1000)
					case pickerPreviewPanel:
						pickerMouse(m, 65, 10, m.previewTop())
						m.scrollPreview(1000)
					case pickerHelpPanel:
						for range 100 {
							m.key(pickerEvent{key: "page-down"})
						}
					}
				}
				b := m.scrollbar(panel)
				rows := pickerFrameRows(m.render(false))
				for i, row := range rows {
					plain := pickerANSI.ReplaceAllString(row, "")
					if glyphWidth(pickerGlyphs(plain, nil)) >= m.width {
						t.Fatalf("%v panel %d row %d wraps: %q", size, panel, i, plain)
					}
					if i >= b.top && i < b.top+b.rows {
						want := "│"
						if i-b.top >= b.thumbStart && i-b.top < b.thumbStart+b.thumbRows {
							want = "█"
						}
						if glyphWidth(pickerGlyphs(plain, nil)) != m.width-1 || !strings.HasSuffix(plain, " "+want) {
							t.Fatalf("%v panel %d row %d: misplaced scrollbar: %q", size, panel, i, plain)
						}
					}
				}
				if end && b.thumbStart+b.thumbRows != b.rows {
					t.Fatal("scrolling to the end did not move the thumb to the end")
				}
			}
		}
	}
	m := scrollbarPicker(2)
	m.resize(100, 300) // All three views fit.
	for _, help := range []bool{false, true} {
		m.help = help
		if strings.ContainsAny(m.render(false), "█│") {
			t.Fatal("fitting content displayed a scrollbar")
		}
	}
}

func TestPickerResizePreservesReadingPosition(t *testing.T) {
	all := []*Entry{{Raw: []string{strings.Repeat("你好 é 👩‍💻 long line ", 100), "", "", "tail"}}}
	m := newPickerModel(all, all, pickerOptions{})
	m.previewScroll = 8
	m.helpScroll = 14
	previewSource := m.previewLines()[m.previewScroll].source
	helpSource := m.helpLines()[m.helpScroll].source
	m.resize(40, 24)
	for _, tt := range []struct {
		name   string
		lines  []pickerLine
		row    int
		source int
	}{
		{"preview", m.previewLines(), m.previewScroll, previewSource},
		{"help", m.helpLines(), m.helpScroll, helpSource},
	} {
		if tt.lines[tt.row].source > tt.source || tt.row+1 < len(tt.lines) && tt.lines[tt.row+1].source <= tt.source {
			t.Fatalf("%s resize lost the text at the top of the viewport", tt.name)
		}
	}
	// Height-only resizing also preserves blank source lines instead of resetting
	// the preview to the first match or the beginning of the command.
	m.previewScroll = len(m.previewLines()) - 3
	m.resize(40, 12)
	if got := m.previewLines()[m.previewScroll]; len(got.glyphs) != 0 || got.source == 0 {
		t.Fatal("resize lost a blank source line")
	}
	m.resize(200, 200)
	if m.previewScroll > max(0, len(m.previewLines())-m.previewRows()) || m.helpScroll > max(0, len(m.helpLines())-m.height+3) {
		t.Fatal("resized scroll positions were not clamped")
	}
}

func TestPickerMouseDragDecoderChunkBoundaries(t *testing.T) {
	for _, tt := range []struct {
		sequence string
		button   int
		release  bool
	}{
		{"\x1b[<0;79;4M", 0, false},
		{"\x1b[<32;79;14M", 32, false},
		{"\x1b[<0;79;14m", 0, true},
	} {
		for split := 0; split <= len(tt.sequence); split++ {
			var decoder pickerDecoder
			events := decoder.feed([]byte(tt.sequence[:split]), false)
			events = append(events, decoder.feed([]byte(tt.sequence[split:]), false)...)
			if len(events) != 1 || events[0].key != "mouse" || events[0].mouseButton != tt.button || events[0].mouseRelease != tt.release || events[0].x != 78 {
				t.Fatalf("%q split %d: %+v", tt.sequence, split, events)
			}
		}
	}
}

func TestPickerFittingPanelsReclaimScrollbarSpace(t *testing.T) {
	// At full width this takes exactly three preview rows. Reserving the rail
	// prematurely would wrap it to four and create an unnecessary scrollbar.
	all := []*Entry{{Raw: []string{strings.Repeat("x", 84)}}}
	m := newPickerModel(all, all, pickerOptions{})
	m.resize(30, 12)
	if len(m.previewLines()) != 3 || m.scrollbar(pickerPreviewPanel).thumbRows != 0 || m.contentWidth(pickerPreviewPanel) != 29 {
		t.Fatal("fitting preview failed to reclaim the rail and margin")
	}
	rows := pickerFrameRows(m.render(false))
	if got := pickerANSI.ReplaceAllString(rows[m.previewTop()], ""); got != " "+strings.Repeat("x", 28) {
		t.Fatalf("fitting preview left unused content columns: %q", got)
	}
	if m.contentWidth(pickerResultsPanel) != 29 {
		t.Fatal("fitting result list retained a scrollbar gutter")
	}
	// Height alone can toggle overflow; cache invalidation must account for it.
	m.resize(30, 10)
	if len(m.previewLines()) != 4 || m.scrollbar(pickerPreviewPanel).thumbRows == 0 || m.contentWidth(pickerPreviewPanel) != 27 {
		t.Fatal("overflowing preview failed to reserve its rail and margin")
	}
	m.resize(30, 12)
	if len(m.previewLines()) != 3 || m.scrollbar(pickerPreviewPanel).thumbRows != 0 {
		t.Fatal("height increase left a stale preview scrollbar or narrow wrapping")
	}

	helpRows := len(wrapPickerText(strings.Join(pickerHelp, "\n"), nil, 29))
	m.help = true
	m.resize(30, helpRows+3)
	if len(m.helpLines()) != helpRows || m.contentWidth(pickerHelpPanel) != 29 || m.scrollbar(pickerHelpPanel).thumbRows != 0 {
		t.Fatal("fitting help retained a scrollbar gutter")
	}
	m.resize(30, helpRows+2)
	if m.contentWidth(pickerHelpPanel) != 27 || m.scrollbar(pickerHelpPanel).thumbRows == 0 {
		t.Fatal("overflowing help failed to reserve its rail and margin")
	}
}
