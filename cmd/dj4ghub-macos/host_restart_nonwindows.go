//go:build !windows

package main

import "time"

func scheduleBackendRestartAfterESIMSwitch(time.Duration) bool { return false }

func scheduleBackendRestartAfterDisconnect(time.Duration) bool { return false }
