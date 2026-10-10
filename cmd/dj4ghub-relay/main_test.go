package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testRelay(t *testing.T) (*relayServer, http.Handler, string) {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	store, err := newEncryptedStore(filepath.Join(t.TempDir(), "state.enc"), key)
	if err != nil {
		t.Fatal(err)
	}
	owner := "owner-token-with-at-least-thirty-two-characters"
	relay, err := newRelayServer(store, owner)
	if err != nil {
		t.Fatal(err)
	}
	return relay, relay.routes(), owner
}

func request(t *testing.T, handler http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(data))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func decodeResponse(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response %d: %v %s", recorder.Code, err, recorder.Body.String())
	}
	return result
}

func TestPairingSnapshotAndSafeCommands(t *testing.T) {
	_, handler, owner := testRelay(t)
	registered := request(t, handler, "POST", "/api/v1/hosts/register", owner, map[string]string{"device_id": "eid:example", "device_name": "Switch SIM", "host_id": "mac:korea", "host_name": "Korea Mac"})
	if registered.Code != 201 {
		t.Fatal(registered.Code, registered.Body.String())
	}
	hostToken := decodeResponse(t, registered)["host_token"].(string)
	updated := request(t, handler, "PUT", "/api/v1/host/snapshot", hostToken, map[string]any{"snapshot": map[string]any{"sms": []any{map[string]string{"sender": "10086", "content": "hello"}}, "status": map[string]string{"network": "LTE"}}})
	if updated.Code != 200 {
		t.Fatal(updated.Code, updated.Body.String())
	}
	pairing := request(t, handler, "POST", "/api/v1/device/pairing", hostToken, map[string]string{"scope": "control"})
	if pairing.Code != 201 {
		t.Fatal(pairing.Code, pairing.Body.String())
	}
	code := decodeResponse(t, pairing)["code"].(string)
	redeemed := request(t, handler, "POST", "/api/v1/pairings/redeem", "", map[string]string{"code": code})
	if redeemed.Code != 201 {
		t.Fatal(redeemed.Code, redeemed.Body.String())
	}
	clientToken := decodeResponse(t, redeemed)["client_token"].(string)
	device := request(t, handler, "GET", "/api/v1/device", clientToken, nil)
	if device.Code != 200 || decodeResponse(t, device)["online"] != true {
		t.Fatal(device.Code, device.Body.String())
	}
	command := request(t, handler, "POST", "/api/v1/device/commands", clientToken, map[string]string{"name": "refresh_sms"})
	if command.Code != 201 {
		t.Fatal(command.Code, command.Body.String())
	}
	dial := request(t, handler, "POST", "/api/v1/device/commands", clientToken, map[string]any{"name": "dial", "payload": map[string]string{"number": "+6421000000"}})
	if dial.Code != 201 {
		t.Fatalf("remote dial was rejected: %d %s", dial.Code, dial.Body.String())
	}
	sms := request(t, handler, "POST", "/api/v1/device/commands", clientToken, map[string]any{"name": "send_sms", "payload": map[string]string{"phone": "+6421000000", "message": "hello"}})
	if sms.Code != 201 {
		t.Fatalf("remote SMS was rejected: %d %s", sms.Code, sms.Body.String())
	}
	deleted := request(t, handler, "POST", "/api/v1/device/commands", clientToken, map[string]any{"name": "delete_sms", "payload": map[string]string{"id": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}})
	if deleted.Code != 201 {
		t.Fatalf("remote SMS deletion was rejected: %d %s", deleted.Code, deleted.Body.String())
	}
	invalid := request(t, handler, "POST", "/api/v1/device/commands", clientToken, map[string]any{"name": "dial", "payload": map[string]string{"number": "AT+CHUP"}})
	if invalid.Code != 400 {
		t.Fatalf("invalid remote dial payload was accepted: %d", invalid.Code)
	}
	pending := request(t, handler, "GET", "/api/v1/host/commands?after=0", hostToken, nil)
	commands := decodeResponse(t, pending)["commands"].([]any)
	if len(commands) != 4 {
		t.Fatalf("expected four validated commands, got %d", len(commands))
	}
}

