package main

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type pickerGlyph struct {
	text    string
	width   int
	matched bool
}

type pickerLine struct {
	glyphs  []pickerGlyph
	matched bool
	source  int // Cluster offset in the original text, counting newlines.
}

func isBidiControl(r rune) bool {
	return r == 0x061c || r == 0x200e || r == 0x200f || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069)
}

// Terminal control bytes from history are rendered as text, never interpreted.
// Combining marks and common emoji sequences stay attached when clipping.
func pickerGlyphs(s string, positions []int) []pickerGlyph {
	glyphs, _ := pickerGlyphsWithin(s, positions, -1)
	return glyphs
}

// A negative limit prepares the full text. Otherwise finish the last visible
// cluster before clipping, so combining marks and joined emoji stay intact.
func pickerGlyphsWithin(s string, positions []int, limit int) ([]pickerGlyph, bool) {
	capacity := 0
	if limit < 0 {
		capacity = utf8.RuneCountInString(s)
	} else {
		capacity = min(len(s), limit+1)
	}
	result := make([]pickerGlyph, 0, capacity)
	matchAt, runeAt := 0, 0
	cells := 0
	join := false
	regional := false
	pictographic, modifierBase, keycapBase := false, false, false
	clusterStart := 0
	var synthetic strings.Builder
	for at, r := range s {
		for matchAt < len(positions) && positions[matchAt] < runeAt {
			matchAt++
		}
		matched := matchAt < len(positions) && positions[matchAt] == runeAt
		runeAt++
		end := at + utf8.RuneLen(r)
		var text string
		if r == utf8.RuneError {
			_, size := utf8.DecodeRuneInString(s[at:])
			end = at + size
			text = "�"
		} else {
			text = s[at:end]
		}
		width := pickerRuneWidth(r)
		switch {
		case r < 0x20:
			text, width = "^"+string(r+'@'), 2
		case r == 0x7f:
			text, width = "^?", 2
		case unicode.IsControl(r) || isBidiControl(r):
			text = fmt.Sprintf("<%04X>", r)
			width = len(text)
		}
		isRegional := r >= 0x1f1e6 && r <= 0x1f1ff
		isPictographic := runeInRanges(r, pictographicRunes)
		modifier := r >= 0x1f3fb && r <= 0x1f3ff && modifierBase
		joined := join && isPictographic
		paired := regional && isRegional
		attached := len(result) > 0 && (width == 0 || modifier || joined || paired)
		if attached {
			g := &result[len(result)-1]
			oldWidth := g.width
			if clusterStart >= 0 {
				// A cluster can contain arbitrarily many combining marks or
				// joined emoji. Borrow its source instead of copying each prefix.
				g.text = s[clusterStart:end]
			} else {
				if synthetic.Len() == 0 {
					synthetic.WriteString(g.text)
				}
				synthetic.WriteString(text)
				g.text = synthetic.String()
			}
			g.matched = g.matched || matched
			if modifier || joined || paired || r == 0xfe0f && (pictographic || keycapBase) || r == 0x20e3 && keycapBase {
				g.width = max(g.width, 2)
			}
			cells += g.width - oldWidth
			if limit >= 0 && cells > limit {
				return result[:len(result)-1], false
			}
		} else {
			if width == 0 {
				text, width = "◌"+text, 1
			}
			if limit >= 0 && cells+width > limit {
				return result, false
			}
			clusterStart = at
			synthetic.Reset()
			if text != s[at:end] {
				// Escaped controls, invalid UTF-8, and leading combining marks
				// need generated text; grow that cluster with one builder.
				clusterStart = -1
			}
			result = append(result, pickerGlyph{text, width, matched})
			cells += width
		}
		if !attached || joined {
			pictographic = isPictographic
			modifierBase = runeInRanges(r, modifierBaseRunes)
			keycapBase = r == '#' || r == '*' || r >= '0' && r <= '9'
		} else if r != 0xfe0f {
			modifierBase, keycapBase = false, false
		}
		join = r == 0x200d && pictographic
		regional = isRegional && !regional
	}
	return result, true
}

