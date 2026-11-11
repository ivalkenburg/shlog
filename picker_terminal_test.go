//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

type terminalRecording struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	notify chan struct{}
}

func (r *terminalRecording) Write(p []byte) (int, error) {
	r.mu.Lock()
	n, err := r.buf.Write(p)
	r.mu.Unlock()
	select {
	case r.notify <- struct{}{}:
	default:
	}
	return n, err
}

func (r *terminalRecording) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.String()
}

func (r *terminalRecording) length() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.Len()
}

type pickerCheckpoint struct{ terminal, stdout int }

func (p *pickerPTY) checkpoint() pickerCheckpoint {
	return pickerCheckpoint{p.terminal.length(), p.stdout.length()}
}

type pickerPTY struct {
	t             *testing.T
	master, slave *os.File
	masterFD      int
	slaveFD       int
	cmd           *exec.Cmd
	terminal      terminalRecording
	stdout        terminalRecording
	done          chan struct{}
	err           error
	before        *terminalState
}

func startPickerPTY(t *testing.T, args ...string) *pickerPTY {
	t.Helper()
	return startPTYCommand(t, binaryPath, args...)
}

func startPTYCommand(t *testing.T, binary string, args ...string) *pickerPTY {
	t.Helper()
	return startPTYCommandWithOutput(t, nil, binary, args...)
}

func startPTYCommandWithOutput(t *testing.T, output io.Writer, binary string, args ...string) *pickerPTY {
	t.Helper()
	master, slave, err := openTestPTY()
	if err != nil {
		t.Fatal(err)
	}
	p := &pickerPTY{t: t, master: master, slave: slave, masterFD: int(master.Fd()), slaveFD: int(slave.Fd()), done: make(chan struct{})}
	if err := syscall.SetNonblock(p.masterFD, true); err != nil {
		t.Fatal(err)
	}
	p.terminal.notify, p.stdout.notify = make(chan struct{}, 1), make(chan struct{}, 1)
	t.Cleanup(func() { master.Close(); slave.Close() })
	p.before, err = readTerminalState(p.slaveFD)
	if err != nil {
		t.Fatal(err)
	}
	p.resize(80, 24)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	p.cmd = exec.CommandContext(ctx, binary, args...)
	p.cmd.Env = append(os.Environ(), "TERM=xterm-256color", "NO_COLOR=1", "PATH=/nonexistent", "PS1=shlog-test> ")
	if output == nil {
		output = &p.stdout
	}
	p.cmd.Stdin, p.cmd.Stdout, p.cmd.Stderr = slave, output, slave
	p.cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		p.err = p.cmd.Wait()
		close(p.done)
	}()
	t.Cleanup(func() { cancel(); <-p.done })
	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := master.Read(buf)
			if n > 0 {
				p.terminal.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	return p
}

func (p *pickerPTY) resize(width, height int) {
	p.t.Helper()
	size := struct{ rows, cols, x, y uint16 }{uint16(height), uint16(width), 0, 0}
	if err := terminalIOCTL(p.slaveFD, syscall.TIOCSWINSZ, unsafe.Pointer(&size)); err != nil {
		p.t.Fatal(err)
	}
}

func (p *pickerPTY) send(s string) {
	p.t.Helper()
	if _, err := p.master.Write([]byte(s)); err != nil {
		p.t.Fatal(err)
	}
}

func (p *pickerPTY) waitText(text string) {
	p.t.Helper()
	p.waitTextAfter(text, pickerCheckpoint{})
}

func (p *pickerPTY) waitTextAfter(text string, after pickerCheckpoint) {
	p.t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for !strings.Contains(p.terminal.String()[after.terminal:]+p.stdout.String()[after.stdout:], text) {
		select {
		case <-p.terminal.notify:
		case <-p.stdout.notify:
		case <-timer.C:
			p.t.Fatalf("waiting for %q\nterminal: %q\nstdout: %q", text, p.terminal.String(), p.stdout.String())
		}
	}
}

