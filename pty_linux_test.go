package main

import (
	"fmt"
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
	var unlock, number uint32
	if err = terminalIOCTL(fd, syscall.TIOCSPTLCK, unsafe.Pointer(&unlock)); err == nil {
		err = terminalIOCTL(fd, syscall.TIOCGPTN, unsafe.Pointer(&number))
	}
	if err != nil {
		master.Close()
		return nil, nil, err
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR, 0)
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
