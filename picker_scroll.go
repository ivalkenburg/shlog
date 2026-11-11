package main

type pickerPanel uint8

const (
	pickerNoPanel pickerPanel = iota
	pickerResultsPanel
	pickerPreviewPanel
	pickerHelpPanel
)

type pickerDrag struct {
	panel pickerPanel
	grab  int // Pointer's row within the thumb.
	y     int
}

type pickerScrollbar struct {
	panel                    pickerPanel
	top, rows, total, offset int
	thumbStart, thumbRows    int
}

func newPickerScrollbar(panel pickerPanel, top, rows, total, offset int) pickerScrollbar {
	b := pickerScrollbar{panel: panel, top: top, rows: rows, total: total}
	b.offset = max(0, min(offset, b.maxOffset()))
	if total > rows {
		b.thumbRows = max(1, min(rows-1, rows*rows/total))
		b.thumbStart = (b.offset*b.travel() + b.maxOffset()/2) / b.maxOffset()
	}
	return b
}

func (b pickerScrollbar) maxOffset() int { return max(0, b.total-b.rows) }
func (b pickerScrollbar) travel() int    { return b.rows - b.thumbRows }

func (b pickerScrollbar) glyph(row int) string {
	if row >= b.thumbStart && row < b.thumbStart+b.thumbRows {
		return "█"
	}
	return "\x1b[2m│" + ansiReset
}

// Only overflowing panels reserve the rail and its one-column content margin.
// The terminal's wrap column always stays unused.
func (m *pickerModel) contentWidth(panel pickerPanel) int {
	width := max(1, m.width-1)
	if m.scrollbar(panel).thumbRows > 0 {
		width = max(1, width-2)
	}
	return width
}

func (m *pickerModel) scrollbar(panel pickerPanel) pickerScrollbar {
	switch panel {
	case pickerResultsPanel:
		if !m.searching && m.queryErr == nil {
			return newPickerScrollbar(panel, 3, m.listRows(), len(m.matches), m.offset)
		}
	case pickerPreviewPanel:
		if !m.searching && m.queryErr == nil {
			return newPickerScrollbar(panel, m.previewTop(), m.previewRows(), len(m.previewLines()), m.previewScroll)
		}
	case pickerHelpPanel:
		return newPickerScrollbar(panel, 2, m.height-3, len(m.helpLines()), m.helpScroll)
	}
	return pickerScrollbar{}
}

func (m *pickerModel) setScroll(panel pickerPanel, offset int) {
	b := m.scrollbar(panel)
	offset = max(0, min(offset, b.maxOffset()))
	switch panel {
	case pickerResultsPanel:
		before := m.focused()
		m.offset = offset
		m.focus = max(offset, min(m.focus, offset+m.listRows()-1))
		if before != m.focused() {
			m.revealMatch()
		}
	case pickerPreviewPanel:
		m.previewScroll = offset
	case pickerHelpPanel:
		m.helpScroll = offset
	}
}

func (m *pickerModel) dragTo(y int) {
	b := m.scrollbar(m.drag.panel)
	if b.thumbRows == 0 {
		m.drag = pickerDrag{}
		return
	}
	// A one-row result viewport has no travel within its rail. Dragging past
	// that cell still reaches either end of the content.
	travel := max(1, b.travel())
	start := max(0, min(y-b.top-m.drag.grab, travel))
	m.setScroll(b.panel, (start*b.maxOffset()+travel/2)/travel)
}

// Visible rails take precedence over row selection. Fitting panels use their
// full content width and have no invisible scrollbar hit target.
func (m *pickerModel) scrollbarMouse(e pickerEvent) bool {
	if e.mouseRelease {
		m.drag = pickerDrag{}
		return true
	}
	if e.mouseButton&32 != 0 {
		m.lastClickItem = -1
		if e.mouseButton&3 == 0 && m.drag.panel != pickerNoPanel && e.y != m.drag.y {
			m.drag.y = e.y
			m.dragTo(e.y)
		}
		return true
	}
	m.drag = pickerDrag{}
	if e.mouseButton != 0 || e.x != m.width-2 {
		return false
	}
	panels := []pickerPanel{pickerResultsPanel, pickerPreviewPanel}
	if m.help {
		panels = []pickerPanel{pickerHelpPanel}
	}
	for _, panel := range panels {
		b := m.scrollbar(panel)
		if b.thumbRows == 0 || e.y < b.top || e.y >= b.top+b.rows {
			continue
		}
		m.lastClickItem = -1
		grab := e.y - b.top - b.thumbStart
		if grab < 0 || grab >= b.thumbRows {
			grab = b.thumbRows / 2
		}
		m.drag = pickerDrag{panel: panel, grab: grab, y: e.y}
		// Grabbing the thumb must not snap a finely scrolled viewport to
		// the coarser position represented by a terminal cell.
		if e.y < b.top+b.thumbStart || e.y >= b.top+b.thumbStart+b.thumbRows {
			m.dragTo(e.y)
		}
		return true
	}
	return false
}

// Source offsets survive wrapping changes, including blank lines and Unicode
// clusters. Keep the old top line's source position visible after a resize.
func pickerReadingPosition(lines []pickerLine, row int) int {
	if len(lines) == 0 {
		return 0
	}
	return lines[max(0, min(row, len(lines)-1))].source
}

func pickerReadingRow(lines []pickerLine, source int) int {
	row := 0
	for row+1 < len(lines) && lines[row+1].source <= source {
		row++
	}
	return row
}

func (m *pickerModel) resize(width, height int) {
	previewSource := pickerReadingPosition(m.previewLines(), m.previewScroll)
	helpSource := pickerReadingPosition(m.helpLines(), m.helpScroll)
	m.drag = pickerDrag{}
	m.width, m.height = width, height
	m.lastClickItem = -1
	m.ensureVisible()
	m.setScroll(pickerPreviewPanel, pickerReadingRow(m.previewLines(), previewSource))
	m.setScroll(pickerHelpPanel, pickerReadingRow(m.helpLines(), helpSource))
}
