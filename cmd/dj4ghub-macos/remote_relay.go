package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Public builds are local-first: users must explicitly configure their own relay.
const defaultRelayURL = ""

type remoteRelayConfig struct {
	RelayURL       string `json:"relay_url"`
	HostEnabled    bool   `json:"host_enabled"`
	HostID         string `json:"host_id"`
	HostName       string `json:"host_name"`
	HostToken      string `json:"host_token,omitempty"`
	HostDeviceID   string `json:"host_device_id,omitempty"`
	HostDeviceName string `json:"host_device_name,omitempty"`
	OwnerToken     string `json:"owner_token,omitempty"`
	ClientToken    string `json:"client_token,omitempty"`
	ClientDevice   string `json:"client_device_id,omitempty"`
	ClientScope    string `json:"client_scope,omitempty"`
}

type remoteRelayManager struct {
	mu             sync.Mutex
	path           string
	config         remoteRelayConfig
	client         *http.Client
	lastSync       time.Time
	lastError      string
	lastRevision   int64
	processed      map[int64]json.RawMessage
	commandJournal remoteCommandJournal
}

type remoteSnapshot struct {
	Status    json.RawMessage           `json:"status"`
	SMS       []receivedSMS             `json:"sms"`
	SMSStatus map[string]any            `json:"sms_status"`
	Calls     []voiceCall               `json:"calls"`
	Retention retentionOverviewResponse `json:"retention"`
	Captured  time.Time                 `json:"captured_at"`
}

type relayError struct {
	Status int
	Text   string
}

func (e *relayError) Error() string { return e.Text }

func newRemoteRelayManager(dataDir string) (*remoteRelayManager, error) {
	root := strings.TrimSpace(dataDir)
	if root == "" {
		var err error
		root, err = os.UserConfigDir()
		if err != nil {
			return nil, err
		}
	}
	path := filepath.Join(root, "DJ4Hub", "remote-relay.json")
	m := &remoteRelayManager{
		path:      path,
		processed: map[int64]json.RawMessage{},
		client: &http.Client{
			Timeout: 20 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
	m.config.RelayURL = defaultRelayURL
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &m.config); err != nil {
			return nil, fmt.Errorf("decode remote relay settings: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := validateRelayURL(m.config.RelayURL); err != nil {
		m.config.RelayURL = defaultRelayURL
	}
	if m.config.HostID == "" {
		identifier, err := randomLocalIdentifier()
		if err != nil {
			return nil, err
		}
		m.config.HostID = "mac:" + identifier
		m.config.HostName, _ = os.Hostname()
		if err := m.saveLocked(); err != nil {
			return nil, err
		}
	}
	if err := m.loadCommandJournal(); err != nil {
		return nil, err
	}
	return m, nil
}

func randomLocalIdentifier() (string, error) {
	data := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}

func validateRelayURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("中继地址必须是没有账号、参数或片段的 HTTPS 地址")
	}
	return nil
}

func (m *remoteRelayManager) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(m.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m.config, "", "  ")
	if err != nil {
		return err
	}
	temporary := m.path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(temporary, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, m.path)
}

func (m *remoteRelayManager) settings() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return map[string]any{
		"relay_url":        m.config.RelayURL,
		"host_enabled":     m.config.HostEnabled,
		"host_registered":  m.config.HostToken != "",
		"host_device_id":   maskRemoteIdentifier(m.config.HostDeviceID),
		"owner_configured": m.config.OwnerToken != "",
		"client_bound":     m.config.ClientToken != "",
		"client_device_id": maskRemoteIdentifier(m.config.ClientDevice),
		"client_scope":     m.config.ClientScope,
		"last_sync":        m.lastSync,
		"last_error":       m.lastError,
		"last_revision":    m.lastRevision,
	}
}

func maskRemoteIdentifier(value string) string {
	if len(value) <= 10 {
		return value
	}
	return value[:5] + "…" + value[len(value)-4:]
}

