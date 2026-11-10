package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// searchText caches the four views used by smart case and Latin accent folding.
// Unchanged views share their strings, so ordinary ASCII commands are cheap.
type searchText struct {
	text, lower, plain, plainLower string
}

func newSearchText(s string) searchText {
	lower := strings.ToLower(s)
	p := foldLatin(s)
	plainLower := lower
	if p != s {
		plainLower = strings.ToLower(p)
	}
	return searchText{s, lower, p, plainLower}
}

type searchChar struct {
	r       rune
	escaped bool
}

type searchTerm struct {
	text                         string
	negative, fuzzy, prefix, end bool
	word, sensitive, normalize   bool
}

// Each group is an OR expression; all groups must match. Thus a b | c means
// a AND (b OR c), following extended fuzzy-search syntax.
type searchQuery struct{ groups [][]searchTerm }

func parseSearch(query string) (searchQuery, error) {
	var tokens [][]searchChar
	var token []searchChar
	runes := []rune(query)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == '\\' {
			if i+1 == len(runes) {
				return searchQuery{}, fmt.Errorf("finish the escape with a character")
			}
			next := runes[i+1]
			if strings.ContainsRune("\\!^$|'", next) || unicode.IsSpace(next) {
				token = append(token, searchChar{next, true})
				i++
				continue
			}
		}
		if unicode.IsSpace(r) {
			if len(token) != 0 {
				tokens = append(tokens, token)
				token = nil
			}
		} else {
			token = append(token, searchChar{r, false})
		}
	}
	if len(token) != 0 {
		tokens = append(tokens, token)
	}
	q := searchQuery{}
	or := false
	for _, chars := range tokens {
		if len(chars) == 1 && chars[0].r == '|' && !chars[0].escaped {
			if len(q.groups) == 0 || or {
				return q, fmt.Errorf("add a search term before |")
			}
			or = true
			continue
		}
		t, err := parseSearchTerm(chars)
		if err != nil {
			return q, err
		}
		if or {
			q.groups[len(q.groups)-1] = append(q.groups[len(q.groups)-1], t)
		} else {
			q.groups = append(q.groups, []searchTerm{t})
		}
		or = false
	}
	if or {
		return q, fmt.Errorf("add a search term after |")
	}
	return q, nil
}

func parseSearchTerm(chars []searchChar) (searchTerm, error) {
	t := searchTerm{fuzzy: true, normalize: true}
	starts := func(r rune) bool { return len(chars) > 0 && chars[0].r == r && !chars[0].escaped }
	ends := func(r rune) bool {
		return len(chars) > 0 && chars[len(chars)-1].r == r && !chars[len(chars)-1].escaped
	}
	if starts('!') {
		t.negative, t.fuzzy = true, false
		chars = chars[1:]
	}
	if starts('\'') {
		t.fuzzy = t.negative // !'word is an inverse fuzzy term.
		chars = chars[1:]
		if !t.negative && ends('\'') {
			t.word = true
			chars = chars[:len(chars)-1]
		}
	}
	if starts('^') {
		t.prefix, t.fuzzy = true, false
		chars = chars[1:]
	}
	if ends('$') {
		t.end, t.fuzzy = true, false
		chars = chars[:len(chars)-1]
	}
	if len(chars) == 0 && !(t.prefix && t.end) {
		return t, fmt.Errorf("add text after the search operator")
	}
	var b strings.Builder
	for _, c := range chars {
		b.WriteRune(c.r)
		t.sensitive = t.sensitive || unicode.IsUpper(c.r)
		if latinBase(c.r) != c.r || unicode.Is(unicode.Mn, c.r) {
			t.normalize = false
		}
	}
	t.text = b.String()
	if !t.sensitive {
		t.text = strings.ToLower(t.text)
	}
	return t, nil
}

