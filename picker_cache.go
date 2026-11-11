package main

import "strings"

// Layout data is independent of focus, selection, and ANSI styling. Keep only
// a small viewport's alignments and the currently focused command's wrapping.
type pickerRenderCache struct {
	positions    map[int][]int
	preview      []pickerLine
	previewItem  int
	previewWidth int
	previewRows  int
	help         []pickerLine
	helpWidth    int
	helpRows     int
}

func (m *pickerModel) matchPositions(item int) []int {
	limit := 2 * m.listRows()
	if len(m.renderCache.positions) > limit {
		clear(m.renderCache.positions)
	}
	if positions, ok := m.renderCache.positions[item]; ok {
		return positions
	}
	if m.renderCache.positions == nil {
		m.renderCache.positions = make(map[int][]int)
	}
	if len(m.renderCache.positions) >= limit {
		clear(m.renderCache.positions)
	}
	positions := m.query.positions(m.items[item].text)
	m.renderCache.positions[item] = positions
	return positions
}

func (m *pickerModel) previewLines() []pickerLine {
	item := m.focused()
	if item < 0 {
		return nil
	}
	width, rows := max(1, m.width-2), m.previewRows()
	cache := &m.renderCache
	if cache.preview == nil || cache.previewItem != item || cache.previewWidth != width || cache.previewRows != rows {
		// Test full-width wrapping first; reserving a rail before testing
		// overflow could create a scrollbar for otherwise fitting content.
		cache.preview = wrapPickerTextRows(m.items[item].text.text, m.matchPositions(item), width, rows+1)
		if len(cache.preview) > rows {
			cache.preview = wrapPickerText(m.items[item].text.text, m.matchPositions(item), max(1, width-2))
		}
		cache.previewItem, cache.previewWidth = item, width
		cache.previewRows = rows
	}
	return cache.preview
}

func (m *pickerModel) helpLines() []pickerLine {
	width, rows := max(1, m.width-1), m.height-3
	if m.renderCache.help == nil || m.renderCache.helpWidth != width || m.renderCache.helpRows != rows {
		text := strings.Join(pickerHelp, "\n")
		m.renderCache.help = wrapPickerTextRows(text, nil, width, rows+1)
		if len(m.renderCache.help) > rows {
			m.renderCache.help = wrapPickerText(text, nil, max(1, width-2))
		}
		m.renderCache.helpWidth = width
		m.renderCache.helpRows = rows
	}
	return m.renderCache.help
}
