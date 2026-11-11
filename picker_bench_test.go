package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func benchmarkPicker(lines, lineBytes int) *pickerModel {
	entries := make([]*Entry, 40)
	for i := range entries {
		command := fmt.Sprintf("git checkout feature/%d\n", i) + strings.Repeat("echo cafe && printf payload-1234567890\n", lines)
		if lineBytes > 0 {
			command = "git checkout " + strings.Repeat("payload ", lineBytes/8)
		}
		entries[i] = &Entry{Raw: []string{command}}
	}
	m := newPickerModel(entries, entries, pickerOptions{multi: true, path: "history"})
	m.width, m.height = 100, 32
	m.setQuery("cafe")
	if lineBytes > 0 {
		m.setQuery("gco")
	}
	matches, _ := filterPicker(context.Background(), m.items, m.query, m.options.sort)
	m.applyMatches(matches, -1)
	m.render(true)
	return m
}

func BenchmarkPickerRenderMultiline(b *testing.B) {
	for _, lines := range []int{100, 1000} {
		b.Run(fmt.Sprint(lines), func(b *testing.B) {
			m := benchmarkPicker(lines, 0)
			b.ReportAllocs()
			for b.Loop() {
				m.render(true)
			}
		})
	}
}

func BenchmarkPickerPreviewScroll(b *testing.B) {
	m := benchmarkPicker(1000, 0)
	delta := 1
	b.ReportAllocs()
	for b.Loop() {
		m.scrollPreview(delta)
		m.render(true)
		delta = -delta
	}
}

func BenchmarkPickerFocusMovement(b *testing.B) {
	m := benchmarkPicker(1000, 0)
	delta := 1
	b.ReportAllocs()
	for b.Loop() {
		m.move(delta)
		m.render(true)
		delta = -delta
	}
}

func BenchmarkPickerWrapCold(b *testing.B) {
	for _, lines := range []int{100, 1000} {
		b.Run(fmt.Sprint(lines), func(b *testing.B) {
			text := "git checkout\n" + strings.Repeat("echo cafe && printf payload-1234567890\n", lines)
			query, _ := parseSearch("cafe")
			positions := query.positions(newSearchText(text))
			b.ReportAllocs()
			for b.Loop() {
				wrapPickerText(text, positions, 98)
			}
		})
	}
}

func BenchmarkPickerRenderLongLine(b *testing.B) {
	m := benchmarkPicker(0, 100_000)
	b.ReportAllocs()
	for b.Loop() {
		m.render(true)
	}
}

func BenchmarkPickerLongCluster(b *testing.B) {
	for _, count := range []int{1_000, 10_000, 40_000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			text := "e" + strings.Repeat("\u0301", count)
			b.ReportAllocs()
			for b.Loop() {
				pickerGlyphsWithin(text, nil, 80)
			}
		})
	}
}

func BenchmarkPickerGroupedRender(b *testing.B) {
	for _, count := range []int{1_000, 100_000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			entries := make([]*Entry, count)
			for i := range entries {
				entries[i] = &Entry{Raw: []string{"git status"}}
			}
			m := newPickerModel(entries, entries, pickerOptions{multi: true, sort: pickerFrequency})
			m.render(false)
			b.ReportAllocs()
			for b.Loop() {
				m.render(false)
			}
		})
	}
}

func BenchmarkPickerSort10K(b *testing.B) {
	entries := make([]*Entry, 10_000)
	for i := range entries {
		entries[i] = &Entry{Raw: []string{fmt.Sprintf("git checkout feature/%d && go test ./... # café", i)}}
	}
	m := newPickerModel(entries, entries, pickerOptions{})
	q, _ := parseSearch("gco cafe")
	for _, order := range []pickerSort{pickerNewest, pickerRelevance, pickerFrequency} {
		b.Run(string(order), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				filterPicker(context.Background(), m.items, q, order)
			}
		})
	}
}

func BenchmarkPickerStartup10K(b *testing.B) {
	for _, distinct := range []int{100, 10_000} {
		b.Run(fmt.Sprint(distinct), func(b *testing.B) {
			entries := make([]*Entry, 10_000)
			for i := range entries {
				entries[i] = &Entry{Raw: []string{fmt.Sprintf(": %d:0;PATH=/tmp/Café_%d docker RUN --NAME=myContainer", 1000+i, i%distinct)}}
			}
			b.ReportAllocs()
			for b.Loop() {
				newPickerModel(entries, entries, pickerOptions{})
			}
		})
	}
}
