package main

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/WongLoki/DJ4Hub/internal/uiassets"
)

const panelCookieName = "dj4ghub_panel"
const panelSessionLifetime = 12 * time.Hour

//go:embed panel/*
var panelFiles embed.FS

type panelSession struct {
	CSRF    string
	Expires time.Time
}

func (s *relayServer) setPanelPassword(password string) {
	s.panelHash = sha256.Sum256([]byte(strings.TrimSpace(password)))
}

func (s *relayServer) setPanelTOTP(secret string) error {
	normalized := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(secret), " ", ""))
	decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(normalized)
	if err != nil || len(decoded) < 20 {
		return fmt.Errorf("invalid panel TOTP secret")
	}
	s.panelTOTP = append([]byte(nil), decoded...)
	return nil
}

func totpAt(secret []byte, moment time.Time) string {
	counter := uint64(moment.Unix() / 30)
	message := make([]byte, 8)
	binary.BigEndian.PutUint64(message, counter)
	mac := hmac.New(sha1.New, secret)
	_, _ = mac.Write(message)
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := (binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff) % 1_000_000
	return fmt.Sprintf("%06d", value)
}

func (s *relayServer) validPanelTOTP(code string) bool {
	if len(s.panelTOTP) == 0 {
		return true
	}
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return false
	}
	now := s.now()
	for step := -1; step <= 1; step++ {
		expected := totpAt(s.panelTOTP, now.Add(time.Duration(step)*30*time.Second))
		if subtle.ConstantTimeCompare([]byte(code), []byte(expected)) == 1 {
			return true
		}
	}
	return false
}

func (s *relayServer) registerPanelRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /panel", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/panel/", http.StatusPermanentRedirect)
	})
	mux.HandleFunc("GET /panel/", s.servePanelAsset)
	mux.HandleFunc("POST /panel/api/login", s.panelLogin)
	mux.HandleFunc("GET /panel/api/session", s.panelSessionInfo)
	mux.HandleFunc("POST /panel/api/logout", s.panelLogout)
	mux.HandleFunc("GET /panel/api/devices", s.panelListDevices)
	mux.HandleFunc("GET /panel/api/devices/{device}", s.panelReadDevice)
	mux.HandleFunc("POST /panel/api/devices/{device}/host-pairing", s.panelCreateHostPairing)
	mux.HandleFunc("POST /panel/api/devices/{device}/commands", s.panelCreateCommand)
	mux.HandleFunc("GET /panel/api/devices/{device}/commands/{command}", s.panelReadCommand)
	mux.HandleFunc("GET /panel/api/audit", s.panelAudit)
}

