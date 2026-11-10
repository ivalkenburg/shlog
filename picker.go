package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var errPickerCanceled = errors.New("picker canceled")

type pickerSort string

const (
	pickerNewest    pickerSort = "newest"
	pickerRelevance pickerSort = "relevance"
	pickerFrequency pickerSort = "frequency"
)

func pickerSortArgument(args []string, at *int) (pickerSort, error) {
	_, value, inline := strings.Cut(args[*at], "=")
	if !inline {
		if *at+1 == len(args) {
			return "", fmt.Errorf("--sort requires newest, relevance, or frequency")
		}
		*at += 1
		value = args[*at]
	}
	switch order := pickerSort(value); order {
	case pickerNewest, pickerRelevance, pickerFrequency:
		return order, nil
	default:
		return "", fmt.Errorf("invalid sort %q: expected newest, relevance, or frequency", value)
	}
}

type pickerOptions struct {
	multi, deleting, unique bool
	path                    string
	sort                    pickerSort
}

type pickerItem struct {
	entry *Entry
	index int
	text  searchText
	count int
}

type pickerModel struct {
	options       pickerOptions
	items         []pickerItem
	matches       []int
	marked        map[int]bool
	focus, offset int
	input         []rune
	cursor        int
	query         searchQuery
	queryErr      error
	searching     bool
	acceptPending bool
	searchFocus   int
	previewScroll int
	help          bool
	helpScroll    int
	width, height int
	lastClick     time.Time
	lastClickItem int
	renderCache   pickerRenderCache
}

func pickEntries(all, pool []*Entry, options pickerOptions) ([]*Entry, error) {
	if len(pool) == 0 {
		return nil, nil
	}
	return runPicker(newPickerModel(all, pool, options))
}

func newPickerModel(all, pool []*Entry, options pickerOptions) *pickerModel {
	if options.sort == "" {
		options.sort = pickerNewest
	}
	m := &pickerModel{options: options, marked: make(map[int]bool), width: 80, height: 24, lastClickItem: -1, searchFocus: -1}
	m.items = make([]pickerItem, 0, min(len(pool), len(all)))
	m.matches = make([]int, 0, cap(m.items))
	allInPool := slices.Equal(all, pool)
	var inPool map[*Entry]bool
	if !allInPool {
		inPool = make(map[*Entry]bool, len(pool))
		for _, e := range pool {
			inPool[e] = true
		}
	}
	counts := make(map[string]int, len(pool))
	for i := len(all) - 1; i >= 0; i-- {
		if allInPool || inPool[all[i]] {
			text := CommandText(all[i])
			counts[text]++
			if options.unique && counts[text] > 1 {
				continue
			}
			m.items = append(m.items, pickerItem{entry: all[i], index: i + 1, text: newSearchText(text)})
			m.matches = append(m.matches, len(m.items)-1)
		}
	}
	for i := range m.items {
		m.items[i].count = counts[m.items[i].text.text]
	}
	if options.sort == pickerFrequency {
		m.matches, _ = filterPicker(context.Background(), m.items, searchQuery{}, options.sort)
	}
	return m
}

func (m *pickerModel) cycleSort() {
	switch m.options.sort {
	case pickerNewest:
		m.options.sort = pickerRelevance
	case pickerRelevance:
		m.options.sort = pickerFrequency
	default:
		m.options.sort = pickerNewest
	}
}

// The control is right-aligned in the header, leaving the wrap column unused.
func (m *pickerModel) sortControl() (string, int) {
	label := "[ newest first ]"
	switch m.options.sort {
	case pickerRelevance:
		label = "[ relevance ]"
	case pickerFrequency:
		label = "[ most used first ]"
	}
	return label, m.width - 1 - len(label)
}

func (m *pickerModel) clickedSort(e pickerEvent) bool {
	_, start := m.sortControl()
	return !m.help && e.mouseButton == 0 && e.y == 0 && e.x >= start && e.x < m.width-1
}

func (m *pickerModel) listRows() int    { return max(1, m.height-max(2, m.height/4)-7) }
func (m *pickerModel) previewRows() int { return max(2, m.height/4) }
func (m *pickerModel) previewTop() int  { return 5 + m.listRows() }