func pickerRuneWidth(r rune) int {
	if r < utf8.RuneSelf {
		return 1
	}
	if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || r == 0x200d {
		return 0
	}
	if runeInRanges(r, wideRunes) {
		return 2
	}
	return 1
}

func runeInRanges(r rune, ranges [][2]rune) bool {
	if len(ranges) == 0 || r < ranges[0][0] || r > ranges[len(ranges)-1][1] {
		return false
	}
	for lo, hi := 0, len(ranges); lo < hi; {
		mid := lo + (hi-lo)/2
		if r < ranges[mid][0] {
			hi = mid
		} else if r > ranges[mid][1] {
			lo = mid + 1
		} else {
			return true
		}
	}
	return false
}

func glyphWidth(glyphs []pickerGlyph) int {
	width := 0
	for _, g := range glyphs {
		width += g.width
	}
	return width
}

func fitPickerText(s string, width int) string {
	if width <= 0 {
		return ""
	}
	glyphs, complete := pickerGlyphsWithin(s, nil, width)
	if complete {
		return renderPickerGlyphs(glyphs, width, false, "")
	}
	return renderPickerGlyphs(glyphs, width-1, false, "") + "…"
}

func renderPickerGlyphs(glyphs []pickerGlyph, width int, color bool, base string) string {
	var b strings.Builder
	used, highlighted := 0, false
	for _, g := range glyphs {
		if used+g.width > width {
			break
		}
		if g.matched != highlighted {
			b.WriteString(ansiReset + base)
			if g.matched {
				b.WriteString("\x1b[1;4m")
				if color && !strings.Contains(base, "7m") {
					b.WriteString("\x1b[36m")
				}
			}
			highlighted = g.matched
		}
		b.WriteString(g.text)
		used += g.width
	}
	if highlighted {
		b.WriteString(ansiReset + base)
	}
	return b.String()
}

func wrapPickerText(s string, positions []int, width int) []pickerLine {
	return wrapPickerTextRows(s, positions, width, 0)
}

// A row limit bounds overflow probes; zero prepares the full preview.
func wrapPickerTextRows(s string, positions []int, width, rowLimit int) []pickerLine {
	width = max(1, width)
	var lines []pickerLine
	source := 0
	runeOffset, position := 0, 0
	for line := range strings.SplitSeq(s, "\n") {
		var local []int
		length := utf8.RuneCountInString(line)
		for position < len(positions) && positions[position] < runeOffset {
			position++
		}
		for position < len(positions) && positions[position] < runeOffset+length {
			local = append(local, positions[position]-runeOffset)
			position++
		}
		runeOffset += length + 1
		cellLimit := -1
		if rowLimit > 0 && width >= 8 {
			// Every glyph fits in a row at these widths. A raw ^I uses two
			// cells but its tab can use only one, so allow twice the visible
			// cells to keep all glyphs needed by the overflow probe.
			cellLimit = 2 * (rowLimit - len(lines)) * width
		}
		glyphs, _ := pickerGlyphsWithin(line, local, cellLimit)
		start, used := 0, 0
		matched := false
		// Expand tabs within the preview, keeping the source rune positions.
		for i := range glyphs {
			g := &glyphs[i]
			tab := g.text == "^I"
			if tab {
				g.width = 4 - used%4
			}
			if used+g.width > width && used > 0 {
				lines = append(lines, pickerLine{glyphs[start:i], matched, source + start})
				if rowLimit > 0 && len(lines) >= rowLimit {
					return lines
				}
				start, used, matched = i, 0, false
				if tab {
					g.width = 4
				}
			}
			if tab {
				g.text = strings.Repeat(" ", g.width)
			}
			matched = matched || g.matched
			used += g.width
		}
		lines = append(lines, pickerLine{glyphs[start:], matched, source + start})
		if rowLimit > 0 && len(lines) >= rowLimit {
			return lines
		}
		source += len(glyphs) + 1
	}
	return lines
}

func pickerPair(left, right string, width int) string {
	left = fitPickerText(left, width)
	leftWidth := glyphWidth(pickerGlyphs(left, nil))
	if width-leftWidth < 6 {
		return fitPickerText(left, width)
	}
	right = fitPickerText(right, width-leftWidth-2)
	rightWidth := glyphWidth(pickerGlyphs(right, nil))
	return ansiBold + left + ansiReset + strings.Repeat(" ", width-leftWidth-rightWidth) + right
}

