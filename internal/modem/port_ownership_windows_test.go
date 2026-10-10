//go:build windows

package modem

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestPortOwnershipExclusiveAndIdempotentRelease(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	port := fmt.Sprintf("unit-test-%d-%d", os.Getpid(), time.Now().UnixNano())
	release, err := acquireATPortOwnership(port)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if second, err := acquireATPortOwnership(port); err == nil {
		second()
		t.Fatal("duplicate owner acquired port")
	}
	release()
	release()
	next, err := acquireATPortOwnership(port)
	if err != nil {
		t.Fatal("port did not unlock: ", err)
	}
	next()
}

func TestPortOwnershipReleasedAfterProcessCrash(t *testing.T) {
	if port := os.Getenv("DJ4HUB_LOCK_TEST_CHILD"); port != "" {
		release, err := acquireATPortOwnership(port)
		if err != nil {
			os.Exit(2)
		}
		defer release()
		fmt.Println("owned")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		return
	}
	t.Setenv("LOCALAPPDATA", t.TempDir())
	port := fmt.Sprintf("crash-test-%d-%d", os.Getpid(), time.Now().UnixNano())
	child := exec.Command(os.Args[0], "-test.run=^TestPortOwnershipReleasedAfterProcessCrash$")
	child.Env = append(os.Environ(), "DJ4HUB_LOCK_TEST_CHILD="+port)
	stdin, _ := child.StdinPipe()
	defer stdin.Close()
	stdout, _ := child.StdoutPipe()
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer child.Process.Kill()
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(stdout).ReadString('\n'); ready <- line }()
	select {
	case line := <-ready:
		if line != "owned\n" {
			t.Fatalf("child: %q", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("child lock timeout")
	}
	if release, err := acquireATPortOwnership(port); err == nil {
		release()
		t.Fatal("cross-process lock missing")
	}
	_ = child.Process.Kill()
	_ = child.Wait()
	release, err := acquireATPortOwnership(port)
	if err != nil {
		t.Fatal("crashed process retained ownership: ", err)
	}
	release()
}