func (m *remoteRelayManager) relayJSON(ctx context.Context, method, path, token string, body any, destination any) error {
	m.mu.Lock()
	base := strings.TrimRight(m.config.RelayURL, "/")
	m.mu.Unlock()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, base+path, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := m.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, 2<<20)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(limited).Decode(&failure)
		if failure.Error == "" {
			failure.Error = response.Status
		}
		return &relayError{Status: response.StatusCode, Text: failure.Error}
	}
	if destination == nil {
		_, _ = io.Copy(io.Discard, limited)
		return nil
	}
	return json.NewDecoder(limited).Decode(destination)
}

func captureLocalJSON(handler func(http.ResponseWriter, *http.Request), path string) (json.RawMessage, error) {
	recorder := httptest.NewRecorder()
	handler(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	if recorder.Code < 200 || recorder.Code >= 300 {
		return nil, fmt.Errorf("local endpoint returned %d", recorder.Code)
	}
	data := bytes.TrimSpace(recorder.Body.Bytes())
	if !json.Valid(data) {
		return nil, errors.New("local endpoint returned invalid JSON")
	}
	return append(json.RawMessage(nil), data...), nil
}

func captureLocalAction(handler func(http.ResponseWriter, *http.Request), method, path string, payload json.RawMessage) (any, error) {
	body := io.Reader(http.NoBody)
	if len(bytes.TrimSpace(payload)) > 0 {
		body = bytes.NewReader(payload)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, body)
	if len(bytes.TrimSpace(payload)) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	handler(recorder, request)
	data := bytes.TrimSpace(recorder.Body.Bytes())
	if recorder.Code < 200 || recorder.Code >= 300 {
		var failure struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &failure)
		if failure.Error == "" {
			failure.Error = fmt.Sprintf("local endpoint returned %d", recorder.Code)
		}
		return nil, errors.New(failure.Error)
	}
	if len(data) == 0 {
		return map[string]bool{"accepted": true}, nil
	}
	var result any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, errors.New("local endpoint returned invalid JSON")
	}
	return result, nil
}

func (a *app) buildRemoteSnapshot() (remoteSnapshot, string, error) {
	status, err := captureLocalJSON(a.status, "/api/status")
	if err != nil {
		return remoteSnapshot{}, "", err
	}
	var statusFields map[string]any
	if err := json.Unmarshal(status, &statusFields); err != nil {
		return remoteSnapshot{}, "", err
	}
	imei, _ := statusFields["imei"].(string)
	imei = strings.TrimSpace(imei)
	if imei == "" {
		return remoteSnapshot{}, "", errors.New("等待模块返回 IMEI 后才能成为设备主机")
	}
	a.smsMu.RLock()
	messages := append([]receivedSMS(nil), a.sms...)
	lastPoll, lastPollError := a.smsLastPoll, a.smsLastPollError
	a.smsMu.RUnlock()
	calls := []voiceCall{}
	if raw, callErr := a.phoneCommand("AT+CLCC"); callErr == nil {
		calls = parseVoiceCalls(raw)
	}
	return remoteSnapshot{
		Status: status,
		SMS:    messages,
		SMSStatus: map[string]any{
			"count": len(messages), "last_poll": lastPoll, "last_poll_error": lastPollError,
		},
		Calls:     calls,
		Retention: a.retentionOverviewValue(),
		Captured:  time.Now().UTC(),
	}, "imei:" + imei, nil
}

func (a *app) monitorRemoteRelay(ctx context.Context) {
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if a.remote != nil {
				a.syncRemoteRelay(ctx)
			}
			timer.Reset(10 * time.Second)
		}
	}
}

