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
	literalPrefix                []int
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
		t.sensitive = t.sensitive || unicode.IsUpper(c.r) || unicode.IsTitle(c.r)
		if latinBase(c.r) != c.r || unicode.Is(unicode.Mn, c.r) {
			t.normalize = false
		}
	}
	t.text = b.String()
	if !t.sensitive {
		t.text = strings.ToLower(t.text)
	}
	if !t.fuzzy && !t.prefix && !t.end && len(t.text) >= 64 {
		// Compile long literals once. Repeated overlapping substring/word
		// matches would otherwise rescan the entire literal at each offset.
		t.literalPrefix = make([]int, len(t.text))
		for i, j := 1, 0; i < len(t.text); i++ {
			for j > 0 && t.text[i] != t.text[j] {
				j = t.literalPrefix[j-1]
			}
			if t.text[i] == t.text[j] {
				j++
			}
			t.literalPrefix[i] = j
		}
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

func (t *searchTerm) matches(s searchText) bool {
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
		matched = t.wordIndex(hay) >= 0
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
		for i := range group {
			if group[i].matches(s) {
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

// visitLiteral visits overlapping occurrences, stopping when visit returns
// false. Long literals use KMP; short ones keep the optimized stdlib search.
// The result reports cancellation, independently of an early visitor stop.
func (t searchTerm) visitLiteral(ctx context.Context, s string, visit func(int) bool) bool {
	if len(t.literalPrefix) == 0 {
		for offset := 0; offset <= len(s); {
			if ctx.Err() != nil {
				return false
			}
			i := strings.Index(s[offset:], t.text)
			if i < 0 {
				break
			}
			i += offset
			if !visit(i) {
				return true
			}
			_, width := utf8.DecodeRuneInString(s[i:])
			if width == 0 {
				break
			}
			offset = i + width
		}
	} else {
		for i, j := 0, 0; i < len(s); i++ {
			if i%4096 == 0 && ctx.Err() != nil {
				return false
			}
			for j > 0 && s[i] != t.text[j] {
				j = t.literalPrefix[j-1]
			}
			if s[i] == t.text[j] {
				j++
			}
			if j == len(t.text) {
				j = t.literalPrefix[j-1]
				if !visit(i + 1 - len(t.text)) {
					return true
				}
			}
		}
	}
	return ctx.Err() == nil
}

func (t searchTerm) wordIndex(s string) int {
	at := -1
	t.visitLiteral(context.Background(), s, func(i int) bool {
		if wordBoundaries(s, i, i+len(t.text)) {
			at = i
			return false
		}
		return true
	})
	return at
}

func wordBoundaries(s string, start, end int) bool {
	before, _ := utf8.DecodeLastRuneInString(s[:start])
	after, _ := utf8.DecodeRuneInString(s[end:])
	return (start == 0 || !wordRune(before)) && (end == len(s) || !wordRune(after))
}

func wordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) || r == '_'
}

// Highlight positions are only needed for visible rows and the focused preview.
// Relevance sorting reuses a workspace to compute scores without traceback.
func (q searchQuery) positions(s searchText) []int {
	var work searchWorkspace
	var positions []int
	for _, group := range q.groups {
		var best []int
		bestScore, found := 0, false
		for _, t := range group {
			if t.negative || !t.matches(s) {
				continue
			}
			p, score, _ := work.align(context.Background(), t, s, true)
			if !found || score > bestScore {
				best, bestScore, found = p, score, true
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
	first, last, bonusSums     []int
	source                     string
	prepared, viewPrepared     bool
	normalize, sensitive       bool
}

func (t searchTerm) positions(s searchText) ([]int, int) {
	var work searchWorkspace
	p, score, _ := work.align(context.Background(), t, s, true)
	return p, score
}

func (w *searchWorkspace) align(ctx context.Context, t searchTerm, s searchText, positions bool) ([]int, int, bool) {
	if !w.prepared || w.source != s.text {
		w.prepared, w.viewPrepared = false, false
		w.raw = w.raw[:0]
		for _, r := range s.text {
			if len(w.raw)%4096 == 0 && ctx.Err() != nil {
				return nil, 0, false
			}
			w.raw = append(w.raw, r)
		}
		w.source, w.prepared = s.text, true
	}
	if !w.viewPrepared || w.normalize != t.normalize || w.sensitive != t.sensitive {
		w.viewPrepared = false
		w.hay, w.indices = w.hay[:0], w.indices[:0]
		w.bonusSums = append(w.bonusSums[:0], 0)
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
			w.bonusSums = append(w.bonusSums, w.bonusSums[len(w.bonusSums)-1]+alignmentBonus(w.raw, i))
		}
		w.normalize, w.sensitive, w.viewPrepared = t.normalize, t.sensitive, true
	}
	w.needle = w.needle[:0]
	for _, r := range t.text {
		w.needle = append(w.needle, r)
	}
	var p []int
	var score int
	var ok bool
	if t.fuzzy {
		p, score, ok = w.alignFuzzy(ctx, positions)
	} else {
		p, score, ok = w.alignExact(ctx, t, t.view(s), positions)
	}
	if !ok {
		return nil, 0, false
	}
	for i := range p {
		p[i] = w.indices[p[i]]
	}
	return p, score, ctx.Err() == nil
}

func (w *searchWorkspace) alignExact(ctx context.Context, t searchTerm, view string, positions bool) ([]int, int, bool) {
	bestAt, bestScore := -1, 0
	// Byte offsets belong to the cached string view; rune offsets belong
	// to the alignment view. Count only the newly scanned bytes.
	byteAt, runeAt := 0, 0
	visit := func(at int) bool {
		start := runeAt + utf8.RuneCountInString(view[byteAt:at])
		byteAt, runeAt = at, start
		if !t.word || t.prefix || t.end || wordBoundaries(view, at, at+len(t.text)) {
			value := 0
			if len(w.needle) > 0 {
				bonus := w.bonusSums[start+1] - w.bonusSums[start]
				value = 24*len(w.needle) - 8 - start + bonus + w.bonusSums[start+len(w.needle)] - w.bonusSums[start]
			}
			if bestAt < 0 || value > bestScore {
				bestAt, bestScore = start, value
			}
		}
		// Every bonus is at most 12. No later occurrence can beat this
		// upper bound, so repetitive commands usually need only one match.
		return bestAt < 0 || 36*len(w.needle)+4-(start+1) > bestScore
	}
	if t.prefix {
		visit(0)
	} else if t.end {
		visit(len(view) - len(t.text))
	} else if !t.visitLiteral(ctx, view, visit) {
		return nil, 0, false
	}
	if bestAt < 0 || !positions {
		return nil, bestScore, true
	}
	p := make([]int, len(w.needle))
	for i := range p {
		p[i] = bestAt + i
	}
	return p, bestScore, true
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
	hay, needle := w.hay, w.needle
	n, m := len(hay), len(needle)
	if m == 0 || n < m {
		return nil, 0, true
	}
	// Earliest/latest feasible positions prune cells that cannot participate
	// in a complete match, including irrelevant command prefixes and suffixes.
	w.first = resizeSearchBuffer(w.first, m)
	w.last = resizeSearchBuffer(w.last, m)
	j := 0
	for i, r := range hay {
		if i%4096 == 0 && ctx.Err() != nil {
			return nil, 0, false
		}
		if r == needle[j] {
			w.first[j] = i
			j++
			if j == m {
				break
			}
		}
	}
	if j != m {
		return nil, 0, true
	}
	j = m - 1
	for i := n - 1; i >= 0; i-- {
		if i%4096 == 0 && ctx.Err() != nil {
			return nil, 0, false
		}
		if hay[i] == needle[j] {
			w.last[j] = i
			j--
			if j < 0 {
				break
			}
		}
	}
	start, end := w.first[0], w.last[m-1]+1
	n = end - start
	if n > 2_000_000/m {
		// The bounded fallback keeps exact membership and scores the returned
		// greedy alignment with the same bonuses and penalties as the DP.
		score := 0
		for j, at := range w.first {
			bonus := w.bonusSums[at+1] - w.bonusSums[at]
			if j == 0 {
				score = 16 + 2*bonus - at
			} else if at == w.first[j-1]+1 {
				score += 24 + bonus
			} else {
				score += 16 + bonus - 3*(at-w.first[j-1])
			}
		}
		if positions {
			return slices.Clone(w.first), score, true
		}
		return nil, score, true
	}
	const impossible = -int(^uint(0)>>1) / 2
	w.previous = resizeSearchBuffer(w.previous, n)
	w.current = resizeSearchBuffer(w.current, n)
	prev, current := w.previous, w.current
	var trace []int32
	if positions {
		trace = make([]int32, n*m)
	}
	for j, r := range needle {
		best, bestAt := impossible, -1
		low, high := w.first[j]-start, w.last[j]-start
		previousLow, previousHigh := 0, -1
		if j > 0 {
			previousLow, previousHigh = w.first[j-1]-start, w.last[j-1]-start
		}
		k := previousLow
		for i := low; i <= high; i++ {
			if (i-low)%4096 == 0 && ctx.Err() != nil {
				return nil, 0, false
			}
			for k < i && k <= previousHigh {
				if k%4096 == 0 && ctx.Err() != nil {
					return nil, 0, false
				}
				if prev[k] > impossible && prev[k]+3*k > best {
					best, bestAt = prev[k]+3*k, k
				}
				k++
			}
			current[i] = impossible
			if hay[start+i] == r {
				bonus := w.bonusSums[start+i+1] - w.bonusSums[start+i]
				if j == 0 {
					current[i] = 16 + 2*bonus - (start + i)
				} else {
					if bestAt >= 0 {
						current[i] = best - 3*i + 16 + bonus
						if positions {
							trace[j*n+i] = int32(bestAt)
						}
					}
					if i-1 >= previousLow && i-1 <= previousHigh && prev[i-1] > impossible && prev[i-1]+24+bonus > current[i] {
						current[i] = prev[i-1] + 24 + bonus
						if positions {
							trace[j*n+i] = int32(i - 1)
						}
					}
				}
			}
		}
		prev, current = current, prev
	}
	at, score := -1, impossible
	for i := w.first[m-1] - start; i <= w.last[m-1]-start; i++ {
		value := prev[i]
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
		p[j] = start + at
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
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			ascii = false
			break
		}
	}
	if ascii {
		return s
	}
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
