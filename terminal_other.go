//go:build !darwin && !linux

package main

import "fmt"

func runPicker(*pickerModel) ([]*Entry, error) {
	return nil, fmt.Errorf("interactive picker is supported on macOS and Linux")
}

func isTerminal(int) bool { return false }