func (a *app) syncRemoteRelay(ctx context.Context) {
	m := a.remote
	m.mu.Lock()
	enabled := m.config.HostEnabled
	m.mu.Unlock()
	if !enabled {
		return
	}
	snapshot, deviceID, err := a.buildRemoteSnapshot()
	if err != nil {
		m.setSyncResult(0, err)
		return
	}
	m.mu.Lock()
	hostToken, ownerToken := m.config.HostToken, m.config.OwnerToken
	registeredDevice := m.config.HostDeviceID
	hostID, hostName := m.config.HostID, m.config.HostName
	m.mu.Unlock()
	deviceName := strings.TrimSpace(a.deviceName)
	if deviceName == "" {
		deviceName = "DJI 4G Module"
	}
	m.mu.Lock()
	registeredName := m.config.HostDeviceName
	m.mu.Unlock()
	if hostToken == "" || registeredDevice != deviceID || registeredName != deviceName {
		if ownerToken == "" {
			m.setSyncResult(0, errors.New("尚未配置设备主机密钥"))
			return
		}
		var registration struct {
			HostToken string `json:"host_token"`
		}
		err = m.relayJSON(ctx, http.MethodPost, "/api/v1/hosts/register", ownerToken, map[string]string{
			"device_id": deviceID, "device_name": deviceName, "host_id": hostID, "host_name": hostName,
		}, &registration)
		if err != nil {
			m.setSyncResult(0, err)
			return
		}
		hostToken = registration.HostToken
		m.mu.Lock()
		m.config.HostToken, m.config.HostDeviceID, m.config.HostDeviceName = hostToken, deviceID, deviceName
		err = m.saveLocked()
		m.mu.Unlock()
		if err != nil {
			m.setSyncResult(0, err)
			return
		}
	}
	var update struct {
		Revision int64 `json:"revision"`
	}
	err = m.relayJSON(ctx, http.MethodPut, "/api/v1/host/snapshot", hostToken, map[string]any{"snapshot": snapshot}, &update)
	if err != nil {
		m.handleHostError(err)
		return
	}
	m.setSyncResult(update.Revision, nil)
	a.processRemoteCommands(ctx, hostToken)
}

func (m *remoteRelayManager) setSyncResult(revision int64, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		m.lastError = err.Error()
		return
	}
	m.lastError = ""
	m.lastSync = time.Now().UTC()
	m.lastRevision = revision
}

func (m *remoteRelayManager) handleHostError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastError = err.Error()
	var remoteErr *relayError
	if errors.As(err, &remoteErr) {
		switch remoteErr.Status {
		case http.StatusUnauthorized:
			m.config.HostToken = ""
		case http.StatusConflict:
			m.config.HostEnabled = false
			m.config.HostToken = ""
		}
		_ = m.saveLocked()
	}
}

type pendingRelayCommand struct {
	ID      int64           `json:"id"`
	Name    string          `json:"name"`
	Payload json.RawMessage `json:"payload"`
}

func (a *app) processRemoteCommands(ctx context.Context, hostToken string) {
	var response struct {
		Commands []pendingRelayCommand `json:"commands"`
	}
	if err := a.remote.relayJSON(ctx, http.MethodGet, "/api/v1/host/commands?after=0", hostToken, nil, &response); err != nil {
		a.remote.handleHostError(err)
		return
	}
	for _, command := range response.Commands {
		a.remote.mu.Lock()
		result, alreadyDone := a.remote.processed[command.ID]
		a.remote.mu.Unlock()
		if !alreadyDone {
			if remoteCommandNeedsJournal(command.Name) {
				var err error
				result, alreadyDone, err = a.remote.reserveRemoteCommand(command)
				if err != nil {
					a.remote.handleHostError(err)
					return
				}
			}
		}
		if !alreadyDone {
			result = a.executeRemoteCommand(command)
			if remoteCommandNeedsJournal(command.Name) {
				if err := a.remote.finishRemoteCommand(command, result); err != nil {
					a.remote.handleHostError(err)
					return // The durable started marker prevents a replay after crash.
				}
			}
			a.remote.mu.Lock()
			a.remote.processed[command.ID] = result
			if len(a.remote.processed) > 100 {
				for id := range a.remote.processed {
					delete(a.remote.processed, id)
					break
				}
			}
			a.remote.mu.Unlock()
		}
		path := fmt.Sprintf("/api/v1/host/commands/%d/result", command.ID)
		if err := a.remote.relayJSON(ctx, http.MethodPost, path, hostToken, map[string]json.RawMessage{"result": result}, nil); err == nil {
			if remoteCommandNeedsJournal(command.Name) {
				_ = a.remote.acknowledgeRemoteCommand(command.ID)
			}
			a.remote.mu.Lock()
			delete(a.remote.processed, command.ID)
			a.remote.mu.Unlock()
		}
	}
}

