package main

// Layout data is independent of focus, selection, and ANSI styling. Keep only
// a small viewport's alignments and the currently focused command's wrapping.
type pickerRenderCache struct {
	positions    map[int][]int
	preview      []pickerLine
	previewItem  int
	previewWidth int
	help         []string
	helpWidth    int
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
	width := max(1, m.width-2)
	cache := &m.renderCache
	if cache.preview == nil || cache.previewItem != item || cache.previewWidth != width {
		cache.preview = wrapPickerText(m.items[item].text.text, m.matchPositions(item), width)
		cache.previewItem, cache.previewWidth = item, width
	}
	return cache.preview
}

func (m *pickerModel) helpLines() []string {
	width := max(1, m.width-1)
	if m.renderCache.help == nil || m.renderCache.helpWidth != width {
		m.renderCache.help = pickerHelpLines(width)
		m.renderCache.helpWidth = width
	}
	return m.renderCache.help
}
