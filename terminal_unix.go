//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"
	"unsafe"
)

const pickerEnterTerminal = "\x1b[?1049h\x1b[?1000h\x1b[?1006h\x1b[?2004h\x1b[2J\x1b[H"
const pickerLeaveTerminal = "\x1b[0m\x1b[?1000l\x1b[?1006l\x1b[?2004l\x1b[?25h\x1b[?1049l"

type terminalState = syscall.Termios

func terminalIOCTL(fd int, request uint, data unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(request), uintptr(data))
	if errno != 0 {
		return errno
	}
	return nil
}

func readTerminalState(fd int) (*terminalState, error) {
	var state terminalState
	err := terminalIOCTL(fd, terminalGetState, unsafe.Pointer(&state))
	return &state, err
}

func makeTerminalRaw(fd int, state *terminalState) error {
	raw := *state
	raw.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP | syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	raw.Oflag &^= syscall.OPOST
	raw.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	raw.Cflag &^= syscall.CSIZE | syscall.PARENB
	raw.Cflag |= syscall.CS8
	raw.Cc[syscall.VMIN], raw.Cc[syscall.VTIME] = 0, 1
	return terminalIOCTL(fd, terminalSetState, unsafe.Pointer(&raw))
}

func restoreTerminal(fd int, state *terminalState) error {
	if err := terminalIOCTL(fd, terminalSetState, unsafe.Pointer(state)); err != nil {
		return fmt.Errorf("restore terminal: %w", err)
	}
	return nil
}

func terminalSize(fd int) (int, int, error) {
	var size struct{ rows, cols, x, y uint16 }
	err := terminalIOCTL(fd, syscall.TIOCGWINSZ, unsafe.Pointer(&size))
	return int(size.cols), int(size.rows), err
}

type pickerSearchResult struct {
	generation int
	matches    []int
	previous   int
}