func (p *pickerPTY) waitForTerminalCleanup() {
	p.t.Helper()
	entered := strings.LastIndex(p.terminal.String(), "\x1b[?1049h")
	if entered < 0 {
		p.t.Fatal("picker never entered the alternate screen")
	}
	p.waitTextAfter("\x1b[?1049l", pickerCheckpoint{terminal: entered})
	recording := p.terminal.String()
	for _, mode := range []struct {
		name, on, off string
		wantOn        bool
	}{
		{"alternate screen", "\x1b[?1049h", "\x1b[?1049l", false},
		{"mouse tracking", "\x1b[?1000h", "\x1b[?1000l", false},
		{"mouse dragging", "\x1b[?1002h", "\x1b[?1002l", false},
		{"mouse coordinates", "\x1b[?1006h", "\x1b[?1006l", false},
		{"bracketed paste", "\x1b[?2004h", "\x1b[?2004l", false},
		{"visible cursor", "\x1b[?25h", "\x1b[?25l", true},
	} {
		on, off := strings.LastIndex(recording, mode.on), strings.LastIndex(recording, mode.off)
		if on == -1 || off == -1 || (on > off) != mode.wantOn {
			p.t.Errorf("%s was not restored after picker exit", mode.name)
		}
	}
}

func (p *pickerPTY) wait() (string, error) {
	p.t.Helper()
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		p.t.Fatalf("picker did not exit\n%s", p.terminal.String())
	}
	p.waitForTerminalCleanup()
	// macOS revokes the slave when its controlling session exits; the master
	// still exposes the restored settings after that hangup.
	after, err := readTerminalState(p.masterFD)
	if err != nil || !reflect.DeepEqual(after, p.before) {
		p.t.Errorf("terminal mode was not restored: %v\nbefore=%+v\nafter=%+v", err, p.before, after)
	}
	if strings.Contains(p.terminal.String(), "\x1b[36m") {
		p.t.Error("picker ignored NO_COLOR")
	}
	return p.stdout.String(), p.err
}

func TestPickPrintsFullCommandAndDelPickDeletesWholeEntry(t *testing.T) {
	original := ": 1000:0;keep\n: 1001:0;first line\\\nsecond line\n"
	hist := writeTempHistory(t, original)
	p := startPickerPTY(t, "--histfile", hist, "pick", "-1")
	p.waitText("shlog pick")
	p.send("\r")
	if out, err := p.wait(); err != nil || out != "first line\nsecond line\n" {
		t.Fatalf("pick output = %q, err=%v", out, err)
	}
	assertFileContent(t, hist, original)
	assertNoBackup(t, hist)
	p = startPickerPTY(t, "--histfile", hist, "-f", "del", "--pick", "-1")
	p.waitText("shlog del --pick")
	p.send("\r")
	if out, err := p.wait(); err != nil {
		t.Fatalf("del --pick: %v\n%s", err, out)
	}
	assertFileContent(t, hist, ": 1000:0;keep\n")
	assertFileContent(t, hist+".bak", original)
}

func TestPickerCancellation(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		key  string
		want string
	}{
		{"escape", []string{"pick"}, "\x1b", ""},
		{"ctrl-c", []string{"pick"}, "\x03", ""},
		{"delete", []string{"del", "--pick"}, "\x1b", ""},
		{"output", []string{"-o", "-s", "del", "--pick"}, "\x03", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			hist := writeTempHistory(t, singleLineHistory)
			p := startPickerPTY(t, append([]string{"--histfile", hist}, tt.args...)...)
			p.waitText("\x1b[?2004h")
			p.send(tt.key)
			out, err := p.wait()
			assertExitStatus(t, err, exitCanceled)
			if out != tt.want {
				t.Fatalf("cancelled pick = %q, want %q", out, tt.want)
			}
			assertFileContent(t, hist, singleLineHistory)
			assertNoBackup(t, hist)
		})
	}
}