func (s *relayServer) servePanelAsset(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/panel/")
	if name == "" {
		name = "index.html"
	}
	if strings.Contains(name, "..") || strings.HasPrefix(name, "/") {
		http.NotFound(w, r)
		return
	}
	if name == "spatial.css" || name == "spatial.js" {
		uiassets.Serve(w, r)
		return
	}
	data, err := fs.ReadFile(panelFiles, "panel/"+name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	if name == "sw.js" {
		w.Header().Set("Service-Worker-Allowed", "/panel/")
	}
	_, _ = w.Write(data)
}

func (s *relayServer) allowPanelAttempt(r *http.Request) bool {
	now, key := s.now(), "panel:"+remoteIP(r)
	s.attemptMu.Lock()
	defer s.attemptMu.Unlock()
	window := s.attempts[key]
	if window.started.IsZero() || now.Sub(window.started) >= 10*time.Minute {
		window = attemptWindow{started: now}
	}
	window.count++
	s.attempts[key] = window
	return window.count <= maximumPairAttempts
}

func (s *relayServer) panelLogin(w http.ResponseWriter, r *http.Request) {
	if !s.allowPanelAttempt(r) {
		writeError(w, http.StatusTooManyRequests, "登录尝试过多，请稍后再试")
		return
	}
	var request struct {
		Password string `json:"password"`
		OTP      string `json:"otp"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if len(request.Password) > 512 {
		writeError(w, http.StatusUnauthorized, "登录失败")
		return
	}
	digest := sha256.Sum256([]byte(strings.TrimSpace(request.Password)))
	if subtle.ConstantTimeCompare(digest[:], s.panelHash[:]) != 1 || !s.validPanelTOTP(request.OTP) {
		time.Sleep(250 * time.Millisecond)
		writeError(w, http.StatusUnauthorized, "登录失败")
		return
	}
	token, err := randomToken(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "会话创建失败")
		return
	}
	csrf, err := randomToken(24)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "会话创建失败")
		return
	}
	expires := s.now().Add(panelSessionLifetime).UTC()
	s.panelMu.Lock()
	for key, session := range s.panels {
		if !s.now().Before(session.Expires) {
			delete(s.panels, key)
		}
	}
	s.panels[tokenHash(token)] = panelSession{CSRF: csrf, Expires: expires}
	s.panelMu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: panelCookieName, Value: token, Path: "/panel/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, Expires: expires, MaxAge: int(panelSessionLifetime.Seconds())})
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "csrf": csrf, "expires_at": expires})
}

func (s *relayServer) panelSessionFor(r *http.Request) (panelSession, bool) {
	cookie, err := r.Cookie(panelCookieName)
	if err != nil || cookie.Value == "" {
		return panelSession{}, false
	}
	key := tokenHash(cookie.Value)
	s.panelMu.Lock()
	defer s.panelMu.Unlock()
	session, ok := s.panels[key]
	if !ok || !s.now().Before(session.Expires) {
		delete(s.panels, key)
		return panelSession{}, false
	}
	return session, true
}

func (s *relayServer) requirePanel(w http.ResponseWriter, r *http.Request, write bool) (panelSession, bool) {
	session, ok := s.panelSessionFor(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "需要登录")
		return panelSession{}, false
	}
	if write {
		provided := strings.TrimSpace(r.Header.Get("X-CSRF-Token"))
		if provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(session.CSRF)) != 1 {
			writeError(w, http.StatusForbidden, "CSRF 校验失败")
			return panelSession{}, false
		}
	}
	return session, true
}

func (s *relayServer) panelSessionInfo(w http.ResponseWriter, r *http.Request) {
	session, ok := s.requirePanel(w, r, false)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "csrf": session.CSRF, "expires_at": session.Expires})
}

func (s *relayServer) panelLogout(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePanel(w, r, true); !ok {
		return
	}
	if cookie, err := r.Cookie(panelCookieName); err == nil {
		s.panelMu.Lock()
		delete(s.panels, tokenHash(cookie.Value))
		s.panelMu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: panelCookieName, Path: "/panel/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func panelDeviceRef(deviceID string) string {
	return tokenHash(deviceID)[:24]
}

func (s *relayServer) panelDeviceByRefLocked(ref string) (*deviceState, bool) {
	for id, device := range s.store.state.Devices {
		if panelDeviceRef(id) == ref {
			return device, true
		}
	}
	return nil, false
}

func panelDeviceSummary(device *deviceState, now time.Time) map[string]any {
	return map[string]any{
		"id": panelDeviceRef(device.ID), "name": device.Name,
		"online": now.Before(device.LeaseUntil), "host_name": device.ActiveHostName,
		"updated_at": device.UpdatedAt, "revision": device.Revision,
	}
}

func (s *relayServer) panelListDevices(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePanel(w, r, false); !ok {
		return
	}
	s.store.mu.Lock()
	devices := make([]map[string]any, 0, len(s.store.state.Devices))
	for _, device := range s.store.state.Devices {
		devices = append(devices, panelDeviceSummary(device, s.now()))
	}
	s.store.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"devices": devices})
}

func (s *relayServer) panelReadDevice(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePanel(w, r, false); !ok {
		return
	}
	s.store.mu.Lock()
	device, ok := s.panelDeviceByRefLocked(r.PathValue("device"))
	if !ok {
		s.store.mu.Unlock()
		writeError(w, http.StatusNotFound, "设备不存在")
		return
	}
	var snapshot any
	if len(device.Snapshot) > 0 {
		_ = json.Unmarshal(device.Snapshot, &snapshot)
	}
	result := panelDeviceSummary(device, s.now())
	result["snapshot"] = snapshot
	s.store.mu.Unlock()
	writeJSON(w, http.StatusOK, result)
}

func (s *relayServer) panelCreateHostPairing(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePanel(w, r, true); !ok {
		return
	}
	code, err := randomPairingCode()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "主机授权码生成失败")
		return
	}
	expires := s.now().UTC().Add(pairingLifetime)
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	device, ok := s.panelDeviceByRefLocked(r.PathValue("device"))
	if !ok {
		writeError(w, http.StatusNotFound, "设备不存在")
		return
	}
	s.store.state.Pairings[tokenHash(normalizePairingCode(code))] = pairingRecord{DeviceID: device.ID, Scope: "host", Expires: expires}
	auditID := s.store.state.NextID
	s.store.state.NextID++
	s.store.state.Audit = append(s.store.state.Audit, relayAuditRecord{ID: auditID, DeviceID: device.ID, Action: "host_pairing", CreatedAt: s.now().UTC(), Source: "panel"})
	if len(s.store.state.Audit) > 500 {
		s.store.state.Audit = append([]relayAuditRecord(nil), s.store.state.Audit[len(s.store.state.Audit)-500:]...)
	}
	if err := s.store.saveLocked(); err != nil {
		writeError(w, http.StatusInternalServerError, "状态保存失败")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"code": code, "expires_at": expires, "scope": "host"})
}

func (s *relayServer) panelCreateCommand(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePanel(w, r, true); !ok {
		return
	}
	var request struct {
		Name    string          `json:"name"`
		Payload json.RawMessage `json:"payload"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if !allowedCommands[request.Name] {
		writeError(w, http.StatusBadRequest, "命令未开放")
		return
	}
	if err := validateRemoteCommandPayload(request.Name, request.Payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	device, ok := s.panelDeviceByRefLocked(r.PathValue("device"))
	if !ok {
		writeError(w, http.StatusNotFound, "设备不存在")
		return
	}
	command := relayCommand{ID: s.store.state.NextID, Name: request.Name, Payload: request.Payload, CreatedAt: s.now().UTC()}
	s.store.state.NextID++
	device.Commands = append(device.Commands, command)
	if len(device.Commands) > maximumCommands {
		device.Commands = append([]relayCommand(nil), device.Commands[len(device.Commands)-maximumCommands:]...)
	}
	s.store.state.Audit = append(s.store.state.Audit, relayAuditRecord{ID: command.ID, DeviceID: device.ID, Action: request.Name, CreatedAt: command.CreatedAt, Source: "panel"})
	if len(s.store.state.Audit) > 500 {
		s.store.state.Audit = append([]relayAuditRecord(nil), s.store.state.Audit[len(s.store.state.Audit)-500:]...)
	}
	if err := s.store.saveLocked(); err != nil {
		writeError(w, http.StatusInternalServerError, "状态保存失败")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": command.ID, "name": command.Name, "created_at": command.CreatedAt})
}

func (s *relayServer) panelReadCommand(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePanel(w, r, false); !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("command"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无效命令编号")
		return
	}
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	device, ok := s.panelDeviceByRefLocked(r.PathValue("device"))
	if !ok {
		writeError(w, http.StatusNotFound, "设备不存在")
		return
	}
	for _, command := range device.Commands {
		if command.ID == id {
			writeJSON(w, http.StatusOK, map[string]any{"id": command.ID, "name": command.Name, "created_at": command.CreatedAt, "done": command.Done, "result": command.Result})
			return
		}
	}
	writeError(w, http.StatusNotFound, "命令不存在")
}

func (s *relayServer) panelAudit(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePanel(w, r, false); !ok {
		return
	}
	s.store.mu.Lock()
	start := len(s.store.state.Audit) - 100
	if start < 0 {
		start = 0
	}
	audit := append([]relayAuditRecord(nil), s.store.state.Audit[start:]...)
	s.store.mu.Unlock()
	for i := range audit {
		audit[i].DeviceID = panelDeviceRef(audit[i].DeviceID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"audit": audit})
}
