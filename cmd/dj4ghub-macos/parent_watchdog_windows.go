//go:build windows

package main

import (
	"log"
	"os"

	"golang.org/x/sys/windows"
)

// Holding the process handle also prevents PID reuse from hiding a dead parent.
func watchDesktopParent(pid int) {
	if pid <= 0 {
		return
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		log.Fatal("desktop parent is unavailable: ", err)
	}
	go func() {
		defer windows.CloseHandle(handle)
		result, err := windows.WaitForSingleObject(handle, windows.INFINITE)
		if err == nil && result == windows.WAIT_OBJECT_0 {
			os.Exit(44)
		}
	}()
}