func TestPickDecodesZshCommand(t *testing.T) {
	original := ": 1000:0;for x in \xe2\x80\x83\xb4\\\ndo echo $x\\\ndone\n"
	hist := writeTempHistory(t, original)
	p := startPickerPTY(t, "--histfile", hist, "pick")
	p.waitText("shlog pick")
	p.send("\r")
	if out, err := p.wait(); err != nil || out != "for x in —\ndo echo $x\ndone\n" {
		t.Fatalf("decoded pick = %q, %v", out, err)
	}
	assertFileContent(t, hist, original)
}

func TestPickerMouseHiddenMarksAndOutputOrder(t *testing.T) {
	hist := writeTempHistory(t, "outside\nold command\nnew command\n")
	p := startPickerPTY(t, "--histfile", hist, "pick", "--multi", "-2")
	p.waitText("shlog pick")
	p.send("\x1b[<0;3;5M") // Mark old first.
	p.waitText("1 selected")
	p.send("\x1b[<0;3;4M") // Mark new second.
	p.waitText("2 selected")
	p.send("no-such-command")
	p.waitText("No matching commands")
	p.send("\r")
	if out, err := p.wait(); err != nil || out != "new command\nold command\n" {
		t.Fatalf("hidden mouse selections = %q, %v", out, err)
	}
	assertNoBackup(t, hist)
}

func TestPickerWheelAndDoubleClick(t *testing.T) {
	var history strings.Builder
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&history, "echo %d\n", i)
	}
	hist := writeTempHistory(t, history.String())
	p := startPickerPTY(t, "--histfile", hist, "pick")
	p.waitText("shlog pick")
	p.send("\x1b[<65;10;4M")
	p.waitText("#27 ·")
	p.send("\r")
	if out, err := p.wait(); err != nil || out != "echo 27\n" {
		t.Fatalf("wheel selection = %q, %v", out, err)
	}
	p = startPickerPTY(t, "--histfile", hist, "pick")
	p.waitText("shlog pick")
	p.send("\x1b[<0;10;5M\x1b[<0;10;5m\x1b[<0;10;5M")
	if out, err := p.wait(); err != nil || out != "echo 29\n" {
		t.Fatalf("double-click selection = %q, %v", out, err)
	}
}

func TestPickerScrollbarMouseInput(t *testing.T) {
	var history strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&history, "echo %d\n", i)
	}
	hist := writeTempHistory(t, history.String())
	for _, tt := range []struct {
		name, input string
		command     int
	}{
		{"thumb drag", "\x1b[<0;79;4M\x1b[<32;1;24M", 11},
		{"rail jump", "\x1b[<0;79;9M", 55},
		{"rail then drag", "\x1b[<0;79;9M\x1b[<32;1;24M", 11},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := startPickerPTY(t, "--histfile", hist, "pick")
			p.waitText("\x1b[2;3H\x1b[?25h")
			p.send(tt.input)
			p.waitText(fmt.Sprintf("#%d ·", tt.command))
			// Release outside the panel; later motion must not change selection.
			p.send("\x1b[<0;1;1m\x1b[<32;79;4M\r")
			if out, err := p.wait(); err != nil || out != fmt.Sprintf("echo %d\n", tt.command) {
				t.Fatalf("scrollbar selection = %q, %v", out, err)
			}
		})
	}
	assertNoBackup(t, hist)
}

func TestPickerSearchPasteAndHelp(t *testing.T) {
	hist := writeTempHistory(t, "git checkout main\necho café\nsudo git status\n")
	p := startPickerPTY(t, "--histfile", hist, "pick")
	p.waitText("shlog pick")
	p.send("\x1bOP")
	p.waitText("picker reference")
	p.send("\x1b")
	p.waitText("F1 help")
	p.send("\x1b[200~git\n!sudo\x1b[201~")
	p.waitText("1 / 3")
	p.send("\r")
	if out, err := p.wait(); err != nil || out != "git checkout main\n" {
		t.Fatalf("extended pasted search = %q, %v", out, err)
	}
}

