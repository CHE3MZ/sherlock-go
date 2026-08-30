//go:build windows

package cli

import (
	"os"
	"syscall"
	"unsafe"
)

// enableVirtualTerminal enables ANSI escape processing on legacy Windows
// console hosts.
func enableVirtualTerminal() {
	const enableVirtualTerminalProcessing = 0x0004
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getConsoleMode := kernel32.NewProc("GetConsoleMode")
	setConsoleMode := kernel32.NewProc("SetConsoleMode")

	h := syscall.Handle(os.Stdout.Fd())
	var mode uint32
	r, _, _ := getConsoleMode.Call(uintptr(h), uintptr(unsafe.Pointer(&mode)))
	if r == 0 {
		return
	}
	setConsoleMode.Call(uintptr(h), uintptr(mode|enableVirtualTerminalProcessing))
}
