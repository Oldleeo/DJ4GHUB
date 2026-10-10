package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func panelRequest(t *testing.T, handler http.Handler, method, target string, body any, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, target, bytes.NewReader(payload))
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func TestPanelTOTP(t *testing.T) {
	relay, handler, owner := testRelay(t)
	secret := "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP"
	if err := relay.setPanelTOTP(secret); err != nil {
		t.Fatal(err)
	}
	relay.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	rejected := panelRequest(t, handler, http.MethodPost, "/panel/api/login", map[string]string{"password": owner, "otp": "000000"}, nil, "")
	if rejected.Code != http.StatusUnauthorized {
		t.Fatalf("invalid TOTP accepted: %d", rejected.Code)
	}
	accepted := panelRequest(t, handler, http.MethodPost, "/panel/api/login", map[string]string{"password": owner, "otp": totpAt(relay.panelTOTP, relay.now())}, nil, "")
	if accepted.Code != http.StatusOK {
		t.Fatal(accepted.Code, accepted.Body.String())
	}
}

func TestPanelLoginAndAppAreExclusiveLightViews(t *testing.T) {
	css, err := fs.ReadFile(panelFiles, "panel/app.css")
	if err != nil {
		t.Fatal(err)
	}
	content := string(css)
	for _, required := range []string{"[hidden]{display:none!important}", "color-scheme:light"} {
		if !strings.Contains(content, required) {
			t.Fatalf("panel CSS is missing %q", required)
		}
	}
	index, err := fs.ReadFile(panelFiles, "panel/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), `id="app" class="app-shell" hidden`) {
		t.Fatal("panel app must be hidden until authentication succeeds")
	}
	panelHTML := string(index)
	for _, required := range []string{"oldlee-avatar.png", `id="esim-scan-qr"`, `id="esim-download-panel"`, `id="notification-form"`, `id="retention-form"`, `data-view="retention"`, "© 2026 老李 Oldlee"} {
		if !strings.Contains(panelHTML, required) {
			t.Fatalf("updated panel is missing %q", required)
		}
	}
	if strings.Contains(panelHTML, `data-view="at"`) {
		t.Fatal("AT navigation must be hidden from the owner panel")
	}
}

func loginPanel(t *testing.T, handler http.Handler, password string) (*http.Cookie, string) {
	t.Helper()
	response := panelRequest(t, handler, http.MethodPost, "/panel/api/login", map[string]string{"password": password}, nil, "")
	if response.Code != http.StatusOK {
		t.Fatal(response.Code, response.Body.String())
	}
	result := decodeResponse(t, response)
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != panelCookieName {
		t.Fatal("panel login did not return session cookie")
	}
	return cookies[0], result["csrf"].(string)
}

func TestPanelLoginCommandAndAudit(t *testing.T) {
	_, handler, owner := testRelay(t)
	registered := request(t, handler, http.MethodPost, "/api/v1/hosts/register", owner, map[string]string{"device_id": "imei:panel", "device_name": "DJI Module", "host_id": "win:one", "host_name": "Windows"})
	if registered.Code != http.StatusCreated {
		t.Fatal(registered.Code, registered.Body.String())
	}
	cookie, csrf := loginPanel(t, handler, owner)
	devices := panelRequest(t, handler, http.MethodGet, "/panel/api/devices", nil, cookie, "")
	if devices.Code != http.StatusOK {
		t.Fatal(devices.Code, devices.Body.String())
	}
	deviceList := decodeResponse(t, devices)["devices"].([]any)
	deviceID := deviceList[0].(map[string]any)["id"].(string)
	withoutCSRF := panelRequest(t, handler, http.MethodPost, "/panel/api/devices/"+deviceID+"/commands", map[string]any{"name": "dial", "payload": map[string]string{"number": "+6421000000"}}, cookie, "")
	if withoutCSRF.Code != http.StatusForbidden {
		t.Fatalf("panel mutation accepted without CSRF: %d", withoutCSRF.Code)
	}
	command := panelRequest(t, handler, http.MethodPost, "/panel/api/devices/"+deviceID+"/commands", map[string]any{"name": "send_sms", "payload": map[string]string{"phone": "+6421000000", "message": "hello"}}, cookie, csrf)
	if command.Code != http.StatusCreated {
		t.Fatal(command.Code, command.Body.String())
	}
	audit := panelRequest(t, handler, http.MethodGet, "/panel/api/audit", nil, cookie, "")
	if audit.Code != http.StatusOK || len(decodeResponse(t, audit)["audit"].([]any)) != 1 {
		t.Fatal(audit.Code, audit.Body.String())
	}
}

