package main

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

type pickerEvent struct {
	key, text    string
	mouseButton  int
	mouseRelease bool
	x, y         int // Zero-based terminal cells.
	err          error
}

type pickerDecoder struct {
	pending []byte
	paste   bool
	text    []byte
}

// feed accepts arbitrary read boundaries, including split UTF-8, CSI and paste
// sequences. timeout distinguishes a standalone Escape from a sequence prefix.
func (d *pickerDecoder) feed(data []byte, timeout bool) []pickerEvent {
	d.pending = append(d.pending, data...)
	var events []pickerEvent
	for len(d.pending) != 0 {
		if d.paste {
			const end = "\x1b[201~"
			if i := strings.Index(string(d.pending), end); i >= 0 {
				d.text = append(d.text, d.pending[:i]...)
				events = append(events, pickerEvent{key: "paste", text: string(d.text)})
				d.text = nil
				d.pending = d.pending[i+len(end):]
				d.paste = false
				continue
			}
			// Retain only a possible terminator prefix between reads.
			keep := min(len(d.pending), len(end)-1)
			d.text = append(d.text, d.pending[:len(d.pending)-keep]...)
			d.pending = d.pending[len(d.pending)-keep:]
			break
		}
		c := d.pending[0]
		if c == 0x1b {
			if len(d.pending) == 1 {
				if !timeout {
					break
				}
				events = append(events, pickerEvent{key: "escape"})
				d.pending = nil
				continue
			}
			if d.pending[1] == 0x1b {
				// Some terminals encode Alt-arrow as Escape + a normal arrow.
				up, down := "\x1b\x1b[A", "\x1b\x1b[B"
				pending := string(d.pending)
				if !timeout && len(pending) < len(up) && (strings.HasPrefix(up, pending) || strings.HasPrefix(down, pending)) {
					break
				}
				if strings.HasPrefix(pending, up) || strings.HasPrefix(pending, down) {
					key := "alt-up"
					if strings.HasPrefix(pending, down) {
						key = "alt-down"
					}
					events = append(events, pickerEvent{key: key})
					d.pending = d.pending[4:]
					continue
				}
				events = append(events, pickerEvent{key: "escape"})
				d.pending = d.pending[1:]
				continue
			}
			if d.pending[1] == '[' || d.pending[1] == 'O' {
				end := 2
				if len(d.pending) > 2 && d.pending[1] == '[' && d.pending[2] == '[' {
					end = 3 // Linux console F1 is CSI [ A.
				}
				for end < len(d.pending) && !(d.pending[end] >= 0x40 && d.pending[end] <= 0x7e) {
					end++
				}
				if end == len(d.pending) {
					if timeout || end > 128 {
						d.pending = nil
					}
					break
				}
				sequence := string(d.pending[:end+1])
				d.pending = d.pending[end+1:]
				if sequence == "\x1b[200~" {
					d.paste = true
					continue
				}
				if e, ok := decodePickerSequence(sequence); ok {
					events = append(events, e)
				}
				continue
			}
			// Only Alt-arrow combinations have meaning here. Treat Escape
			// followed by ordinary typing as separate keys, so quickly leaving
			// help cannot swallow the first character of the next query.
			events = append(events, pickerEvent{key: "escape"})
			d.pending = d.pending[1:]
			continue
		}
		if c < 0x20 || c == 0x7f {
			key := pickerControlKey(c)
			d.pending = d.pending[1:]
			if key != "" {
				events = append(events, pickerEvent{key: key})
			}
			continue
		}
		n := 0
		for n < len(d.pending) && d.pending[n] >= 0x20 && d.pending[n] != 0x7f && utf8.FullRune(d.pending[n:]) {
			_, size := utf8.DecodeRune(d.pending[n:])
			n += size
		}
		if n == 0 {
			break
		}
		events = append(events, pickerEvent{key: "text", text: string(d.pending[:n])})
		d.pending = d.pending[n:]
	}
	return events
}

func decodePickerSequence(s string) (pickerEvent, bool) {
	if key := pickerSequenceKey(s); key != "" {
		return pickerEvent{key: key}, true
	}
	if strings.HasPrefix(s, "\x1b[<") && (strings.HasSuffix(s, "M") || strings.HasSuffix(s, "m")) {
		parts := strings.Split(s[3:len(s)-1], ";")
		if len(parts) == 3 {
			button, e1 := strconv.Atoi(parts[0])
			x, e2 := strconv.Atoi(parts[1])
			y, e3 := strconv.Atoi(parts[2])
			if e1 == nil && e2 == nil && e3 == nil && button >= 0 && x > 0 && y > 0 {
				return pickerEvent{key: "mouse", mouseButton: button, mouseRelease: strings.HasSuffix(s, "m"), x: x - 1, y: y - 1}, true
			}
		}
	}
	return pickerEvent{}, false
}

func pickerControlKey(c byte) string {
	switch c {
	case 1:
		return "ctrl-a"
	case 3:
		return "cancel"
	case 4:
		return "delete"
	case 5:
		return "ctrl-e"
	case 8, 127:
		return "backspace"
	case 9:
		return "tab"
	case 10, 13:
		return "enter"
	case 12:
		return "redraw"
	case 14:
		return "ctrl-n"
	case 16:
		return "ctrl-p"
	case 19:
		return "ctrl-s"
	case 21:
		return "ctrl-u"
	case 23:
		return "ctrl-w"
	case 26:
		return "suspend"
	}
	return ""
}

func pickerSequenceKey(s string) string {
	switch s {
	case "\x1b[A", "\x1bOA":
		return "up"
	case "\x1b[B", "\x1bOB":
		return "down"
	case "\x1b[C", "\x1bOC":
		return "right"
	case "\x1b[D", "\x1bOD":
		return "left"
	case "\x1b[H", "\x1bOH", "\x1b[1~", "\x1b[7~":
		return "home"
	case "\x1b[F", "\x1bOF", "\x1b[4~", "\x1b[8~":
		return "end"
	case "\x1b[3~":
		return "delete"
	case "\x1b[5~":
		return "page-up"
	case "\x1b[6~":
		return "page-down"
	case "\x1b[Z":
		return "back-tab"
	case "\x1bOP", "\x1b[11~", "\x1b[[A":
		return "help"
	case "\x1b[1;3A":
		return "alt-up"
	case "\x1b[1;3B":
		return "alt-down"
	}
	return ""
}
