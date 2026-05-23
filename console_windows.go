//go:build windows

package main

import "syscall"

func enableUTF8Console() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	setOut := kernel32.NewProc("SetConsoleOutputCP")
	setIn := kernel32.NewProc("SetConsoleCP")
	const utf8CodePage = 65001

	_, _, _ = setOut.Call(uintptr(utf8CodePage))
	_, _, _ = setIn.Call(uintptr(utf8CodePage))
}