func TestPickerIncompleteQueryBlocksAcceptance(t *testing.T) {
	hist := writeTempHistory(t, "old\ntarget\nnew\n")
	p := startPickerPTY(t, "--histfile", hist, "pick", "--multi")
	p.waitText("shlog pick")
	p.send("\tnew |")
	p.waitText("add a search term after |")
	p.send("\r\x15target")
	p.waitText("1 / 3")
	p.send("\t\r")
	if out, err := p.wait(); err != nil || out != "new\ntarget\n" {
		t.Fatalf("query validation or selection retention failed: %q, %v", out, err)
	}
}

func TestPickerRestoresBeforeDeletionConfirmation(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(fmt.Sprint(conflict), func(t *testing.T) {
			original := "keep\ndelete\n"
			hist := writeTempHistory(t, original)
			p := startPickerPTY(t, "--histfile", hist, "del", "--pick", "-1")
			p.waitText("shlog del --pick")
			p.send("\r")
			p.waitText("Confirm? [y/N]")
			p.waitForTerminalCleanup()
			state, err := readTerminalState(p.slaveFD)
			if err != nil || !reflect.DeepEqual(state, p.before) {
				t.Fatalf("confirmation received a raw terminal: %v", err)
			}
			if conflict {
				if err := os.WriteFile(hist, []byte(original+"appended\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			p.send("yes\n")
			out, err := p.wait()
			if conflict {
				if err == nil {
					t.Fatal("concurrent history update was overwritten")
				}
				assertFileContent(t, hist, original+"appended\n")
				assertNoBackup(t, hist)
			} else {
				if err != nil || !strings.Contains(out, "Deleted 1 entries") {
					t.Fatalf("confirmation input was consumed by picker: %q, %v", out, err)
				}
				assertFileContent(t, hist, "keep\n")
				assertFileContent(t, hist+".bak", original)
			}
		})
	}
}

func TestPickerResizeAndSignalCleanup(t *testing.T) {
	hist := writeTempHistory(t, "echo hello\n")
	p := startPickerPTY(t, "--histfile", hist, "pick")
	p.waitText("\x1b[2;3H\x1b[?25h") // Wait for the complete initial frame.
	after := p.checkpoint()
	p.resize(50, 14)
	if err := p.cmd.Process.Signal(syscall.SIGWINCH); err != nil {
		t.Fatal(err)
	}
	p.waitTextAfter("\x1b[14;1H", after)
	resized := p.terminal.String()[after.terminal:]
	if !strings.Contains(resized, "\x1b[14;1H\x1b[2K↑↓ move") || strings.Contains(resized, "\x1b[24;1H") {
		t.Fatalf("resize did not redraw the footer at the new height: %q", resized)
	}
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if out, err := p.wait(); err == nil || out != "" {
		t.Fatalf("SIGTERM = %q, %v", out, err)
	}
	assertNoBackup(t, hist)
}

func TestPickerCommandSubstitution(t *testing.T) {
	hist := writeTempHistory(t, "old\necho chosen\n")
	p := startPTYCommand(t, "/bin/sh", "-c", `result="$("$1" --histfile "$2" pick)"; printf '<%s>' "$result"`, "shlog-test", binaryPath, hist)
	p.waitText("shlog pick")
	p.send("\r")
	if out, err := p.wait(); err != nil || out != "<echo chosen>" {
		t.Fatalf("command substitution contaminated stdout: %q, %v", out, err)
	}
}

func TestPickerCommandSubstitutionDoesNotSuspendParent(t *testing.T) {
	hist := writeTempHistory(t, "old\necho chosen\n")
	p := startPTYCommand(t, "/bin/sh", "-c", `result="$("$1" --histfile "$2" pick)"; printf '<%s>' "$result"`, "shlog-test", binaryPath, hist)
	p.waitText("shlog pick")
	p.send("\x1achosen")
	p.waitText("1 / 2")
	if strings.Contains(p.terminal.String(), "\x1b[?1049l") {
		t.Fatal("unsupported suspension left the picker and stranded its parent shell")
	}
	p.send("\r")
	if out, err := p.wait(); err != nil || out != "<echo chosen>" {
		t.Fatalf("Ctrl-Z in command substitution = %q, %v", out, err)
	}
}

func TestPickerWithoutControllingTerminal(t *testing.T) {
	hist := writeTempHistory(t, "echo hello\n")
	cmd := exec.Command(binaryPath, "--histfile", hist, "pick")
	cmd.Env = append(os.Environ(), "TERM=xterm")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "requires a controlling terminal") {
		t.Fatalf("noninteractive picker = %q, %v", out, err)
	}
}

func TestPickerKeepsRedirectedConfirmationInput(t *testing.T) {
	hist := writeTempHistory(t, "keep\ndelete\n")
	p := startPTYCommand(t, "/bin/sh", "-c", `printf 'yes\n' | "$1" --histfile "$2" del --pick -1`, "shlog-test", binaryPath, hist)
	p.waitText("shlog del --pick")
	p.send("\r")
	if out, err := p.wait(); err != nil || !strings.Contains(out, "Deleted 1 entries") {
		t.Fatalf("picker consumed redirected confirmation input: %q, %v", out, err)
	}
	assertFileContent(t, hist, "keep\n")
	assertFileContent(t, hist+".bak", "keep\ndelete\n")
}

func TestPickerEscapeThenTypingLeavesHelp(t *testing.T) {
	hist := writeTempHistory(t, "echo café\ngit status\n")
	p := startPickerPTY(t, "--histfile", hist, "pick")
	p.waitText("shlog pick")
	p.send("\x1bOP")
	p.waitText("picker reference")
	p.send("\x1bcafe") // Escape and the next query can arrive in one read.
	p.waitText("1 / 2")
	p.send("\r")
	if out, err := p.wait(); err != nil || out != "echo café\n" {
		t.Fatalf("Escape swallowed subsequent typing: %q, %v", out, err)
	}
}

func TestPickerSuspendAndResume(t *testing.T) {
	for _, tt := range []struct{ name, suffix string }{
		{"standalone", ""}, {"pipeline", " | /bin/cat"},
	} {
		t.Run(tt.name, func(t *testing.T) { testPickerSuspendAndResume(t, tt.suffix) })
	}
}

func testPickerSuspendAndResume(t *testing.T, suffix string) {
	t.Helper()
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash is not installed")
	}
	hist := writeTempHistory(t, "echo café\ngit status\n")
	p := startPTYCommand(t, "/bin/bash", "--noprofile", "--norc", "-i")
	const prompt = "shlog-test> "
	p.waitText(prompt)
	after := p.checkpoint()
	p.send("set +o emacs; set +o vi\n")
	p.waitTextAfter(prompt, after)
	shellState, err := readTerminalState(p.slaveFD)
	if err != nil {
		t.Fatal(err)
	}
	// Keep checkout and temporary paths with spaces intact in the shell.
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	after = p.checkpoint()
	p.send(quote(binaryPath) + " --histfile " + quote(hist) + " pick" + suffix + "\n")
	p.waitTextAfter("shlog pick", after)
	p.send("cafe")
	p.waitText("1 / 2")
	after = p.checkpoint()
	p.send("\x1a")
	p.waitTextAfter(prompt, after)
	state, err := readTerminalState(p.slaveFD)
	if err != nil || !reflect.DeepEqual(state, shellState) {
		t.Fatalf("suspend did not restore the shell terminal: %v", err)
	}
	suspended := p.terminal.String()[after.terminal:]
	if strings.Count(suspended, "\x1b[?1049l") != 1 || strings.Contains(suspended, "\x1b[?1049h") {
		t.Fatalf("picker did not remain suspended: %q", suspended)
	}
	if !strings.Contains(suspended, "\x1b[?1002l") {
		t.Fatal("suspend did not disable mouse dragging")
	}
	// A shell command proves that the stopped picker is no longer reading
	// terminal input; a termios-only assertion cannot catch a surviving reader.
	after = p.checkpoint()
	p.send("printf 'SHELL-READY\\n'\n")
	p.waitTextAfter("SHELL-READY\n", after)
	after = p.checkpoint()
	p.send("fg\n")
	p.waitTextAfter("\x1b[?1049h", after)
	p.waitTextAfter("\x1b[?1002h", after)
	p.waitTextAfter("cafe", after)
	state, err = readTerminalState(p.slaveFD)
	if err != nil || state.Lflag&(syscall.ICANON|syscall.ECHO) != 0 {
		t.Fatalf("resume did not restore picker raw mode: %v", err)
	}
	after = p.checkpoint()
	p.send("\r")
	p.waitTextAfter(prompt, after)
	if !strings.Contains(p.stdout.String(), "echo café\n") {
		t.Fatalf("resumed picker lost the query/selection: %q", p.stdout.String())
	}
	p.send("exit\n")
	if _, err := p.wait(); err != nil {
		t.Fatalf("interactive shell did not exit cleanly: %v", err)
	}
	assertFileContent(t, hist, "echo café\ngit status\n")
	assertNoBackup(t, hist)
}