func (m *pickerModel) focused() int {
	if m.focus >= 0 && m.focus < len(m.matches) {
		return m.matches[m.focus]
	}
	return -1
}

func (m *pickerModel) ensureVisible() {
	m.focus = max(0, min(m.focus, len(m.matches)-1))
	if m.focus < m.offset {
		m.offset = m.focus
	}
	if m.focus >= m.offset+m.listRows() {
		m.offset = m.focus - m.listRows() + 1
	}
	m.offset = max(0, min(m.offset, max(0, len(m.matches)-m.listRows())))
}

func (m *pickerModel) move(delta int) {
	before := m.focused()
	m.focus += delta
	m.ensureVisible()
	if before != m.focused() {
		m.revealMatch()
	}
}

func (m *pickerModel) revealMatch() {
	m.previewScroll = 0
	if i := m.focused(); i >= 0 {
		positions := m.matchPositions(i)
		if len(positions) != 0 {
			lines := m.previewLines()
			for at, line := range lines {
				if line.matched {
					m.previewScroll = max(0, at-1)
					break
				}
			}
		}
	}
}

func (m *pickerModel) scrollPreview(delta int) {
	if i := m.focused(); i >= 0 {
		lines := m.previewLines()
		m.previewScroll = max(0, min(m.previewScroll+delta, max(0, len(lines)-m.previewRows())))
	}
}

func (m *pickerModel) setQuery(text string) {
	m.acceptPending = false
	m.query, m.queryErr = parseSearch(text)
	clear(m.renderCache.positions)
	m.renderCache.preview = nil
}

func filterPicker(ctx context.Context, items []pickerItem, q searchQuery, order pickerSort) ([]int, bool) {
	if ctx.Err() != nil {
		return nil, false
	}
	var matches []int
	type rank struct{ score, length int }
	var ranks []rank
	var work searchWorkspace
	rankMatches := order == pickerRelevance && q.hasPositiveTerms()
	for i := range items {
		if rankMatches || i%128 == 0 {
			select {
			case <-ctx.Done():
				return nil, false
			default:
			}
		}
		if q.matches(items[i].text) {
			if matches == nil {
				matches = make([]int, 0, len(items))
			}
			matches = append(matches, i)
			if rankMatches {
				if ranks == nil {
					ranks = make([]rank, len(items))
				}
				score, ok := q.score(ctx, items[i].text, &work)
				if !ok {
					return nil, false
				}
				ranks[i] = rank{score, utf8.RuneCountInString(items[i].text.text)}
			}
		}
	}
	if order == pickerFrequency || ranks != nil {
		slices.SortFunc(matches, func(a, b int) int {
			if order == pickerFrequency {
				if d := items[b].count - items[a].count; d != 0 {
					return d
				}
			} else {
				if d := ranks[b].score - ranks[a].score; d != 0 {
					return d
				}
				if d := ranks[a].length - ranks[b].length; d != 0 {
					return d
				}
			}
			return items[b].index - items[a].index
		})
	}
	return matches, ctx.Err() == nil
}

func (m *pickerModel) applyMatches(matches []int, previous int) {
	m.matches, m.focus, m.offset = matches, 0, 0
	if i := slices.Index(matches, previous); i >= 0 {
		m.focus = i
	}
	m.searching = false
	m.ensureVisible()
	m.revealMatch()
}

func (m *pickerModel) accept() ([]*Entry, bool) {
	if m.queryErr != nil {
		return nil, false
	}
	if m.searching {
		m.acceptPending = true
		return nil, false
	}
	m.acceptPending = false
	var result []*Entry
	if len(m.marked) != 0 {
		for i, item := range m.items {
			if m.marked[i] {
				result = append(result, item.entry)
			}
		}
		return result, true
	}
	if i := m.focused(); i >= 0 {
		return []*Entry{m.items[i].entry}, true
	}
	return nil, false
}

func (m *pickerModel) toggle() {
	if i := m.focused(); i >= 0 && m.options.multi {
		if m.marked[i] {
			delete(m.marked, i)
		} else {
			m.marked[i] = true
		}
	}
}