func (m *pickerModel) render(color bool) string {
	width, height := max(1, m.width-1), m.height // Leave the terminal's wrap column unused.
	lines := make([]string, height)
	cursorRow, cursorCol := 2, 3
	if m.help {
		contentWidth := m.contentWidth(pickerHelpPanel)
		lines[0] = ansiBold + "shlog · picker reference" + ansiReset
		lines[1] = strings.Repeat("─", width)
		help := m.helpLines()
		m.helpScroll = max(0, min(m.helpScroll, max(0, len(help)-height+3)))
		start := m.helpScroll
		bar := m.scrollbar(pickerHelpPanel)
		for row := 2; row < height-1 && start+row-2 < len(help); row++ {
			line := help[start+row-2]
			lines[row] = renderPickerGlyphs(line.glyphs, contentWidth, false, "")
			if bar.thumbRows > 0 {
				lines[row] += strings.Repeat(" ", max(0, contentWidth-glyphWidth(line.glyphs))) + " " + bar.glyph(row-2)
			}
		}
		lines[height-1] = fitPickerText("↑↓ / wheel scroll   esc / F1 return", width)
	} else {
		title := "shlog pick"
		if m.options.deleting {
			title = "shlog del --pick"
		}
		if m.options.multi {
			title += " · multi"
		}
		if m.options.unique {
			title += " · unique"
		}
		badge, badgeStart := m.sortControl()
		if width >= 65 {
			title += " · " + filepath.Base(m.options.path)
		}
		left := fitPickerText(title, max(1, badgeStart-2))
		lines[0] = pickerPair(left, badge, width)

		total := len(m.items)
		if m.options.sort == pickerFrequency {
			total = m.commands
		}
		count := fmt.Sprintf("%d / %d", len(m.matches), total)
		if m.searching {
			count = "searching"
		}
		selectedCommands := 0
		if len(m.marked) > 0 || m.options.multi {
			if m.options.sort == pickerFrequency {
				selectedCommands = m.selectedCommands()
				commandLabel, entryLabel := "commands", "entries"
				if selectedCommands == 1 {
					commandLabel = "command"
				}
				if len(m.marked) == 1 {
					entryLabel = "entry"
				}
				count += fmt.Sprintf(" · %d %s · %d %s selected", selectedCommands, commandLabel, len(m.marked), entryLabel)
			} else {
				count += fmt.Sprintf(" · %d selected", len(m.marked))
			}
		}
		countWidth := glyphWidth(pickerGlyphs(count, nil))
		if width-countWidth-5 < 8 {
			count = fmt.Sprintf("%d/%d", len(m.matches), total)
			if m.options.multi {
				if m.options.sort == pickerFrequency {
					count += fmt.Sprintf(" [%dc/%de]", selectedCommands, len(m.marked))
				} else {
					count += fmt.Sprintf(" [%d]", len(m.marked))
				}
			}
			countWidth = glyphWidth(pickerGlyphs(count, nil))
		}
		if countWidth > width-5 {
			// Large histories can exhaust even the abbreviated counter. Keep
			// selection counts visible and leave room to edit the query.
			if m.options.multi {
				if m.options.sort == pickerFrequency {
					count = fmt.Sprintf("%dc/%de", selectedCommands, len(m.marked))
				} else {
					count = fmt.Sprintf("%d selected", len(m.marked))
				}
			} else {
				count = fmt.Sprint(len(m.matches))
			}
			countWidth = glyphWidth(pickerGlyphs(count, nil))
		}
		queryWidth := max(1, width-countWidth-5)
		// Bound work to the visible query window, even after a large paste.
		start := max(0, m.cursor-queryWidth)
		for start < m.cursor && glyphWidth(pickerGlyphs(string(m.input[start:m.cursor]), nil)) >= queryWidth {
			start++
		}
		end := min(len(m.input), m.cursor+queryWidth)
		query := renderPickerGlyphs(pickerGlyphs(string(m.input[start:end]), nil), queryWidth, false, "")
		cursorCol = 3 + glyphWidth(pickerGlyphs(string(m.input[start:m.cursor]), nil))
		lines[1] = "› " + query + strings.Repeat(" ", max(1, width-2-glyphWidth(pickerGlyphs(query, nil))-countWidth)) + count
		lines[2] = strings.Repeat("─", width)
		if m.queryErr != nil {
			lines[3] = fitPickerText("Query: "+m.queryErr.Error(), width)
		} else if m.searching {
			lines[3] = "Searching…"
		} else if len(m.matches) == 0 {
			lines[3] = "No matching commands"
			if len(m.marked) > 0 && m.listRows() > 1 {
				lines[4] = fitPickerText("Enter accepts the marked entries", width)
			}
		} else {
			bar := m.scrollbar(pickerResultsPanel)
			contentWidth := m.contentWidth(pickerResultsPanel)
			for row := 0; row < m.listRows() && m.offset+row < len(m.matches); row++ {
				at := m.offset + row
				itemIndex := m.matches[at]
				item := m.items[itemIndex]
				base := ""
				marker := "  "
				if at == m.focus {
					base, marker = "\x1b[7m", "> "
				}
				if m.options.multi {
					marker = marker[:1] + "[" + string(m.rowMark(itemIndex)) + "] "
				}
				meta := ""
				if m.options.unique || m.options.sort == pickerFrequency {
					meta = fmt.Sprintf("  ×%d", item.count)
				}
				if width >= 40 {
					meta += "  " + strconv.Itoa(item.index)
				}
				if width >= 65 {
					date := "—"
					if item.entry.Timestamp != 0 {
						date = item.entry.Time().Format("01-02 15:04")
					}
					meta += "  " + date
				}
				positions := m.matchPositions(itemIndex)
				commandWidth := max(1, contentWidth-len(marker)-glyphWidth(pickerGlyphs(meta, nil)))
				firstLine, _, multiline := strings.Cut(item.text.text, "\n")
				glyphs, complete := pickerGlyphsWithin(firstLine, positions, commandWidth)
				if complete && multiline {
					glyphs = append(glyphs, pickerGlyph{text: " ", width: 1}, pickerGlyph{text: "↵", width: 1})
				}
				text := renderPickerGlyphs(glyphs, commandWidth, color, base)
				used := 0
				for _, g := range glyphs {
					if used+g.width > commandWidth {
						break
					}
					used += g.width
				}
				// Pad within the focused reverse-video band, including metadata.
				lines[3+row] = base + marker + text + strings.Repeat(" ", max(0, commandWidth-used)) + meta + ansiReset
				if bar.thumbRows > 0 {
					lines[3+row] += " " + bar.glyph(row)
				}
			}
		}
		previewDivider := 3 + m.listRows()
		lines[previewDivider] = strings.Repeat("─", width)
		previewHeader := previewDivider + 1
		if i := m.focused(); i >= 0 && !m.searching && m.queryErr == nil {
			item := m.items[i]
			date := "no timestamp"
			if item.entry.Timestamp != 0 {
				date = item.entry.Time().Format("2006-01-02 15:04:05")
			}
			preview := m.previewLines()
			m.previewScroll = max(0, min(m.previewScroll, max(0, len(preview)-m.previewRows())))
			bar := m.scrollbar(pickerPreviewPanel)
			contentWidth := m.contentWidth(pickerPreviewPanel)
			heading := fmt.Sprintf("#%d · %s", item.index, date)
			if m.options.unique || m.options.sort == pickerFrequency {
				heading += fmt.Sprintf(" · %d uses", item.count)
			}
			if len(preview) > m.previewRows() {
				heading += fmt.Sprintf(" · %d–%d/%d", m.previewScroll+1, min(len(preview), m.previewScroll+m.previewRows()), len(preview))
			}
			lines[previewHeader] = fitPickerText(heading, width)
			for row := 0; row < m.previewRows() && m.previewScroll+row < len(preview); row++ {
				line := preview[m.previewScroll+row]
				lines[m.previewTop()+row] = " " + renderPickerGlyphs(line.glyphs, contentWidth-1, color, "")
				if bar.thumbRows > 0 {
					lines[m.previewTop()+row] += strings.Repeat(" ", max(0, contentWidth-1-glyphWidth(line.glyphs))) + " " + bar.glyph(row)
				}
			}
		}
		lines[height-2] = strings.Repeat("─", width)
		footer := "↑↓ move  enter pick  ctrl-s sort  esc cancel  F1 help"
		if m.options.multi {
			footer = "↑↓ move  tab mark  enter accept  ctrl-s sort  esc cancel  F1 help"
		}
		if width < len([]rune(footer)) {
			footer = "↑↓ move  enter  esc cancel  F1 help"
			if m.options.multi {
				footer = "↑↓ move  tab mark  enter  esc  F1 help"
			}
		}
		if width < len([]rune(footer)) {
			footer = "↑↓  enter pick  esc  F1 help"
			if m.options.multi {
				footer = "tab mark  enter  esc  F1 help"
			}
		}
		lines[height-1] = fitPickerText(footer, width)
	}
	var frame strings.Builder
	frame.WriteString("\x1b[?25l")
	for row, line := range lines {
		fmt.Fprintf(&frame, "\x1b[%d;1H\x1b[2K%s%s", row+1, line, ansiReset)
	}
	if !m.help {
		fmt.Fprintf(&frame, "\x1b[%d;%dH\x1b[?25h", cursorRow, min(width, cursorCol))
	}
	return frame.String()
}

