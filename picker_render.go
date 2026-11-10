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
	for at, r := range s {
		for matchAt < len(positions) && positions[matchAt] < runeAt {
			matchAt++
		}
		matched := matchAt < len(positions) && positions[matchAt] == runeAt
		runeAt++
		var text string
		if r == utf8.RuneError {
			text = "�"
		} else {
			text = s[at : at+utf8.RuneLen(r)]
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
			g.text += text
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
	width = max(1, width)
	var lines []pickerLine
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
		glyphs := pickerGlyphs(line, local)
		start, used := 0, 0
		matched := false
		// Expand tabs within the preview, keeping the source rune positions.
		for i := range glyphs {
			g := &glyphs[i]
			if g.text == "^I" {
				g.text, g.width = strings.Repeat(" ", 4-used%4), 4-used%4
			}
			if used+g.width > width && used > 0 {
				lines = append(lines, pickerLine{glyphs[start:i], matched})
				start, used, matched = i, 0, false
			}
			matched = matched || g.matched
			used += g.width
		}
		lines = append(lines, pickerLine{glyphs[start:], matched})
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
		lines[0] = ansiBold + "shlog · picker reference" + ansiReset
		lines[1] = strings.Repeat("─", width)
		help := m.helpLines()
		start := min(m.helpScroll, max(0, len(help)-height+3))
		for row := 2; row < height-1 && start+row-2 < len(help); row++ {
			lines[row] = help[start+row-2]
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
		path := filepath.Base(m.options.path)
		if width >= 65 {
			path += " · newest first"
		}
		lines[0] = pickerPair(title, path, width)
		count := fmt.Sprintf("%d / %d", len(m.matches), len(m.items))
		if m.searching {
			count = "searching"
		}
		if len(m.marked) > 0 || m.options.multi {
			count += fmt.Sprintf(" · %d selected", len(m.marked))
		}
		queryWidth := max(1, width-len(count)-5)
		if queryWidth < 8 {
			count = fmt.Sprintf("%d/%d", len(m.matches), len(m.items))
			if m.options.multi {
				count += fmt.Sprintf(" [%d]", len(m.marked))
			}
			queryWidth = max(1, width-len(count)-5)
		}
		// Bound work to the visible query window, even after a large paste.
		start := max(0, m.cursor-queryWidth)
		for start < m.cursor && glyphWidth(pickerGlyphs(string(m.input[start:m.cursor]), nil)) >= queryWidth {
			start++
		}
		end := min(len(m.input), m.cursor+queryWidth)
		query := renderPickerGlyphs(pickerGlyphs(string(m.input[start:end]), nil), queryWidth, false, "")
		cursorCol = 3 + glyphWidth(pickerGlyphs(string(m.input[start:m.cursor]), nil))
		lines[1] = "› " + query + strings.Repeat(" ", max(1, width-2-glyphWidth(pickerGlyphs(query, nil))-len(count))) + count
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
					marker = marker[:1] + "[ ] "
					if m.marked[itemIndex] {
						marker = marker[:1] + "[x] "
					}
				}
				meta := ""
				if width >= 40 {
					meta = "  " + strconv.Itoa(item.index)
				}
				if width >= 65 {
					date := "—"
					if item.entry.Timestamp != 0 {
						date = item.entry.Time().Format("01-02 15:04")
					}
					meta += "  " + date
				}
				positions := m.matchPositions(itemIndex)
				commandWidth := max(1, width-len(marker)-glyphWidth(pickerGlyphs(meta, nil)))
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
			m.previewScroll = min(m.previewScroll, max(0, len(preview)-m.previewRows()))
			heading := fmt.Sprintf("#%d · %s", item.index, date)
			if len(preview) > m.previewRows() {
				heading += fmt.Sprintf(" · %d–%d/%d", m.previewScroll+1, min(len(preview), m.previewScroll+m.previewRows()), len(preview))
			}
			lines[previewHeader] = fitPickerText(heading, width)
			for row := 0; row < m.previewRows() && m.previewScroll+row < len(preview); row++ {
				lines[m.previewTop()+row] = " " + renderPickerGlyphs(preview[m.previewScroll+row].glyphs, width-1, color, "")
			}
		}
		lines[height-2] = strings.Repeat("─", width)
		footer := "↑↓ move  enter pick  esc cancel  F1 help"
		if m.options.multi {
			footer = "↑↓ move  tab mark  enter accept  esc cancel  F1 help"
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
	"Results and output stay newest-first. Matching never executes a command.", "",
	"KEYBOARD", "↑↓ / ctrl-p/n   move between results", "page-up/down    move a page",
	"tab / shift-tab toggle selection and move (multi)", "enter           accept marks, otherwise focused entry",
	"esc / ctrl-c    cancel without selecting", "left/right      move query cursor", "home/end        query start/end (also ctrl-a/e)",
	"backspace/del   erase query characters", "ctrl-u          clear query", "ctrl-w          erase previous word",
	"alt-up/down     scroll preview", "ctrl-z          suspend; shell fg resumes", "F1              toggle this reference", "",
	"MOUSE", "wheel           scroll results; over preview, scroll preview",
	"click           focus a row", "click [ ]       toggle mark (multi)", "double-click    accept, like Enter", "",
	"Marks persist across queries, including hidden entries.",
	"Enter can accept hidden marks with no matching rows.",
	"Incomplete queries block acceptance until corrected.",
	"Pasted newlines/tabs become spaces; paste cannot accept a command.",
}

func pickerHelpLines(width int) []string {
	var lines []string
	for _, line := range pickerHelp {
		for _, wrapped := range wrapPickerText(line, nil, width) {
			lines = append(lines, renderPickerGlyphs(wrapped.glyphs, width, false, ""))
		}
	}
	return lines
}