func TestPanelAcceptsExpandedDesktopCommands(t *testing.T) {
	_, handler, owner := testRelay(t)
	registered := request(t, handler, http.MethodPost, "/api/v1/hosts/register", owner, map[string]string{"device_id": "imei:full-panel", "device_name": "DJI Module", "host_id": "win:full", "host_name": "Windows"})
	if registered.Code != http.StatusCreated {
		t.Fatal(registered.Code, registered.Body.String())
	}
	cookie, csrf := loginPanel(t, handler, owner)
	devices := panelRequest(t, handler, http.MethodGet, "/panel/api/devices", nil, cookie, "")
	deviceID := decodeResponse(t, devices)["devices"].([]any)[0].(map[string]any)["id"].(string)
	for _, command := range []map[string]any{
		{"name": "read_network"},
		{"name": "execute_at", "payload": map[string]string{"command": "AT+CSQ"}},
		{"name": "save_apn", "payload": map[string]string{"apn": "internet", "pdn": "IPV4V6"}},
		{"name": "read_esim"},
		{"name": "read_history", "payload": map[string]any{"kind": "sms", "offset": 0}},
		{"name": "read_notifications"},
		{"name": "delete_sms", "payload": map[string]string{"id": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
		{"name": "save_notifications", "payload": map[string]any{"bark_enabled": true, "bark_url": "https://api.day.app/example", "bark_group": "DJ4GHUB"}},
		{"name": "test_notification", "payload": map[string]string{"channel": "bark"}},
		{"name": "read_retention"},
		{"name": "save_retention", "payload": map[string]any{"label": "UK main", "operator": "Example", "next_due": "2027-09-16", "grace_period_days": 60, "enabled": true}},
		{"name": "delete_retention", "payload": map[string]string{"id": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
		{"name": "complete_retention", "payload": map[string]string{"id": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "next_due": "2027-09-16"}},
		{"name": "verify_retention", "payload": map[string]string{"id": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
	} {
		response := panelRequest(t, handler, http.MethodPost, "/panel/api/devices/"+deviceID+"/commands", command, cookie, csrf)
		if response.Code != http.StatusCreated {
			t.Fatalf("%s failed: %d %s", command["name"], response.Code, response.Body.String())
		}
	}
	invalidAT := panelRequest(t, handler, http.MethodPost, "/panel/api/devices/"+deviceID+"/commands", map[string]any{"name": "execute_at", "payload": map[string]string{"command": "AT+CSQ\nAT+CFUN=1,1"}}, cookie, csrf)
	if invalidAT.Code != http.StatusBadRequest {
		t.Fatalf("multi-line AT command accepted: %d", invalidAT.Code)
	}
}

func TestNotificationCredentialPayloadIsClearedAfterHostResult(t *testing.T) {
	relay, handler, owner := testRelay(t)
	registered := request(t, handler, http.MethodPost, "/api/v1/hosts/register", owner, map[string]string{"device_id": "imei:notify", "device_name": "DJI Module", "host_id": "win:notify", "host_name": "Windows"})
	if registered.Code != http.StatusCreated {
		t.Fatal(registered.Code, registered.Body.String())
	}
	hostToken := decodeResponse(t, registered)["host_token"].(string)
	cookie, csrf := loginPanel(t, handler, owner)
	devices := panelRequest(t, handler, http.MethodGet, "/panel/api/devices", nil, cookie, "")
	deviceID := decodeResponse(t, devices)["devices"].([]any)[0].(map[string]any)["id"].(string)
	created := panelRequest(t, handler, http.MethodPost, "/panel/api/devices/"+deviceID+"/commands", map[string]any{"name": "save_notifications", "payload": map[string]any{"bark_enabled": true, "bark_url": "https://api.day.app/private-key"}}, cookie, csrf)
	if created.Code != http.StatusCreated {
		t.Fatal(created.Code, created.Body.String())
	}
	commandID := int64(decodeResponse(t, created)["id"].(float64))
	result := request(t, handler, http.MethodPost, fmt.Sprintf("/api/v1/host/commands/%d/result", commandID), hostToken, map[string]any{"result": map[string]any{"ok": true}})
	if result.Code != http.StatusOK {
		t.Fatal(result.Code, result.Body.String())
	}
	relay.store.mu.Lock()
	defer relay.store.mu.Unlock()
	commands := relay.store.state.Devices["imei:notify"].Commands
	if len(commands) != 1 || len(commands[0].Payload) != 0 {
		t.Fatal("notification credential payload was retained after execution")
	}
}

func TestPanelCreatesOneTimeHostPairing(t *testing.T) {
	_, handler, owner := testRelay(t)
	registered := request(t, handler, http.MethodPost, "/api/v1/hosts/register", owner, map[string]string{"device_id": "imei:host-pair", "device_name": "DJI Module", "host_id": "win:old", "host_name": "Old Windows"})
	if registered.Code != http.StatusCreated {
		t.Fatal(registered.Code, registered.Body.String())
	}
	cookie, csrf := loginPanel(t, handler, owner)
	devices := panelRequest(t, handler, http.MethodGet, "/panel/api/devices", nil, cookie, "")
	deviceID := decodeResponse(t, devices)["devices"].([]any)[0].(map[string]any)["id"].(string)
	pairing := panelRequest(t, handler, http.MethodPost, "/panel/api/devices/"+deviceID+"/host-pairing", map[string]any{}, cookie, csrf)
	if pairing.Code != http.StatusCreated {
		t.Fatal(pairing.Code, pairing.Body.String())
	}
	code := decodeResponse(t, pairing)["code"].(string)
	claim := request(t, handler, http.MethodPost, "/api/v1/hosts/claim", "", map[string]string{"code": code, "host_id": "win:new", "host_name": "New Windows"})
	if claim.Code != http.StatusCreated {
		t.Fatal(claim.Code, claim.Body.String())
	}
	reuse := request(t, handler, http.MethodPost, "/api/v1/hosts/claim", "", map[string]string{"code": code, "host_id": "win:third", "host_name": "Third Windows"})
	if reuse.Code != http.StatusBadRequest {
		t.Fatalf("one-time host code was reusable: %d", reuse.Code)
	}
}

func TestAudioSignalingBetweenPanelAndHost(t *testing.T) {
	_, handler, owner := testRelay(t)
	registered := request(t, handler, http.MethodPost, "/api/v1/hosts/register", owner, map[string]string{"device_id": "imei:audio", "device_name": "DJI Module", "host_id": "win:audio", "host_name": "Windows"})
	hostToken := decodeResponse(t, registered)["host_token"].(string)
	cookie, csrf := loginPanel(t, handler, owner)
	devices := panelRequest(t, handler, http.MethodGet, "/panel/api/devices", nil, cookie, "")
	deviceID := decodeResponse(t, devices)["devices"].([]any)[0].(map[string]any)["id"].(string)
	created := panelRequest(t, handler, http.MethodPost, "/panel/api/devices/"+deviceID+"/audio/sessions", map[string]any{}, cookie, csrf)
	if created.Code != http.StatusCreated {
		t.Fatal(created.Code, created.Body.String())
	}
	sessionID := decodeResponse(t, created)["id"].(string)
	panelOffer := panelRequest(t, handler, http.MethodPost, "/panel/api/devices/"+deviceID+"/audio/sessions/"+sessionID+"/signals", map[string]any{"type": "offer", "payload": map[string]string{"type": "offer", "sdp": "example"}}, cookie, csrf)
	if panelOffer.Code != http.StatusCreated {
		t.Fatal(panelOffer.Code, panelOffer.Body.String())
	}
	hostSignals := request(t, handler, http.MethodGet, "/api/v1/host/audio/sessions/"+sessionID+"/signals?after=0", hostToken, nil)
	if hostSignals.Code != http.StatusOK || len(decodeResponse(t, hostSignals)["signals"].([]any)) != 1 {
		t.Fatal(hostSignals.Code, hostSignals.Body.String())
	}
	hostAnswer := request(t, handler, http.MethodPost, "/api/v1/host/audio/sessions/"+sessionID+"/signals", hostToken, map[string]any{"type": "answer", "payload": map[string]string{"type": "answer", "sdp": "example-answer"}})
	if hostAnswer.Code != http.StatusCreated {
		t.Fatal(hostAnswer.Code, hostAnswer.Body.String())
	}
	panelSignals := panelRequest(t, handler, http.MethodGet, "/panel/api/devices/"+deviceID+"/audio/sessions/"+sessionID+"/signals?after=0", nil, cookie, "")
	if panelSignals.Code != http.StatusOK || len(decodeResponse(t, panelSignals)["signals"].([]any)) != 1 {
		t.Fatal(panelSignals.Code, panelSignals.Body.String())
	}
}
