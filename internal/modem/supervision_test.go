package modem

import (
	"testing"
	"time"

	"github.com/WongLoki/DJ4Hub/internal/config"
)

func TestExpectedDeviceKeyNeverMatchesEmptyOrOtherIdentity(t *testing.T) {
	if matchesExpectedDeviceKey("", "") || matchesExpectedDeviceKey("1234", "wrong") {
		t.Fatal("identity guard accepted an unknown device")
	}
}

func TestHealthProbeSkipsValidLongCommandButDetectsStalledWrite(t *testing.T) {
	m, err := New(config.DeviceConfig{ID: "test", ATPort: "test-port"})
	if err != nil {
		t.Fatal(err)
	}
	m.port = &timeoutSerialPort{}
	m.running = true
	m.commandDeadline = time.Now().Add(60 * time.Second)
	if err := m.ProbeATHealth(); err != nil {
		t.Fatal("normal long command interrupted: ", err)
	}
	m.commandDeadline = time.Now().Add(-time.Second)
	if err := m.ProbeATHealth(); err == nil {
		t.Fatal("stalled write reported healthy")
	}
	m.running = false
	if err := m.ProbeATHealth(); err == nil {
		t.Fatal("stopped manager reported healthy")
	}
}