func (a *app) executeRemoteCommand(command pendingRelayCommand) json.RawMessage {
	result := map[string]any{"ok": true, "command": command.Name}
	var err error
	local := func(handler func(http.ResponseWriter, *http.Request), method, path string, payload json.RawMessage) {
		var data any
		data, err = captureLocalAction(handler, method, path, payload)
		if err == nil {
			result["data"] = data
		}
	}
	switch command.Name {
	case "refresh_status":
		local(a.status, http.MethodGet, "/api/status?refresh=1", nil)
	case "refresh_sms":
		local(a.refreshSMS, http.MethodPost, "/api/sms/refresh", nil)
	case "read_sms_status":
		local(a.smsStatus, http.MethodGet, "/api/sms/status", nil)
	case "clear_sms":
		local(a.clearModuleSMS, http.MethodPost, "/api/sms/clear-module", nil)
	case "delete_sms":
		var payload struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(command.Payload, &payload) != nil || !smsMessageIDPattern.MatchString(strings.ToLower(strings.TrimSpace(payload.ID))) {
			err = errors.New("invalid SMS deletion payload")
			break
		}
		encoded, _ := json.Marshal(map[string]string{"id": strings.ToLower(strings.TrimSpace(payload.ID))})
		local(a.deleteSMS, http.MethodPost, "/api/sms/delete", encoded)
	case "answer":
		local(a.callAction, http.MethodPost, "/api/calls", json.RawMessage(`{"action":"answer"}`))
	case "hangup":
		local(a.callAction, http.MethodPost, "/api/calls", json.RawMessage(`{"action":"hangup"}`))
	case "dial":
		var payload struct {
			Number string `json:"number"`
		}
		if json.Unmarshal(command.Payload, &payload) != nil || !voiceNumberPattern.MatchString(payload.Number) {
			err = errors.New("invalid dial payload")
			break
		}
		encoded, _ := json.Marshal(map[string]string{"action": "dial", "number": payload.Number})
		local(a.callAction, http.MethodPost, "/api/calls", encoded)
	case "dtmf":
		var payload struct {
			Digit string `json:"digit"`
		}
		if json.Unmarshal(command.Payload, &payload) != nil || !voiceDTMFPattern.MatchString(payload.Digit) {
			err = errors.New("invalid DTMF payload")
			break
		}
		encoded, _ := json.Marshal(map[string]string{"action": "dtmf", "digit": payload.Digit})
		local(a.callAction, http.MethodPost, "/api/calls", encoded)
	case "send_sms":
		var payload struct {
			Phone   string `json:"phone"`
			Message string `json:"message"`
		}
		if json.Unmarshal(command.Payload, &payload) != nil || !voiceNumberPattern.MatchString(payload.Phone) || strings.TrimSpace(payload.Message) == "" || len([]rune(payload.Message)) > 2000 {
			err = errors.New("invalid SMS payload")
			break
		}
		identity := a.historyIdentity()
		segments, sendErr := a.sendTextSMS(payload.Phone, payload.Message)
		state := "sent"
		if sendErr != nil {
			state = "failed_or_partial"
		}
		archiveErr := a.appendHistory(historyRecord{Kind: "sms", Direction: "outgoing", ICCID: confirmedHistoryIdentity(identity, a.historyIdentity()), Number: payload.Phone, Content: payload.Message, State: state, Started: time.Now()})
		if sendErr != nil {
			err = sendErr
		} else if archiveErr != nil {
			err = errors.New("SMS was sent but history could not be saved; do not retry automatically")
		} else {
			result["segments"] = segments
		}
	case "read_history":
		var payload struct {
			Kind   string `json:"kind"`
			Offset int    `json:"offset"`
		}
		_ = json.Unmarshal(command.Payload, &payload)
		query := url.Values{"card": []string{"all"}, "offset": []string{fmt.Sprint(payload.Offset)}}
		if payload.Kind != "" {
			query.Set("kind", payload.Kind)
		}
		local(a.listHistory, http.MethodGet, "/api/history?"+query.Encode(), nil)
	case "read_network":
		local(a.networkDiagnostic, http.MethodGet, "/api/network", nil)
	case "read_network_local":
		local(a.localNetworkConnection, http.MethodGet, "/api/network/local", nil)
	case "read_network_activity":
		local(a.networkActivity, http.MethodGet, "/api/network/activity", nil)
	case "read_network_traffic":
		local(a.networkTraffic, http.MethodGet, "/api/network/traffic", nil)
	case "check_4g":
		local(a.check4GRoute, http.MethodPost, "/api/network/check-4g", nil)
	case "check_proxy":
		local(a.checkProxyRoute, http.MethodPost, "/api/network/check-proxy", nil)
	case "enable_network":
		local(a.enableNetworkService, http.MethodPost, "/api/network/enable-service", nil)
	case "save_apn":
		local(a.saveAPN, http.MethodPost, "/api/network/apn", command.Payload)
	case "set_usbnet":
		local(a.setUSBNetMode, http.MethodPost, "/api/network/usbnet", command.Payload)
	case "reboot_module":
		local(a.rebootModule, http.MethodPost, "/api/network/reboot-module", nil)
	case "execute_at":
		local(a.executeAT, http.MethodPost, "/api/at", command.Payload)
	case "read_esim":
		local(a.esimOverview, http.MethodGet, "/api/esim", nil)
	case "read_esim_health":
		local(a.esimHealth, http.MethodGet, "/api/esim/health", nil)
	case "probe_esim_phonebook":
		local(a.probeESIMPhonebook, http.MethodPost, "/api/esim/phonebook/probe", nil)
	case "read_esim_notes":
		local(a.listESIMNotes, http.MethodGet, "/api/esim/notes", nil)
	case "save_esim_note":
		local(a.saveESIMNote, http.MethodPut, "/api/esim/notes", command.Payload)
	case "read_module_esim_notes":
		local(a.listModuleESIMNotes, http.MethodGet, "/api/esim/module-notes", nil)
	case "save_module_esim_note":
		local(a.saveModuleESIMNote, http.MethodPut, "/api/esim/module-notes", command.Payload)
	case "switch_esim":
		local(a.switchESIM, http.MethodPost, "/api/esim/switch", command.Payload)
	case "rename_esim":
		local(a.renameESIMProfile, http.MethodPatch, "/api/esim/profile", command.Payload)
	case "delete_esim":
		local(a.deleteESIMProfile, http.MethodDelete, "/api/esim/profile", command.Payload)
	case "download_esim":
		local(a.downloadESIMProfile, http.MethodPost, "/api/esim/download", command.Payload)
	case "read_notifications":
		local(a.notificationSettings, http.MethodGet, "/api/notifications", nil)
	case "save_notifications":
		local(a.updateNotificationSettings, http.MethodPut, "/api/notifications", command.Payload)
	case "test_notification":
		local(a.testNotification, http.MethodPost, "/api/notifications/test", command.Payload)
	case "read_retention":
		local(a.retentionOverview, http.MethodGet, "/api/retention", nil)
	case "save_retention":
		local(a.saveRetentionCard, http.MethodPost, "/api/retention", command.Payload)
	case "delete_retention":
		local(a.deleteRetentionCard, http.MethodDelete, "/api/retention", command.Payload)
	case "complete_retention":
		local(a.completeRetentionCard, http.MethodPost, "/api/retention/complete", command.Payload)
	case "verify_retention":
		local(a.verifyRetentionCard, http.MethodPost, "/api/retention/verify", command.Payload)
	default:
		err = errors.New("command is not allowed")
	}
	if err != nil {
		result["ok"], result["error"] = false, err.Error()
	}
	encoded, _ := json.Marshal(result)
	return encoded
}

