package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHealthSnapshotReportsOperationalLayers(t *testing.T) {
	a := &app{demo: true, port: "demo", smsPollInterval: 8 * time.Second}
	snapshot := a.healthSnapshot()
	if snapshot["version"] != appVersion || snapshot["overall_state"] != "ready" {
		t.Fatalf("unexpected health snapshot: %#v", snapshot)
	}
	if snapshot["at_ready"] != true || snapshot["sim_ready"] != true || snapshot["network_registered"] != true {
		t.Fatalf("demo layers were not ready: %#v", snapshot)
	}
}

func TestDiagnosticSnapshotDoesNotExposeRelaySecrets(t *testing.T) {
	a := &app{
		demo:            true,
		port:            "demo",
		smsPollInterval: 8 * time.Second,
		remote: &remoteRelayManager{config: remoteRelayConfig{
			HostEnabled: true,
			HostToken:   "host-secret",
			OwnerToken:  "owner-secret",
			ClientToken: "client-secret",
		}},
	}
	raw, err := json.Marshal(a.diagnosticSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, secret := range []string{"host-secret", "owner-secret", "client-secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("diagnostic report exposed %q: %s", secret, text)
		}
	}
}

func TestExportSMSWritesExcelCompatibleCSV(t *testing.T) {
	a := &app{sms: []receivedSMS{{Sender: "10086", Content: "验证码 1234", Code: "1234", Timestamp: time.Date(2026, 9, 17, 10, 0, 0, 0, time.Local)}}}
	w := httptest.NewRecorder()
	a.exportSMS(w, httptest.NewRequest("GET", "/api/sms/export", nil))
	if !strings.Contains(w.Header().Get("Content-Disposition"), "DJ4GHUB-sms-") {
		t.Fatalf("missing download filename: %q", w.Header().Get("Content-Disposition"))
	}
	body := w.Body.Bytes()
	if len(body) < 3 || body[0] != 0xEF || body[1] != 0xBB || body[2] != 0xBF {
		t.Fatalf("CSV is missing UTF-8 BOM: %x", body[:min(3, len(body))])
	}
	if !strings.Contains(string(body), "验证码 1234") {
		t.Fatalf("CSV content missing: %s", body)
	}
}
