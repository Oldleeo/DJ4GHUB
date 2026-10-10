package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRemoteRelayRequiresHTTPS(t *testing.T) {
	for _, value := range []string{"http://relay.example.com", "https://user@example.com", "https://example.com/?token=secret"} {
		if validateRelayURL(value) == nil {
			t.Fatalf("unsafe relay URL accepted: %s", value)
		}
	}
	if err := validateRelayURL("https://relay.example.com"); err != nil {
		t.Fatal(err)
	}
}

func TestPublicRelayDefaultsLocalOnly(t *testing.T) {
	m, err := newRemoteRelayManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if m.config.RelayURL != "" || m.config.HostEnabled {
		t.Fatal("public build must default to local-only")
	}
	instance := &app{remote: m}
	for _, enabled := range []bool{false, true} {
		body, _ := json.Marshal(map[string]any{"enabled": enabled, "relay_url": ""})
		response := httptest.NewRecorder()
		instance.updateRemoteHost(response, httptest.NewRequest(http.MethodPut, "/api/remote/host", bytes.NewReader(body)))
		want := http.StatusOK
		if enabled {
			want = http.StatusBadRequest
		}
		if response.Code != want {
			t.Fatalf("enabled=%v: status %d, want %d", enabled, response.Code, want)
		}
	}
}

func TestRemoteRelaySettingsRedactTokens(t *testing.T) {
	m, err := newRemoteRelayManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m.config.OwnerToken = "owner-secret"
	m.config.HostToken = "host-secret"
	m.config.ClientToken = "client-secret"
	encoded, err := json.Marshal(m.settings())
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"owner-secret", "host-secret", "client-secret"} {
		if bytes.Contains(encoded, []byte(secret)) {
			t.Fatalf("settings leaked %s", secret)
		}
	}
}

func TestRemoteRelayConfigIsPrivate(t *testing.T) {
	root := t.TempDir()
	m, err := newRemoteRelayManager(root)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(root, "DJ4Hub", "remote-relay.json"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("remote config mode is %o", info.Mode().Perm())
	}
	if m.config.HostID == "" {
		t.Fatal("host identifier was not generated")
	}
}

func TestDemoRemoteSnapshotUsesPhysicalModuleIdentity(t *testing.T) {
	app := newDemoApp()
	snapshot, deviceID, err := app.buildRemoteSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if deviceID != "imei:867400000000001" {
		t.Fatalf("unexpected device ID %q", deviceID)
	}
	if len(snapshot.SMS) != 2 {
		t.Fatalf("unexpected SMS count %d", len(snapshot.SMS))
	}
}

func TestDemoExecutesExpandedRemoteCommands(t *testing.T) {
	instance := newDemoApp()
	tests := []struct {
		name    string
		payload any
	}{
		{name: "refresh_status"},
		{name: "execute_at", payload: map[string]string{"command": "AT+CSQ"}},
		{name: "read_esim"},
		{name: "read_network"},
		{name: "save_apn", payload: map[string]string{"apn": "3gnet", "pdn": "IPV4V6"}},
	}
	for _, item := range tests {
		t.Run(item.name, func(t *testing.T) {
			payload, err := json.Marshal(item.payload)
			if err != nil {
				t.Fatal(err)
			}
			var result map[string]any
			if err := json.Unmarshal(instance.executeRemoteCommand(pendingRelayCommand{Name: item.name, Payload: payload}), &result); err != nil {
				t.Fatal(err)
			}
			if ok, _ := result["ok"].(bool); !ok {
				t.Fatalf("command failed: %#v", result)
			}
			if _, ok := result["data"]; !ok {
				t.Fatalf("command returned no data: %#v", result)
			}
		})
	}
}

func TestRemoteCommandsBlockDialAndSMSSendLocally(t *testing.T) {
	m, err := newRemoteRelayManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m.config.ClientToken = "configured-client-token"
	app := &app{remote: m}
	for _, name := range []string{"dial", "send_sms"} {
		body, _ := json.Marshal(map[string]string{"name": name})
		request := httptest.NewRequest(http.MethodPost, "/api/remote/commands", bytes.NewReader(body))
		recorder := httptest.NewRecorder()
		app.sendRemoteCommand(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s was not blocked locally: %d", name, recorder.Code)
		}
	}
}