func runPicker(m *pickerModel) (selected []*Entry, err error) {
	if term := os.Getenv("TERM"); term == "" || term == "dumb" {
		return nil, fmt.Errorf("interactive picker requires a capable terminal (TERM is %q)", term)
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("interactive picker requires a controlling terminal: %w", err)
	}
	defer tty.Close()
	fd := int(tty.Fd())
	state, err := readTerminalState(fd)
	if err != nil {
		return nil, fmt.Errorf("read terminal settings: %w", err)
	}
	signals := make(chan os.Signal, 8)
	signal.Notify(signals, syscall.SIGWINCH, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGTSTP, syscall.SIGCONT)
	defer signal.Stop(signals)
	active := false
	leave := func() error {
		if !active {
			return nil
		}
		active = false
		_, writeErr := io.WriteString(tty, pickerLeaveTerminal)
		return errors.Join(writeErr, restoreTerminal(fd, state))
	}
	defer func() { err = errors.Join(err, leave()) }()
	enter := func() error {
		if err := makeTerminalRaw(fd, state); err != nil {
			return err
		}
		active = true
		_, err := io.WriteString(tty, pickerEnterTerminal)
		return err
	}
	resize := func() error {
		width, height, err := terminalSize(fd)
		if err != nil {
			return fmt.Errorf("read terminal size: %w", err)
		}
		if width < 30 || height < 10 {
			return fmt.Errorf("picker needs a terminal of at least 30 columns and 10 rows (got %d×%d)", width, height)
		}
		m.width, m.height = width, height
		m.ensureVisible()
		m.revealMatch()
		return nil
	}
	if err := resize(); err != nil {
		return nil, err
	}
	if err := enter(); err != nil {
		return nil, fmt.Errorf("start interactive picker: %w", err)
	}
	color := os.Getenv("NO_COLOR") == ""
	if _, err := io.WriteString(tty, m.render(color)); err != nil {
		return nil, err
	}
	events := make(chan pickerEvent, 128)
	var stopReader, readerDone chan struct{}
	startInput := func() {
		stopReader, readerDone = make(chan struct{}), make(chan struct{})
		go readPickerEvents(fd, events, stopReader, readerDone)
	}
	stopInput := func() {
		if stopReader == nil {
			return
		}
		close(stopReader)
		<-readerDone
		stopReader = nil
		for {
			select {
			case <-events:
			default:
				return
			}
		}
	}
	startInput()
	// Stop and join the reader before restoring the terminal or asking for
	// deletion confirmation. No input goroutine may outlive the picker.
	defer stopInput()
	results := make(chan pickerSearchResult, 1)
	ctx, cancelAll := context.WithCancel(context.Background())
	defer cancelAll()
	cancelSearch := func() {}
	defer func() { cancelSearch() }()
	generation := 0
	startSearch := func() {
		cancelSearch()
		generation++
		previous := m.focused()
		m.setQuery(string(m.input))
		m.matches, m.focus, m.offset, m.previewScroll = nil, 0, 0, 0
		m.searching = m.queryErr == nil
		if !m.searching {
			return
		}
		searchCtx, cancel := context.WithCancel(ctx)
		cancelSearch = cancel
		q, id := m.query, generation
		go func() {
			if matches, ok := filterPicker(searchCtx, m.items, q); ok {
				select {
				case results <- pickerSearchResult{id, matches, previous}:
				case <-searchCtx.Done():
				}
			}
		}()
	}
	suspend := func() error {
		stopInput()
		if err := leave(); err != nil {
			return err
		}
		// SIGSTOP cannot be recaptured by Go's asynchronous signal handler.
		// Execution continues here only after the shell resumes the job.
		if err := syscall.Kill(os.Getpid(), syscall.SIGSTOP); err != nil {
			return err
		}
		if err := enter(); err != nil {
			return err
		}
		if err := resize(); err != nil {
			return err
		}
		startInput()
		return nil
	}
	ticker := time.NewTicker(time.Second / 60)
	defer ticker.Stop()
	dirty := false
	for {
		select {
		case e := <-events:
			if e.err != nil {
				return nil, fmt.Errorf("read picker input: %w", e.err)
			}
			switch e.key {
			case "cancel":
				return nil, nil
			case "escape":
				if !m.help {
					return nil, nil
				}
			case "suspend":
				if err := suspend(); err != nil {
					return nil, err
				}
			case "enter":
				if !m.help {
					if entries, accepted := m.accept(); accepted {
						return entries, nil
					}
				}
			case "mouse":
				if m.mouse(e, time.Now()) {
					if entries, accepted := m.accept(); accepted {
						return entries, nil
					}
				}
			}
			if m.key(e) {
				startSearch()
			}
			dirty = true
		case result := <-results:
			if result.generation == generation {
				m.applyMatches(result.matches, result.previous)
				if m.acceptPending {
					if entries, accepted := m.accept(); accepted {
						return entries, nil
					}
				}
				dirty = true
			}
		case sig := <-signals:
			switch sig {
			case syscall.SIGWINCH, syscall.SIGCONT:
				if err := resize(); err != nil {
					return nil, err
				}
			case syscall.SIGTSTP:
				m.acceptPending = false
				if err := suspend(); err != nil {
					return nil, err
				}
			case syscall.SIGINT:
				return nil, nil
			default:
				return nil, fmt.Errorf("picker interrupted by %s", sig)
			}
			dirty = true
		case <-ticker.C:
			if dirty {
				if _, err := io.WriteString(tty, m.render(color)); err != nil {
					return nil, fmt.Errorf("draw picker: %w", err)
				}
				dirty = false
			}
		}
	}
}

func readPickerEvents(fd int, events chan<- pickerEvent, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	var decoder pickerDecoder
	buf := make([]byte, 4096)
	for {
		select {
		case <-stop:
			return
		default:
		}
		n, err := syscall.Read(fd, buf)
		if err == syscall.EINTR {
			continue
		}
		var next []pickerEvent
		if err != nil {
			next = []pickerEvent{{err: err}}
		} else {
			next = decoder.feed(buf[:n], n == 0)
		}
		for _, e := range next {
			select {
			case events <- e:
			case <-stop:
				return
			}
		}
		if err != nil {
			return
		}
	}
}