func TestPairingIsSingleUseAndExpires(t *testing.T) {
	relay, handler, owner := testRelay(t)
	now := time.Date(2026, 9, 15, 1, 2, 3, 0, time.UTC)
	relay.now = func() time.Time { return now }
	registered := request(t, handler, "POST", "/api/v1/hosts/register", owner, map[string]string{"device_id": "eid:expiry", "host_id": "mac:one"})
	hostToken := decodeResponse(t, registered)["host_token"].(string)
	pairing := request(t, handler, "POST", "/api/v1/device/pairing", hostToken, map[string]string{"scope": "read"})
	code := decodeResponse(t, pairing)["code"].(string)
	if request(t, handler, "POST", "/api/v1/pairings/redeem", "", map[string]string{"code": code}).Code != 201 {
		t.Fatal("first redemption failed")
	}
	if request(t, handler, "POST", "/api/v1/pairings/redeem", "", map[string]string{"code": code}).Code != 400 {
		t.Fatal("pairing code was reusable")
	}
	pairing = request(t, handler, "POST", "/api/v1/device/pairing", hostToken, map[string]string{"scope": "read"})
	code = decodeResponse(t, pairing)["code"].(string)
	now = now.Add(pairingLifetime + time.Second)
	if request(t, handler, "POST", "/api/v1/pairings/redeem", "", map[string]string{"code": code}).Code != 400 {
		t.Fatal("expired code was accepted")
	}
}

func TestEncryptedStateDoesNotContainSMSPlaintext(t *testing.T) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	path := filepath.Join(t.TempDir(), "state.enc")
	store, err := newEncryptedStore(path, key)
	if err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	store.state.Devices["eid:test"] = &deviceState{ID: "eid:test", Snapshot: json.RawMessage(`{"sms":"private-message"}`)}
	err = store.saveLocked()
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("private-message")) {
		t.Fatal("state contains plaintext SMS")
	}
	reloaded, err := newEncryptedStore(path, key)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(reloaded.state.Devices["eid:test"].Snapshot, []byte("private-message")) {
		t.Fatal("encrypted state did not reload")
	}
}

func TestNewHostRevokesOldHostLease(t *testing.T) {
	_, handler, owner := testRelay(t)
	first := request(t, handler, "POST", "/api/v1/hosts/register", owner, map[string]string{
		"device_id": "imei:one", "host_id": "mac:first",
	})
	firstToken := decodeResponse(t, first)["host_token"].(string)
	second := request(t, handler, "POST", "/api/v1/hosts/register", owner, map[string]string{
		"device_id": "imei:one", "host_id": "mac:second",
	})
	secondToken := decodeResponse(t, second)["host_token"].(string)
	if response := request(t, handler, "POST", "/api/v1/host/heartbeat", firstToken, nil); response.Code != http.StatusConflict {
		t.Fatalf("old host retained control: %d %s", response.Code, response.Body.String())
	}
	if response := request(t, handler, "POST", "/api/v1/host/heartbeat", secondToken, nil); response.Code != http.StatusOK {
		t.Fatalf("new host did not control device: %d %s", response.Code, response.Body.String())
	}
}

func TestOneTimeHostPairingMovesDeviceHost(t *testing.T) {
	_, handler, owner := testRelay(t)
	registered := request(t, handler, "POST", "/api/v1/hosts/register", owner, map[string]string{
		"device_id": "imei:move", "host_id": "mac:old",
	})
	oldToken := decodeResponse(t, registered)["host_token"].(string)
	pairing := request(t, handler, "POST", "/api/v1/device/pairing", oldToken, map[string]string{"scope": "host"})
	if pairing.Code != http.StatusCreated {
		t.Fatal(pairing.Code, pairing.Body.String())
	}
	code := decodeResponse(t, pairing)["code"].(string)
	claim := request(t, handler, "POST", "/api/v1/hosts/claim", "", map[string]string{
		"code": code, "host_id": "mac:new", "host_name": "New Mac",
	})
	if claim.Code != http.StatusCreated {
		t.Fatal(claim.Code, claim.Body.String())
	}
	newToken := decodeResponse(t, claim)["host_token"].(string)
	if request(t, handler, "POST", "/api/v1/host/heartbeat", oldToken, nil).Code != http.StatusConflict {
		t.Fatal("old host was not revoked")
	}
	if request(t, handler, "POST", "/api/v1/host/heartbeat", newToken, nil).Code != http.StatusOK {
		t.Fatal("new host was not activated")
	}
	if request(t, handler, "POST", "/api/v1/hosts/claim", "", map[string]string{
		"code": code, "host_id": "mac:third",
	}).Code != http.StatusBadRequest {
		t.Fatal("host pairing code was reusable")
	}
}

func TestLoadPanelPasswordAcceptsUserChosenPassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.password")
	if err := os.WriteFile(path, []byte("OldLee2026\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	password, err := loadPanelPassword(path)
	if err != nil {
		t.Fatal(err)
	}
	if password != "OldLee2026" {
		t.Fatal("panel password was not loaded exactly")
	}
}
