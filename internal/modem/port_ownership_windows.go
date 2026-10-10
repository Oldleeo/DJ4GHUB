//go:build windows

package modem

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sys/windows"
)

// A crash-safe, cross-process guard shared by hosts and discovery probes.
// Windows still exclusively owns the actual serial handle; we never evict
// another program or force-close its port.
func acquireATPortOwnership(port string) (func(), error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	root = filepath.Join(root, "DJ4Hub", "PortLocks")
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(strings.ToUpper(strings.TrimSpace(port))))
	path, err := windows.UTF16PtrFromString(filepath.Join(root, fmt.Sprintf("%x.lock", sum[:16])))
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(path, windows.GENERIC_READ|windows.GENERIC_WRITE,
		0, nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, fmt.Errorf("AT 端口 %s 已被其他设备主机占用或锁不可用: %w", port, err)
	}
	var once sync.Once
	return func() { once.Do(func() { _ = windows.CloseHandle(handle) }) }, nil
}