// key edits the model and reports whether the query changed. Acceptance and
// terminal lifecycle keys are handled by the event loop.
func (m *pickerModel) key(e pickerEvent) bool {
	if e.key != "enter" {
		m.acceptPending = false
	}
	if m.help {
		switch e.key {
		case "escape", "help":
			m.help = false
		case "up", "ctrl-p":
			m.helpScroll = max(0, m.helpScroll-1)
		case "down", "ctrl-n":
			m.helpScroll = min(max(0, len(m.helpLines())-m.height+3), m.helpScroll+1)
		case "page-up":
			m.helpScroll = max(0, m.helpScroll-m.height+3)
		case "page-down":
			m.helpScroll = min(max(0, len(m.helpLines())-m.height+3), m.helpScroll+m.height-3)
		}
		return false
	}
	switch e.key {
	case "ctrl-s":
		m.cycleSort()
		return true
	case "help":
		m.help, m.helpScroll = true, 0
	case "up", "ctrl-p":
		m.move(-1)
	case "down", "ctrl-n":
		m.move(1)
	case "page-up":
		m.move(-m.listRows())
	case "page-down":
		m.move(m.listRows())
	case "alt-up":
		m.scrollPreview(-1)
	case "alt-down":
		m.scrollPreview(1)
	case "tab", "back-tab":
		if m.options.multi {
			m.toggle()
			if e.key == "back-tab" {
				m.move(-1)
			} else {
				m.move(1)
			}
		}
	case "left":
		m.cursor = max(0, m.cursor-1)
	case "right":
		m.cursor = min(len(m.input), m.cursor+1)
	case "home", "ctrl-a":
		m.cursor = 0
	case "end", "ctrl-e":
		m.cursor = len(m.input)
	case "backspace":
		if m.cursor > 0 {
			m.input = slices.Delete(m.input, m.cursor-1, m.cursor)
			m.cursor--
			return true
		}
	case "delete":
		if m.cursor < len(m.input) {
			m.input = slices.Delete(m.input, m.cursor, m.cursor+1)
			return true
		}
	case "ctrl-u":
		m.input, m.cursor = nil, 0
		return true
	case "ctrl-w":
		start := m.cursor
		for start > 0 && unicode.IsSpace(m.input[start-1]) {
			start--
		}
		for start > 0 && !unicode.IsSpace(m.input[start-1]) {
			start--
		}
		m.input = slices.Delete(m.input, start, m.cursor)
		m.cursor = start
		return true
	case "text", "paste":
		text := strings.Map(func(r rune) rune {
			if r == '\n' || r == '\r' || r == '\t' {
				return ' '
			}
			if unicode.IsControl(r) || isBidiControl(r) {
				return -1
			}
			return r
		}, e.text)
		chars := []rune(text)
		m.input = slices.Insert(m.input, m.cursor, chars...)
		m.cursor += len(chars)
		return len(chars) != 0
	}
	return false
}

func (m *pickerModel) mouse(e pickerEvent, now time.Time) bool {
	m.acceptPending = false
	if e.mouseButton&64 != 0 {
		delta := 3
		if e.mouseButton&1 == 0 {
			delta = -delta
		}
		if m.help {
			m.helpScroll = max(0, min(m.helpScroll+delta, max(0, len(m.helpLines())-m.height+3)))
		} else if e.y >= m.previewTop() && e.y < m.previewTop()+m.previewRows() {
			m.scrollPreview(delta)
		} else {
			m.move(delta)
		}
		return false
	}
	if m.help || e.mouseButton != 0 || e.y < 3 || e.y >= 3+m.listRows() {
		return false
	}
	row := m.offset + e.y - 3
	if row >= len(m.matches) || m.searching || m.queryErr != nil {
		return false
	}
	m.focus = row
	m.revealMatch()
	if m.options.multi && e.x < 4 {
		m.toggle()
		m.lastClickItem = -1
		return false
	}
	double := m.lastClickItem == m.focused() && now.Sub(m.lastClick) < 400*time.Millisecond
	m.lastClick, m.lastClickItem = now, m.focused()
	return double
}
