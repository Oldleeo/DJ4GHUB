//go:build !windows

package modem

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/WongLoki/DJ4Hub/pkg/logger"
)

// forceReleasePort checks a Unix device file with fuser and terminates only
// external processes. Windows serial ports are handled in the Windows stub.
func (m *Manager) forceReleasePort(portPath string) {
	if _, err := os.Stat(portPath); err != nil {
		return
	}

	out, _ := exec.Command("fuser", portPath).CombinedOutput()
	if len(out) == 0 {
		return
	}

	occupiedPIDs := parseFuserPIDs(string(out))
	if len(occupiedPIDs) == 0 {
		return
	}

	selfTaskPIDs := currentProcessTaskPIDSet()
	released := make([]int, 0, len(occupiedPIDs))
	skipped := make([]int, 0, len(occupiedPIDs))

	for _, pid := range occupiedPIDs {
		if pid <= 2 {
			skipped = append(skipped, pid)
			continue
		}
		if _, isSelf := selfTaskPIDs[pid]; isSelf {
			skipped = append(skipped, pid)
			continue
		}
		if err := syscall.Kill(pid, syscall.SIGTERM); err == nil {
			released = append(released, pid)
		}
	}

	if len(skipped) > 0 {
		logger.Warn(fmt.Sprintf("[%s] 端口被当前进程占用，跳过自杀式释放", m.cfg.ID), "port", portPath, "self_pids", skipped)
	}
	if len(released) > 0 {
		logger.Warn(fmt.Sprintf("[%s] 检测到端口被外部进程占用，正在强制释放", m.cfg.ID), "port", portPath, "pids", released)
		time.Sleep(200 * time.Millisecond)
	}
}

func currentProcessTaskPIDSet() map[int]struct{} {
	out := map[int]struct{}{os.Getpid(): {}}
	entries, err := os.ReadDir("/proc/self/task")
	if err != nil {
		return out
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(strings.TrimSpace(entry.Name()))
		if err == nil && pid > 0 {
			out[pid] = struct{}{}
		}
	}
	return out
}