func TestPickerCancelFromHelpRestoresTerminal(t *testing.T) {
	hist := writeTempHistory(t, "echo hello\n")
	p := startPickerPTY(t, "--histfile", hist, "pick")
	p.waitText("shlog pick")
	p.send("\x1bOP")
	p.waitText("picker reference")
	p.send("\x03")
	out, err := p.wait()
	assertExitStatus(t, err, exitCanceled)
	if out != "" {
		t.Fatalf("cancel from help = %q", out)
	}
}

func TestPickerAcceptsQueryAndEnterTogether(t *testing.T) {
	hist := writeTempHistory(t, "echo cafe\ngit status\n")
	p := startPickerPTY(t, "--histfile", hist, "pick")
	p.waitText("shlog pick")
	p.send("cafe\r")
	if out, err := p.wait(); err != nil || out != "echo cafe\n" {
		t.Fatalf("Enter during filtering was lost: %q, %v", out, err)
	}
	assertNoBackup(t, hist)
}

func TestPickerReportsOutputFailure(t *testing.T) {
	hist := writeTempHistory(t, "echo selected\n")
	readOnly, err := os.Open(hist)
	if err != nil {
		t.Fatal(err)
	}
	defer readOnly.Close()
	p := startPTYCommandWithOutput(t, readOnly, binaryPath, "--histfile", hist, "pick")
	p.waitText("shlog pick")
	p.send("\r")
	if _, err := p.wait(); err == nil {
		t.Fatal("pick reported success after its output write failed")
	}
	p.waitText("error writing output")
	assertFileContent(t, hist, "echo selected\n")
	assertNoBackup(t, hist)
}

