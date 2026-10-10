//go:build windows

package modem

import "os"

// Windows does not provide fuser semantics. Let the serial package return a
// normal access error instead of terminating unrelated processes.
func (m *Manager) forceReleasePort(string) {}

func currentProcessTaskPIDSet() map[int]struct{} {
	return map[int]struct{}{os.Getpid(): {}}
}