func (a *app) remoteSettings(w http.ResponseWriter, _ *http.Request) {
	if a.remote == nil {
		writeError(w, http.StatusServiceUnavailable, "远程中继未初始化")
		return
	}
	writeJSON(w, http.StatusOK, a.remote.settings())
}

func (a *app) updateRemoteHost(w http.ResponseWriter, r *http.Request) {
	if a.remote == nil {
		writeError(w, http.StatusServiceUnavailable, "远程中继未初始化")
		return
	}
	var body struct {
		Enabled  bool   `json:"enabled"`
		RelayURL string `json:"relay_url"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.RelayURL == "" {
		body.RelayURL = defaultRelayURL
	}
	if body.Enabled || body.RelayURL != "" {
		if err := validateRelayURL(body.RelayURL); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	a.remote.mu.Lock()
	if a.remote.config.RelayURL != strings.TrimRight(body.RelayURL, "/") {
		a.remote.config.HostToken = ""
		a.remote.config.ClientToken = ""
	}
	a.remote.config.RelayURL = strings.TrimRight(body.RelayURL, "/")
	a.remote.config.HostEnabled = body.Enabled
	a.remote.lastError = ""
	err := a.remote.saveLocked()
	a.remote.mu.Unlock()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, a.remote.settings())
}

func (a *app) setRemoteOwnerToken(w http.ResponseWriter, r *http.Request) {
	if a.remote == nil {
		writeError(w, http.StatusServiceUnavailable, "远程中继未初始化")
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 256))
	if err != nil {
		writeError(w, http.StatusBadRequest, "无法读取主机密钥")
		return
	}
	token := strings.TrimSpace(string(data))
	if len(token) < 32 {
		writeError(w, http.StatusBadRequest, "主机密钥无效")
		return
	}
	a.remote.mu.Lock()
	a.remote.config.OwnerToken = token
	err = a.remote.saveLocked()
	a.remote.mu.Unlock()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"configured": true})
}

func (a *app) createRemotePairing(w http.ResponseWriter, r *http.Request) {
	if a.remote == nil {
		writeError(w, http.StatusServiceUnavailable, "远程中继未初始化")
		return
	}
	var body struct {
		Scope string `json:"scope"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Scope == "" {
		body.Scope = "read"
	}
	a.remote.mu.Lock()
	token := a.remote.config.HostToken
	a.remote.mu.Unlock()
	if token == "" {
		writeError(w, http.StatusConflict, "本机尚未注册为设备主机")
		return
	}
	var pairing map[string]any
	if err := a.remote.relayJSON(r.Context(), http.MethodPost, "/api/v1/device/pairing", token, map[string]string{"scope": body.Scope}, &pairing); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, pairing)
}