var pickerHelp = []string{
	"SEARCH", "Terms separated by spaces are ANDed.",
	"gco             fuzzy: git checkout", "git main | dev  git AND (main OR dev)",
	"'checkout       exact substring", "'git'           exact word", "^git  main$     command prefix / suffix",
	"^pwd$           entire command", "!sudo           exclude exact substring", "!^sudo  !log$   exclude prefix / suffix",
	"!'abc           exclude fuzzy match", "\\!  \\|  \\  \\\\   literal operators, space, backslash", "",
	"Lowercase ignores case; uppercase makes that term case-sensitive.",
	"Unaccented Latin terms match accents; accented terms stay literal.",
	"Search covers full multiline commands. Anchors cover the whole command.",
	"Results default to newest-first; click the sort control or press ctrl-s.",
	"Sort cycles: newest first, relevance, most used first.",
	"Equal relevance uses shorter commands, then newer entries.",
	"Frequency counts uses within the current history selection.",
	"Most used first shows one row per exact command, using its latest entry.",
	"With no marks, deleting a grouped row removes all its scoped occurrences.",
	"Other sorts show individual entries unless --unique is set.",
	"--unique groups commands; the row represents their latest occurrence.",
	"Multi-select output stays newest-first regardless of display order.",
	"In most used first, pick output prints each selected command once.",
	"Group marks: [x] all selected; [-] some selected. Toggle selects all or clears all.",
	"Marks stay attached to individual entries across sort changes.",
	"Matching never executes a command.", "",
	"KEYBOARD", "↑↓ / ctrl-p/n   move between results", "page-up/down    move a page",
	"tab / shift-tab toggle selection and move (multi)", "enter           accept marks, otherwise focused entry",
	"esc / ctrl-c    cancel without selecting", "left/right      move query cursor", "home/end        query start/end (also ctrl-a/e)",
	"backspace/del   erase query characters", "ctrl-u          clear query", "ctrl-w          erase previous word",
	"ctrl-s          cycle result sorting", "alt-up/down     scroll preview", "ctrl-z          suspend a separate foreground job; shell fg resumes", "F1              toggle this reference", "",
	"MOUSE", "wheel           scroll results; over preview, scroll preview",
	"drag scrollbar  scroll results, preview, or help", "click rail      jump to that position",
	"click           focus a row", "click [ ]       toggle mark (multi)", "double-click    accept, like Enter", "",
	"Marks persist across queries, including hidden entries.",
	"Enter can accept hidden marks with no matching rows.",
	"Incomplete queries block acceptance until corrected.",
	"Pasted newlines/tabs become spaces; paste cannot accept a command.",
}