func TestPickerUniqueAndSortControls(t *testing.T) {
	hist := writeTempHistory(t, "git checkout\ngit checkout\ngiant careful operation\npwd\n")
	p := startPickerPTY(t, "pick", "--unique", "--histfile", hist)
	p.waitText("[ newest first ]")
	p.waitText("3 / 3")
	p.send("gco")
	p.waitText("2 / 3")
	sorted := p.checkpoint()
	p.send("\x13")
	p.waitText("[ relevance ]")
	p.waitTextAfter("#3 ·", sorted)
	p.send("\x1b[A\r") // Focus was retained on the newer, weaker match.
	if out, err := p.wait(); err != nil || out != "git checkout\n" {
		t.Fatalf("relevance sorting = %q, %v", out, err)
	}
	p = startPickerPTY(t, "pick", "--unique", "--histfile", hist)
	p.waitText("[ newest first ]")
	after := p.checkpoint()
	p.send("\x1b[<0;70;1M")
	p.waitText("[ relevance ]")
	p.waitTextAfter("#4 ·", after)
	after = p.checkpoint()
	p.send("\x1b[<0;70;1M")
	p.waitText("[ most used first ]")
	p.waitTextAfter("#4 ·", after)
	p.send("\x1b[A\r") // Retained pwd focus moves up to the most-used command.
	if out, err := p.wait(); err != nil || out != "git checkout\n" {
		t.Fatalf("clickable sorting = %q, %v", out, err)
	}
	p = startPickerPTY(t, "pick", "--unique", "--sort=frequency", "--histfile", hist)
	p.waitText("[ most used first ]")
	p.send("\r")
	if out, err := p.wait(); err != nil || out != "git checkout\n" {
		t.Fatalf("initial frequency sort = %q, %v", out, err)
	}
	assertFileContent(t, hist, "git checkout\ngit checkout\ngiant careful operation\npwd\n")
	assertNoBackup(t, hist)
}

