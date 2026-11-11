package main

func (m *pickerModel) rowItems(item int) []int {
	if m.options.sort == pickerFrequency {
		if group := m.groups[m.items[item].text.text]; len(group) > 0 {
			return group
		}
	}
	return []int{item}
}

func (m *pickerModel) rowMark(item int) byte {
	if len(m.marked) == 0 {
		return ' '
	}
	marked, unmarked := false, false
	for _, member := range m.rowItems(item) {
		if m.marked[member] {
			marked = true
		} else {
			unmarked = true
		}
		if marked && unmarked {
			return '-'
		}
	}
	if marked {
		return 'x'
	}
	return ' '
}

func (m *pickerModel) selectedCommands() int {
	commands := make(map[string]bool)
	for item := range m.marked {
		commands[m.items[item].text.text] = true
	}
	return len(commands)
}
