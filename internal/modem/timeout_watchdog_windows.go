//go:build windows

package modem

// The desktop supervisor now continuously recreates failed hosts; retaining
// a dead handle would prevent it from recovering the actual control channel.
func keepATManagerAliveAfterTimeoutThreshold() bool { return false }