func TestDeletionFrequencySortDeletesGroupedOccurrences(t *testing.T) {
	hist := writeTempHistory(t, "git\ngit\npwd\n")
	p := startPickerPTY(t, "del", "--pick", "--sort", "frequency", "-f", "--histfile", hist)
	p.waitText("[ most used first ]")
	p.waitText("2 / 2")
	p.send("\r")
	out, err := p.wait()
	assertExitStatus(t, err, 0)
	if !strings.Contains(out, "Deleted 2 entries") {
		t.Fatalf("unexpected deletion: %q", out)
	}
	assertFileContent(t, hist, "pwd\n")
	assertFileContent(t, hist+".bak", "git\ngit\npwd\n")
}

func TestDeletionFrequencySortConfirmsScopedEntryCount(t *testing.T) {
	original := "git\ngit\ngit\npwd\n"
	hist := writeTempHistory(t, original)
	p := startPickerPTY(t, "del", "--pick", "--sort", "frequency", "-3", "--histfile", hist)
	p.waitText("2 / 2")
	p.send("\r")
	p.waitText("About to delete 2 of 4 entries. Confirm? [y/N]")
	p.waitForTerminalCleanup()
	assertFileContent(t, hist, original)
	assertNoBackup(t, hist)
	p.send("y\n")
	if out, err := p.wait(); err != nil || !strings.Contains(out, "Deleted 2 entries") {
		t.Fatalf("grouped deletion = %q, %v", out, err)
	}
	assertFileContent(t, hist, "git\npwd\n") // The out-of-scope git survives.
	assertFileContent(t, hist+".bak", original)
}

func TestFrequencyMultiOutputAndSortRestoration(t *testing.T) {
	hist := writeTempHistory(t, "git\ngit\npwd\n")
	for _, newest := range []bool{false, true} {
		t.Run(fmt.Sprint(newest), func(t *testing.T) {
			p := startPickerPTY(t, "pick", "--multi", "--sort", "frequency", "--histfile", hist)
			p.waitText("2 / 2")
			p.send("\t") // Mark both git occurrences through their grouped row.
			p.waitText("1 command · 2 entries selected")
			want := "git\n"
			if newest {
				p.send("\x13")
				p.waitText("[ newest first ]")
				p.waitText("3 / 3")
				p.waitText("2 selected")
				want = "git\ngit\n"
			}
			p.send("\r")
			if out, err := p.wait(); err != nil || out != want {
				t.Fatalf("multi output = %q, %v; want %q", out, err, want)
			}
		})
	}
	assertFileContent(t, hist, "git\ngit\npwd\n")
	assertNoBackup(t, hist)
}

func TestPickerSIGINTCancellationStatus(t *testing.T) {
	hist := writeTempHistory(t, "git\n")
	p := startPickerPTY(t, "pick", "--histfile", hist)
	p.waitText("[ newest first ]")
	if err := p.cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	out, err := p.wait()
	assertExitStatus(t, err, exitCanceled)
	if out != "" {
		t.Fatalf("cancel produced output: %q", out)
	}
	assertNoBackup(t, hist)
}
