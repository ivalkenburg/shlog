package main

import "syscall"

const (
	terminalGetState = syscall.TIOCGETA
	terminalSetState = syscall.TIOCSETA
)