func (a *app) claimRemoteHost(w http.ResponseWriter, r *http.Request) {
	if a.remote == nil {
		writeError(w, http.StatusServiceUnavailable, "远程中继未初始化")
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	a.remote.mu.Lock()
	hostID, hostName := a.remote.config.HostID, a.remote.config.HostName
	a.remote.mu.Unlock()
	var result struct {
		HostToken string `json:"host_token"`
		DeviceID  string `json:"device_id"`
	}
	if err := a.remote.relayJSON(r.Context(), http.MethodPost, "/api/v1/hosts/claim", "", map[string]string{
		"code": body.Code, "host_id": hostID, "host_name": hostName,
	}, &result); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	a.remote.mu.Lock()
	a.remote.config.HostToken = result.HostToken
	a.remote.config.HostDeviceID = result.DeviceID
	a.remote.config.HostEnabled = true
	a.remote.lastError = ""
	err := a.remote.saveLocked()
	a.remote.mu.Unlock()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"claimed": true, "device_id": maskRemoteIdentifier(result.DeviceID)})
}

func (a *app) redeemRemotePairing(w http.ResponseWriter, r *http.Request) {
	if a.remote == nil {
		writeError(w, http.StatusServiceUnavailable, "远程中继未初始化")
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	var result struct {
		ClientToken string `json:"client_token"`
		DeviceID    string `json:"device_id"`
		Scope       string `json:"scope"`
	}
	if err := a.remote.relayJSON(r.Context(), http.MethodPost, "/api/v1/pairings/redeem", "", body, &result); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	a.remote.mu.Lock()
	a.remote.config.ClientToken = result.ClientToken
	a.remote.config.ClientDevice = result.DeviceID
	a.remote.config.ClientScope = result.Scope
	err := a.remote.saveLocked()
	a.remote.mu.Unlock()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"bound": true, "device_id": maskRemoteIdentifier(result.DeviceID), "scope": result.Scope})
}

