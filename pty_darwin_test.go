package main

import (
	"bytes"
	"os"
	"syscall"
	"unsafe"
)

func openTestPTY() (*os.File, *os.File, error) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, err
	}
	fd := int(master.Fd())
	var name [128]byte
	for _, request := range []uint{syscall.TIOCPTYGRANT, syscall.TIOCPTYUNLK, syscall.TIOCPTYGNAME} {
		if err = terminalIOCTL(fd, request, unsafe.Pointer(&name[0])); err != nil {
			master.Close()
			return nil, nil, err
		}
	}
	slave, err := os.OpenFile(string(bytes.TrimRight(name[:], "\x00")), os.O_RDWR, 0)
	if err != nil {
		master.Close()
		return nil, nil, err
	}
	if err := syscall.SetNonblock(fd, true); err != nil {
		master.Close()
		slave.Close()
		return nil, nil, err
	}
	return master, slave, nil
}
