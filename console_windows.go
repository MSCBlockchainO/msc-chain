//go:build windows

package main

import "syscall"

// enableUTF8Console implements the enable utf8 console helper.
func enableUTF8Console() {
	// `kernel32` stores the value produced by this operation.
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	// `setOut` stores the result produced by this operation.
	setOut := kernel32.NewProc("SetConsoleOutputCP")
	// `setIn` stores the value produced by this operation.
	setIn := kernel32.NewProc("SetConsoleCP")
	// `utf8CodePage` defines the constant value used by this package.
	const utf8CodePage = 65001

	_, _, _ = setOut.Call(uintptr(utf8CodePage))
	_, _, _ = setIn.Call(uintptr(utf8CodePage))
}
