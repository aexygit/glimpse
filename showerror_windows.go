package main

import (
	"syscall"
	"unsafe"
)

// showError pops a native message box. user32.dll is a KnownDLL, so the bare
// name always resolves to the copy in the system directory.
func showError(title, msg string) {
	t, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	m, err := syscall.UTF16PtrFromString(msg)
	if err != nil {
		return
	}
	proc := syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")
	if proc.Find() != nil {
		return
	}
	const mbIconError = 0x10
	proc.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), mbIconError)
}
