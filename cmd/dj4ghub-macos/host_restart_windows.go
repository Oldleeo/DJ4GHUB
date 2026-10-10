//go:build windows

package main

import (
	"os"
	"sync"
	"time"
)

var disconnectRestartOnce sync.Once

// The DJI modem re-enumerates its Windows COM interface after CFUN=1,1. The
// desktop shell watches this dedicated exit code and starts a fresh backend,
// which re-discovers the COM port and rebuilds all modem/eSIM managers.
func scheduleBackendRestartAfterESIMSwitch(delay time.Duration) bool {
	go func() {
		time.Sleep(delay)
		os.Exit(42)
	}()
	return true
}

func scheduleBackendRestartAfterDisconnect(delay time.Duration) bool {
	disconnectRestartOnce.Do(func() {
		go func() {
			time.Sleep(delay)
			os.Exit(43)
		}()
	})
	return true
}
