//go:build !windows

package modem

func keepATManagerAliveAfterTimeoutThreshold() bool { return false }