func (a *app) readRemoteDevice(w http.ResponseWriter, r *http.Request) {
	if a.remote == nil {
		writeError(w, http.StatusServiceUnavailable, "远程中继未初始化")
		return
	}
	a.remote.mu.Lock()
	token := a.remote.config.ClientToken
	a.remote.mu.Unlock()
	if token == "" {
		writeError(w, http.StatusConflict, "尚未绑定远程设备")
		return
	}
	var device map[string]any
	if err := a.remote.relayJSON(r.Context(), http.MethodGet, "/api/v1/device", token, nil, &device); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, device)
}

func (a *app) sendRemoteCommand(w http.ResponseWriter, r *http.Request) {
	if a.remote == nil {
		writeError(w, http.StatusServiceUnavailable, "远程中继未初始化")
		return
	}
	var body struct {
		Name    string          `json:"name"`
		Payload json.RawMessage `json:"payload"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	allowed := map[string]bool{"refresh_status": true, "refresh_sms": true, "answer": true, "hangup": true, "send_sms": true, "dial": true, "dtmf": true}
	if !allowed[body.Name] {
		writeError(w, http.StatusBadRequest, "远程命令未开放")
		return
	}
	if err := validateLocalRemoteCommand(body.Name, body.Payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.remote.mu.Lock()
	token := a.remote.config.ClientToken
	a.remote.mu.Unlock()
	if token == "" {
		writeError(w, http.StatusConflict, "尚未绑定远程设备")
		return
	}
	var result map[string]any
	if err := a.remote.relayJSON(r.Context(), http.MethodPost, "/api/v1/device/commands", token, body, &result); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func validateLocalRemoteCommand(name string, payload json.RawMessage) error {
	switch name {
	case "refresh_status", "refresh_sms", "answer", "hangup":
		return nil
	case "dial":
		var value struct {
			Number string `json:"number"`
		}
		if json.Unmarshal(payload, &value) != nil || !voiceNumberPattern.MatchString(value.Number) {
			return errors.New("请输入有效电话号码")
		}
	case "dtmf":
		var value struct {
			Digit string `json:"digit"`
		}
		if json.Unmarshal(payload, &value) != nil || !voiceDTMFPattern.MatchString(value.Digit) {
			return errors.New("无效按键")
		}
	case "send_sms":
		var value struct {
			Phone   string `json:"phone"`
			Message string `json:"message"`
		}
		if json.Unmarshal(payload, &value) != nil || !voiceNumberPattern.MatchString(value.Phone) {
			return errors.New("请输入有效短信号码")
		}
		if strings.TrimSpace(value.Message) == "" || len([]rune(value.Message)) > 2000 {
			return errors.New("短信正文必须为 1 至 2000 个字符")
		}
	}
	return nil
}

func (a *app) unlinkRemoteDevice(w http.ResponseWriter, _ *http.Request) {
	if a.remote == nil {
		writeError(w, http.StatusServiceUnavailable, "远程中继未初始化")
		return
	}
	a.remote.mu.Lock()
	a.remote.config.ClientToken = ""
	a.remote.config.ClientDevice = ""
	a.remote.config.ClientScope = ""
	err := a.remote.saveLocked()
	a.remote.mu.Unlock()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"unlinked": true})
}
