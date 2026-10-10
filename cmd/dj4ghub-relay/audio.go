package main

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const audioSessionLifetime = 30 * time.Minute

type iceServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

type audioSignal struct {
	ID        int64           `json:"id"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	Sender    string          `json:"-"`
	CreatedAt time.Time       `json:"created_at"`
}

type audioSession struct {
	ID        string
	DeviceID  string
	CreatedAt time.Time
	ExpiresAt time.Time
	NextID    int64
	Signals   []audioSignal
}

type audioHub struct {
	mu       sync.Mutex
	sessions map[string]*audioSession
}

func newAudioHub() *audioHub { return &audioHub{sessions: map[string]*audioSession{}} }

func (h *audioHub) cleanupLocked(now time.Time) {
	for id, session := range h.sessions {
		if !now.Before(session.ExpiresAt) {
			delete(h.sessions, id)
		}
	}
}

func (h *audioHub) create(deviceID string, now time.Time) (*audioSession, error) {
	id, err := randomToken(16)
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cleanupLocked(now)
	for existingID, existing := range h.sessions {
		if existing.DeviceID == deviceID {
			delete(h.sessions, existingID)
		}
	}
	session := &audioSession{ID: id, DeviceID: deviceID, CreatedAt: now.UTC(), ExpiresAt: now.Add(audioSessionLifetime).UTC(), NextID: 1}
	h.sessions[id] = session
	clone := *session
	return &clone, nil
}

func (h *audioHub) get(id string, now time.Time) (*audioSession, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cleanupLocked(now)
	session, ok := h.sessions[id]
	if !ok {
		return nil, false
	}
	clone := *session
	clone.Signals = append([]audioSignal(nil), session.Signals...)
	return &clone, true
}

func (h *audioHub) list(deviceID string, now time.Time) []audioSession {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cleanupLocked(now)
	result := []audioSession{}
	for _, session := range h.sessions {
		if session.DeviceID == deviceID {
			result = append(result, audioSession{ID: session.ID, DeviceID: session.DeviceID, CreatedAt: session.CreatedAt, ExpiresAt: session.ExpiresAt})
		}
	}
	return result
}

func (h *audioHub) append(sessionID, deviceID, sender, kind string, payload json.RawMessage, now time.Time) (audioSignal, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cleanupLocked(now)
	session := h.sessions[sessionID]
	if session == nil || session.DeviceID != deviceID {
		return audioSignal{}, errors.New("audio session not found")
	}
	signal := audioSignal{ID: session.NextID, Type: kind, Payload: append(json.RawMessage(nil), payload...), Sender: sender, CreatedAt: now.UTC()}
	session.NextID++
	session.Signals = append(session.Signals, signal)
	if len(session.Signals) > 256 {
		session.Signals = append([]audioSignal(nil), session.Signals[len(session.Signals)-256:]...)
	}
	if kind == "hangup" {
		session.ExpiresAt = now.Add(30 * time.Second).UTC()
	}
	return signal, nil
}

func (h *audioHub) signals(sessionID, deviceID, receiver string, after int64, now time.Time) ([]audioSignal, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cleanupLocked(now)
	session := h.sessions[sessionID]
	if session == nil || session.DeviceID != deviceID {
		return nil, false
	}
	result := []audioSignal{}
	for _, signal := range session.Signals {
		if signal.ID > after && signal.Sender != receiver {
			result = append(result, signal)
		}
	}
	return result, true
}

func (s *relayServer) configureTURN(rawURLs, secretPath string) error {
	s.stunURL = "stun:stun.cloudflare.com:3478"
	for _, value := range strings.Split(rawURLs, ",") {
		value = strings.TrimSpace(value)
		if value != "" {
			s.turnURLs = append(s.turnURLs, value)
		}
	}
	if len(s.turnURLs) == 0 {
		return nil
	}
	if strings.TrimSpace(secretPath) == "" {
		return errors.New("TURN URLs require -turn-secret")
	}
	data, err := os.ReadFile(secretPath)
	if err != nil {
		return fmt.Errorf("read TURN secret: %w", err)
	}
	s.turnKey = []byte(strings.TrimSpace(string(data)))
	if len(s.turnKey) < 24 {
		return errors.New("TURN shared secret is too short")
	}
	return nil
}

func (s *relayServer) iceServers(sessionID string) []iceServer {
	servers := []iceServer{}
	if s.stunURL != "" {
		servers = append(servers, iceServer{URLs: []string{s.stunURL}})
	}
	if len(s.turnURLs) == 0 || len(s.turnKey) == 0 {
		return servers
	}
	username := fmt.Sprintf("%d:%s", s.now().Add(time.Hour).Unix(), sessionID)
	mac := hmac.New(sha1.New, s.turnKey)
	_, _ = mac.Write([]byte(username))
	servers = append(servers, iceServer{URLs: append([]string(nil), s.turnURLs...), Username: username, Credential: base64.StdEncoding.EncodeToString(mac.Sum(nil))})
	return servers
}

func (s *relayServer) registerAudioRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/device/audio/sessions", s.clientCreateAudioSession)
	mux.HandleFunc("GET /api/v1/device/audio/sessions/{session}/signals", s.clientReadAudioSignals)
	mux.HandleFunc("POST /api/v1/device/audio/sessions/{session}/signals", s.clientWriteAudioSignal)
	mux.HandleFunc("GET /api/v1/host/audio/sessions", s.hostAudioSessions)
	mux.HandleFunc("GET /api/v1/host/audio/sessions/{session}/signals", s.hostReadAudioSignals)
	mux.HandleFunc("POST /api/v1/host/audio/sessions/{session}/signals", s.hostWriteAudioSignal)
	mux.HandleFunc("POST /panel/api/devices/{device}/audio/sessions", s.panelCreateAudioSession)
	mux.HandleFunc("GET /panel/api/devices/{device}/audio/sessions/{session}/signals", s.panelReadAudioSignals)
	mux.HandleFunc("POST /panel/api/devices/{device}/audio/sessions/{session}/signals", s.panelWriteAudioSignal)
}

func (s *relayServer) audioSessionResponse(session *audioSession) map[string]any {
	return map[string]any{"id": session.ID, "created_at": session.CreatedAt, "expires_at": session.ExpiresAt, "ice_servers": s.iceServers(session.ID)}
}

func (s *relayServer) clientCreateAudioSession(w http.ResponseWriter, r *http.Request) {
	credential, ok := s.clientCredential(r)
	if !ok || (credential.Scope != "control" && credential.Scope != "owner") || credential.DeviceID == "" {
		writeError(w, http.StatusForbidden, "control authorization required")
		return
	}
	session, err := s.audio.create(credential.DeviceID, s.now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "audio session creation failed")
		return
	}
	writeJSON(w, http.StatusCreated, s.audioSessionResponse(session))
}

func (s *relayServer) panelCreateAudioSession(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requirePanel(w, r, true); !ok {
		return
	}
	s.store.mu.Lock()
	device, ok := s.panelDeviceByRefLocked(r.PathValue("device"))
	if !ok {
		s.store.mu.Unlock()
		writeError(w, http.StatusNotFound, "设备不存在")
		return
	}
	deviceID := device.ID
	s.store.mu.Unlock()
	session, err := s.audio.create(deviceID, s.now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "音频会话创建失败")
		return
	}
	writeJSON(w, http.StatusCreated, s.audioSessionResponse(session))
}

func (s *relayServer) hostAudioSessions(w http.ResponseWriter, r *http.Request) {
	credential, ok := s.activeHostCredential(r)
	if !ok {
		writeError(w, http.StatusConflict, "this computer is no longer the active device host")
		return
	}
	deadline := s.now().Add(time.Duration(waitSeconds(r)) * time.Second)
	for {
		sessions := s.audio.list(credential.DeviceID, s.now())
		if len(sessions) > 0 || !s.now().Before(deadline) {
			result := make([]map[string]any, 0, len(sessions))
			for i := range sessions {
				result = append(result, s.audioSessionResponse(&sessions[i]))
			}
			writeJSON(w, http.StatusOK, map[string]any{"sessions": result})
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func waitSeconds(r *http.Request) int {
	wait, _ := strconv.Atoi(r.URL.Query().Get("wait"))
	if wait < 0 {
		return 0
	}
	if wait > 25 {
		return 25
	}
	return wait
}

func readAfter(r *http.Request) int64 {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	return after
}

func decodeAudioSignal(w http.ResponseWriter, r *http.Request) (string, json.RawMessage, bool) {
	var request struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if !decodeJSON(w, r, &request) {
		return "", nil, false
	}
	allowed := map[string]bool{"offer": true, "answer": true, "candidate": true, "hangup": true}
	if !allowed[request.Type] || len(request.Payload) > 256<<10 || (len(request.Payload) > 0 && !json.Valid(request.Payload)) {
		writeError(w, http.StatusBadRequest, "invalid audio signal")
		return "", nil, false
	}
	return request.Type, request.Payload, true
}

func (s *relayServer) writeAudioSignal(w http.ResponseWriter, r *http.Request, deviceID, sender string) {
	kind, payload, ok := decodeAudioSignal(w, r)
	if !ok {
		return
	}
	signal, err := s.audio.append(r.PathValue("session"), deviceID, sender, kind, payload, s.now())
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, signal)
}

func (s *relayServer) readAudioSignals(w http.ResponseWriter, r *http.Request, deviceID, receiver string) {
	deadline := s.now().Add(time.Duration(waitSeconds(r)) * time.Second)
	for {
		signals, ok := s.audio.signals(r.PathValue("session"), deviceID, receiver, readAfter(r), s.now())
		if !ok {
			writeError(w, http.StatusNotFound, "audio session not found")
			return
		}
		if len(signals) > 0 || !s.now().Before(deadline) {
			writeJSON(w, http.StatusOK, map[string]any{"signals": signals})
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func (s *relayServer) clientAudioDevice(r *http.Request) (string, bool) {
	credential, ok := s.clientCredential(r)
	return credential.DeviceID, ok && (credential.Scope == "control" || credential.Scope == "owner") && credential.DeviceID != ""
}

func (s *relayServer) clientReadAudioSignals(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := s.clientAudioDevice(r)
	if !ok {
		writeError(w, http.StatusForbidden, "control authorization required")
		return
	}
	s.readAudioSignals(w, r, deviceID, "client")
}

func (s *relayServer) clientWriteAudioSignal(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := s.clientAudioDevice(r)
	if !ok {
		writeError(w, http.StatusForbidden, "control authorization required")
		return
	}
	s.writeAudioSignal(w, r, deviceID, "client")
}

func (s *relayServer) hostReadAudioSignals(w http.ResponseWriter, r *http.Request) {
	credential, ok := s.activeHostCredential(r)
	if !ok {
		writeError(w, http.StatusConflict, "active host authorization required")
		return
	}
	s.readAudioSignals(w, r, credential.DeviceID, "host")
}

func (s *relayServer) hostWriteAudioSignal(w http.ResponseWriter, r *http.Request) {
	credential, ok := s.activeHostCredential(r)
	if !ok {
		writeError(w, http.StatusConflict, "active host authorization required")
		return
	}
	s.writeAudioSignal(w, r, credential.DeviceID, "host")
}

func (s *relayServer) panelAudioDevice(w http.ResponseWriter, r *http.Request) (string, bool) {
	if _, ok := s.requirePanel(w, r, r.Method != http.MethodGet); !ok {
		return "", false
	}
	s.store.mu.Lock()
	device, ok := s.panelDeviceByRefLocked(r.PathValue("device"))
	if ok {
		deviceID := device.ID
		s.store.mu.Unlock()
		return deviceID, true
	}
	s.store.mu.Unlock()
	writeError(w, http.StatusNotFound, "设备不存在")
	return "", false
}

func (s *relayServer) panelReadAudioSignals(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := s.panelAudioDevice(w, r)
	if ok {
		s.readAudioSignals(w, r, deviceID, "client")
	}
}

func (s *relayServer) panelWriteAudioSignal(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := s.panelAudioDevice(w, r)
	if ok {
		s.writeAudioSignal(w, r, deviceID, "client")
	}
}
