package main

import "syscall"

const (
	terminalGetState = syscall.TCGETS
	terminalSetState = syscall.TCSETS
)