func (t searchTerm) view(s searchText) string {
	if t.normalize {
		if t.sensitive {
			return s.plain
		}
		return s.plainLower
	}
	if t.sensitive {
		return s.text
	}
	return s.lower
}

func (t searchTerm) matches(s searchText) bool {
	hay := t.view(s)
	matched := false
	switch {
	case t.prefix && t.end:
		matched = hay == t.text
	case t.prefix:
		matched = strings.HasPrefix(hay, t.text)
	case t.end:
		matched = strings.HasSuffix(hay, t.text)
	case t.word:
		matched = wordIndex(hay, t.text) >= 0
	case !t.fuzzy:
		matched = strings.Contains(hay, t.text)
	default:
		matched = true
		for _, r := range t.text {
			i := strings.IndexRune(hay, r)
			if i < 0 {
				matched = false
				break
			}
			width := utf8.RuneLen(r)
			if r == utf8.RuneError {
				_, width = utf8.DecodeRuneInString(hay[i:])
			}
			hay = hay[i+width:]
		}
	}
	return matched != t.negative
}

func (q searchQuery) matches(s searchText) bool {
	for _, group := range q.groups {
		found := false
		for _, t := range group {
			if t.matches(s) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (q searchQuery) hasPositiveTerms() bool {
	for _, group := range q.groups {
		for _, t := range group {
			if !t.negative {
				return true
			}
		}
	}
	return false
}

// Use the same best alignment as highlighting. Exclusions constrain membership
// but do not improve rank; each OR group contributes its best positive match.
func (q searchQuery) score(ctx context.Context, s searchText, work *searchWorkspace) (int, bool) {
	total := 0
	for _, group := range q.groups {
		best, found := 0, false
		for _, t := range group {
			if t.negative || !t.matches(s) {
				continue
			}
			_, score, ok := work.align(ctx, t, s, false)
			if !ok {
				return 0, false
			}
			if !found || score > best {
				best, found = score, true
			}
		}
		total += best
	}
	return total, ctx.Err() == nil
}

func wordIndex(s, word string) int {
	for offset := 0; offset <= len(s); {
		i := strings.Index(s[offset:], word)
		if i < 0 {
			break
		}
		i += offset
		before, _ := utf8.DecodeLastRuneInString(s[:i])
		after, _ := utf8.DecodeRuneInString(s[i+len(word):])
		if (i == 0 || !wordRune(before)) && (i+len(word) == len(s) || !wordRune(after)) {
			return i
		}
		_, width := utf8.DecodeRuneInString(s[i:])
		if width == 0 {
			break
		}
		offset = i + width
	}
	return -1
}

func wordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) || r == '_'
}

// Highlight positions are only needed for visible rows and the focused preview.
// Relevance sorting reuses a workspace to compute scores without traceback.
func (q searchQuery) positions(s searchText) []int {
	var positions []int
	for _, group := range q.groups {
		var best []int
		bestScore := -1 << 30
		for _, t := range group {
			if t.negative || !t.matches(s) {
				continue
			}
			p, score := t.positions(s)
			if score > bestScore {
				best, bestScore = p, score
			}
		}
		positions = append(positions, best...)
	}
	slices.Sort(positions)
	return slices.Compact(positions)
}

// searchWorkspace reuses normalization and DP buffers across scored entries.
// Traceback and returned positions are allocated only for visible highlights.
type searchWorkspace struct {
	raw, hay, needle           []rune
	indices, previous, current []int
}

func (t searchTerm) positions(s searchText) ([]int, int) {
	var work searchWorkspace
	p, score, _ := work.align(context.Background(), t, s, true)
	return p, score
}

func (w *searchWorkspace) align(ctx context.Context, t searchTerm, s searchText, positions bool) ([]int, int, bool) {
	w.raw = w.raw[:0]
	for _, r := range s.text {
		if len(w.raw)%4096 == 0 && ctx.Err() != nil {
			return nil, 0, false
		}
		w.raw = append(w.raw, r)
	}
	w.hay, w.indices = w.hay[:0], w.indices[:0]
	latin := false
	for i, r := range w.raw {
		if i%4096 == 0 && ctx.Err() != nil {
			return nil, 0, false
		}
		if t.normalize {
			if r < utf8.RuneSelf {
				latin = asciiLetter(r)
			} else {
				if latin && unicode.Is(unicode.Mn, r) {
					continue
				}
				if !unicode.IsMark(r) {
					latin = unicode.In(r, unicode.Latin)
				}
				r = latinBase(r)
			}
		}
		if !t.sensitive {
			r = unicode.ToLower(r)
		}
		w.hay, w.indices = append(w.hay, r), append(w.indices, i)
	}
	w.needle = w.needle[:0]
	for _, r := range t.text {
		w.needle = append(w.needle, r)
	}
	var p []int
	score := 0
	if t.fuzzy {
		var ok bool
		p, score, ok = w.alignFuzzy(ctx, positions)
		if !ok {
			return nil, 0, false
		}
	} else {
		view := string(w.hay)
		at := strings.Index(view, t.text)
		if t.prefix {
			at = 0
		} else if t.end {
			at = len(view) - len(t.text)
		} else if t.word {
			at = wordIndex(view, t.text)
		}
		if at >= 0 {
			start := utf8.RuneCountInString(view[:at])
			for i := range w.needle {
				index := start + i
				if positions {
					p = append(p, index)
				}
				if i == 0 {
					score = 16 + 2*alignmentBonus(w.raw, w.indices[index]) - index
				} else {
					score += 24 + alignmentBonus(w.raw, w.indices[index])
				}
			}
		}
	}
	for i := range p {
		p[i] = w.indices[p[i]]
	}
	return p, score, ctx.Err() == nil
}

func alignmentBonus(raw []rune, at int) int {
	if at == 0 || unicode.IsSpace(raw[at-1]) || strings.ContainsRune("/\\_-.:;", raw[at-1]) {
		return 12
	}
	if !wordRune(raw[at-1]) || (unicode.IsLower(raw[at-1]) && unicode.IsUpper(raw[at])) {
		return 9
	}
	return 0
}

// Dynamic programming maximizes boundary/contiguous bonuses and penalizes gaps.
// Limit trace memory on pathological commands, as terminal previews can contain
// arbitrarily long history entries. Membership is exact in both paths.
func (w *searchWorkspace) alignFuzzy(ctx context.Context, positions bool) ([]int, int, bool) {
	hay, needle, raw, indices := w.hay, w.needle, w.raw, w.indices
	n, m := len(hay), len(needle)
	if m == 0 || n < m {
		return nil, 0, true
	}
	if n > 2_000_000/m {
		var p []int
		j := 0
		for i, r := range hay {
			if i%4096 == 0 && ctx.Err() != nil {
				return nil, 0, false
			}
			if r == needle[j] {
				if positions {
					p = append(p, i)
				}
				j++
				if j == m {
					return p, 0, true
				}
			}
		}
		return nil, 0, true
	}
	const impossible = -1 << 28
	w.previous = resizeSearchBuffer(w.previous, n)
	w.current = resizeSearchBuffer(w.current, n)
	prev, current := w.previous, w.current
	clear(prev)
	var trace []int32
	if positions {
		trace = make([]int32, n*m)
	}
	for j, r := range needle {
		best, bestAt := impossible, -1
		for i, c := range hay {
			if i%4096 == 0 && ctx.Err() != nil {
				return nil, 0, false
			}
			current[i] = impossible
			if positions {
				trace[j*n+i] = -1
			}
			if c == r {
				bonus := alignmentBonus(raw, indices[i])
				if j == 0 {
					current[i] = 16 + 2*bonus - i
				} else {
					if bestAt >= 0 {
						current[i] = best - 3*i + 16 + bonus
						if positions {
							trace[j*n+i] = int32(bestAt)
						}
					}
					if i > 0 && prev[i-1] > impossible && prev[i-1]+24+bonus > current[i] {
						current[i] = prev[i-1] + 24 + bonus
						if positions {
							trace[j*n+i] = int32(i - 1)
						}
					}
				}
			}
			if prev[i] > impossible && prev[i]+3*i > best {
				best, bestAt = prev[i]+3*i, i
			}
		}
		prev, current = current, prev
	}
	at, score := -1, impossible
	for i, value := range prev {
		if value > score {
			at, score = i, value
		}
	}
	if at < 0 {
		return nil, 0, true
	}
	if !positions {
		return nil, score, true
	}
	p := make([]int, m)
	for j := m - 1; j >= 0; j-- {
		p[j] = at
		at = int(trace[j*n+at])
	}
	return p, score, true
}

func resizeSearchBuffer(buffer []int, n int) []int {
	if cap(buffer) < n {
		return make([]int, n)
	}
	return buffer[:n]
}

func foldLatin(s string) string {
	latin := false
	return strings.Map(func(r rune) rune {
		if r < utf8.RuneSelf {
			latin = asciiLetter(r)
			return r
		}
		if latin && unicode.Is(unicode.Mn, r) {
			return -1
		}
		if !unicode.IsMark(r) {
			latin = unicode.In(r, unicode.Latin)
		}
		return latinBase(r)
	}, s)
}

func asciiLetter(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}

// Deliberately bounded Latin folding, rather than a claim of full Unicode
// normalization. Explicitly accented query terms use the original text.
var latinFolds = func() map[rune]rune {
	const groups = "ÀÁÂÃÄÅĀĂĄǍǞǠǺȀȂȦẠẢẤẦẨẪẬẮẰẲẴẶ|àáâãäåāăąǎǟǡǻȁȃȧạảấầẩẫậắằẳẵặ|ÇĆĈĊČ|çćĉċč|ĎĐḌḎḐ|ďđḍḏḑ|ÈÉÊËĒĔĖĘĚȄȆẸẺẼẾỀỂỄỆ|èéêëēĕėęěȅȇẹẻẽếềểễệ|ĜĞĠĢǦ|ĝğġģǧ|ĤĦḤ|ĥħḥ|ÌÍÎÏĨĪĬĮİǏȈȊỊỈ|ìíîïĩīĭįıǐȉȋịỉ|Ĵ|ĵ|ĶǨḲ|ķǩḳ|ĹĻĽŁḶ|ĺļľłḷ|ÑŃŅŇṄṆ|ñńņňṅṇ|ÒÓÔÕÖØŌŎŐǑǪǬǾȌȎȪȬȮȰỌỎỐỒỔỖỘỚỜỞỠỢƠ|òóôõöøōŏőǒǫǭǿȍȏȫȭȯȱọỏốồổỗộớờởỡợơ|ŔŖŘṚ|ŕŗřṛ|ŚŜŞŠȘṢ|śŝşšșṣ|ŢŤŦȚṬ|ţťŧțṭ|ÙÚÛÜŨŪŬŮŰŲǓǕǗǙǛȔȖỤỦỨỪỬỮỰƯ|ùúûüũūŭůűųǔǖǘǚǜȕȗụủứừửữựư|ŴẀẂẄ|ŵẁẃẅ|ÝŶŸỲỴỶỸ|ýÿŷỳỵỷỹ|ŹŻŽẒ|źżžẓ"
	const bases = "AaCcDdEeGgHhIiJjKkLlNnOoRrSsTtUuWwYyZz"
	folds := make(map[rune]rune)
	group := 0
	for _, c := range groups {
		if c == '|' {
			group++
		} else {
			folds[c] = rune(bases[group])
		}
	}
	return folds
}()

func latinBase(r rune) rune {
	if r >= 0xC0 {
		if base, ok := latinFolds[r]; ok {
			return base
		}
	}
	return r
}
