package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	stateVersion        = 1
	stateMagic          = "DJ4R1"
	maxRequestBytes     = 1 << 20
	hostLeaseDuration   = 45 * time.Second
	pairingLifetime     = 10 * time.Minute
	maximumCommands     = 100
	maximumPairAttempts = 10
)

var identifierPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{3,128}$`)
var remotePhonePattern = regexp.MustCompile(`^\+?[0-9]{1,20}$`)
var remoteDTMFPattern = regexp.MustCompile(`^[0-9*#A-D]$`)

type deviceState struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	ActiveHostID   string          `json:"active_host_id"`
	ActiveHostName string          `json:"active_host_name"`
	LeaseUntil     time.Time       `json:"lease_until"`
	UpdatedAt      time.Time       `json:"updated_at"`
	Revision       int64           `json:"revision"`
	Snapshot       json.RawMessage `json:"snapshot,omitempty"`
	Commands       []relayCommand  `json:"commands,omitempty"`
}

type hostCredential struct {
	DeviceID string    `json:"device_id"`
	HostID   string    `json:"host_id"`
	Created  time.Time `json:"created"`
}

type clientCredential struct {
	DeviceID string    `json:"device_id"`
	Scope    string    `json:"scope"`
	Created  time.Time `json:"created"`
}

type pairingRecord struct {
	DeviceID string    `json:"device_id"`
	Scope    string    `json:"scope"`
	Expires  time.Time `json:"expires"`
}

type relayCommand struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	Result    json.RawMessage `json:"result,omitempty"`
	Done      bool            `json:"done"`
}

type relayAuditRecord struct {
	ID        int64     `json:"id"`
	DeviceID  string    `json:"device_id"`
	Action    string    `json:"action"`
	CreatedAt time.Time `json:"created_at"`
	Source    string    `json:"source"`
}

type persistedState struct {
	Version  int                         `json:"version"`
	NextID   int64                       `json:"next_id"`
	Devices  map[string]*deviceState     `json:"devices"`
	Hosts    map[string]hostCredential   `json:"hosts"`
	Clients  map[string]clientCredential `json:"clients"`
	Pairings map[string]pairingRecord    `json:"pairings"`
	Audit    []relayAuditRecord          `json:"audit,omitempty"`
}

type encryptedStore struct {
	mu    sync.Mutex
	path  string
	aead  cipher.AEAD
	state persistedState
}

func newEncryptedStore(path string, key []byte) (*encryptedStore, error) {
	if len(key) != 32 {
		return nil, errors.New("state encryption key must contain 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	store := &encryptedStore{path: path, aead: aead}
	store.state = persistedState{Version: stateVersion, NextID: 1, Devices: map[string]*deviceState{}, Hosts: map[string]hostCredential{}, Clients: map[string]clientCredential{}, Pairings: map[string]pairingRecord{}}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *encryptedStore) load() error {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(data) < len(stateMagic)+s.aead.NonceSize() || string(data[:len(stateMagic)]) != stateMagic {
		return errors.New("relay state has an invalid encrypted header")
	}
	nonce := data[len(stateMagic) : len(stateMagic)+s.aead.NonceSize()]
	plain, err := s.aead.Open(nil, nonce, data[len(stateMagic)+s.aead.NonceSize():], []byte(stateMagic))
	if err != nil {
		return fmt.Errorf("decrypt relay state: %w", err)
	}
	if err := json.Unmarshal(plain, &s.state); err != nil {
		return fmt.Errorf("decode relay state: %w", err)
	}
	if s.state.Version != stateVersion {
		return fmt.Errorf("unsupported relay state version %d", s.state.Version)
	}
	if s.state.Devices == nil {
		s.state.Devices = map[string]*deviceState{}
	}
	if s.state.Hosts == nil {
		s.state.Hosts = map[string]hostCredential{}
	}
	if s.state.Clients == nil {
		s.state.Clients = map[string]clientCredential{}
	}
	if s.state.Pairings == nil {
		s.state.Pairings = map[string]pairingRecord{}
	}
	if s.state.NextID < 1 {
		s.state.NextID = 1
	}
	return nil
}

func (s *encryptedStore) saveLocked() error {
	plain, err := json.Marshal(s.state)
	if err != nil {
		return err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	data := append([]byte(stateMagic), nonce...)
	data = s.aead.Seal(data, nonce, plain, []byte(stateMagic))
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	temporary := s.path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(temporary, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, s.path)
}

type attemptWindow struct {
	started time.Time
	count   int
}

type relayServer struct {
	store     *encryptedStore
	ownerHash [32]byte
	panelHash [32]byte
	panelTOTP []byte
	now       func() time.Time
	attemptMu sync.Mutex
	attempts  map[string]attemptWindow
	panelMu   sync.Mutex
	panels    map[string]panelSession
	audio     *audioHub
	stunURL   string
	turnURLs  []string
	turnKey   []byte
}

func newRelayServer(store *encryptedStore, ownerToken string) (*relayServer, error) {
	if len(strings.TrimSpace(ownerToken)) < 32 {
		return nil, errors.New("owner token is too short")
	}
	ownerHash := sha256.Sum256([]byte(strings.TrimSpace(ownerToken)))
	return &relayServer{
		store: store, ownerHash: ownerHash, panelHash: ownerHash, now: time.Now,
		attempts: map[string]attemptWindow{}, panels: map[string]panelSession{}, audio: newAudioHub(),
	}, nil
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func randomToken(bytes int) (string, error) {
	buffer := make([]byte, bytes)
	if _, err := io.ReadFull(rand.Reader, buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func randomPairingCode() (string, error) {
	const alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	buffer := make([]byte, 8)
	random := make([]byte, 8)
	if _, err := io.ReadFull(rand.Reader, random); err != nil {
		return "", err
	}
	for index := range buffer {
		buffer[index] = alphabet[int(random[index])%len(alphabet)]
	}
	return string(buffer[:4]) + "-" + string(buffer[4:]), nil
}

func normalizePairingCode(value string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(value), "-", ""))
}

func bearer(r *http.Request) string {
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(value) < 8 || !strings.EqualFold(value[:7], "Bearer ") {
		return ""
	}
	return strings.TrimSpace(value[7:])
}

func (s *relayServer) isOwner(token string) bool {
	digest := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return subtle.ConstantTimeCompare(digest[:], s.ownerHash[:]) == 1
}

func (s *relayServer) hostCredential(r *http.Request) (hostCredential, bool) {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	credential, ok := s.store.state.Hosts[tokenHash(bearer(r))]
	return credential, ok
}

func (s *relayServer) activeHostCredential(r *http.Request) (hostCredential, bool) {
	credential, ok := s.hostCredential(r)
	if !ok {
		return hostCredential{}, false
	}
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	device := s.store.state.Devices[credential.DeviceID]
	return credential, device != nil && device.ActiveHostID == credential.HostID
}

func (s *relayServer) clientCredential(r *http.Request) (clientCredential, bool) {
	if s.isOwner(bearer(r)) {
		return clientCredential{Scope: "owner"}, true
	}
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	credential, ok := s.store.state.Clients[tokenHash(bearer(r))]
	return credential, ok
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request")
		return false
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		writeError(w, http.StatusBadRequest, "request must contain one JSON value")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func (s *relayServer) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.home)
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("POST /api/v1/hosts/register", s.registerHost)
	mux.HandleFunc("POST /api/v1/hosts/claim", s.claimHost)
	mux.HandleFunc("POST /api/v1/host/heartbeat", s.hostHeartbeat)
	mux.HandleFunc("PUT /api/v1/host/snapshot", s.updateSnapshot)
	mux.HandleFunc("GET /api/v1/host/commands", s.hostCommands)
	mux.HandleFunc("POST /api/v1/host/commands/{id}/result", s.hostCommandResult)
	mux.HandleFunc("POST /api/v1/device/pairing", s.createPairing)
	mux.HandleFunc("POST /api/v1/pairings/redeem", s.redeemPairing)
	mux.HandleFunc("GET /api/v1/device", s.readDevice)
	mux.HandleFunc("POST /api/v1/device/commands", s.createCommand)
	s.registerAudioRoutes(mux)
	s.registerPanelRoutes(mux)
	return s.securityHeaders(mux)
}

func (s *relayServer) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		w.Header().Set("Permissions-Policy", "camera=(self), geolocation=(), payment=(), usb=(), microphone=(self)")
		if strings.HasPrefix(r.URL.Path, "/panel") {
			w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; media-src 'self' blob:; worker-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		}
		next.ServeHTTP(w, r)
	})
}

var homeTemplate = template.Must(template.New("home").Parse(`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>老李 Oldlee · 4G 云控制台</title><style>*{box-sizing:border-box}body{margin:0;background:#f4f8f6;color:#18241e;font:16px -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;display:grid;min-height:100vh;place-items:center}.card{width:min(620px,calc(100% - 32px));padding:40px;border:1px solid #d8e6df;border-radius:28px;background:#fff;box-shadow:0 22px 70px rgba(30,70,51,.1);text-align:center}.avatar{width:112px;height:112px;border-radius:50%;object-fit:cover;border:4px solid #e5f3ec;background:#fff}.eyebrow{display:block;margin:18px 0 7px;color:#168a56;font-size:12px;font-weight:800;letter-spacing:.16em}h1{margin:0 0 12px;font-size:32px}p{margin:0 auto 24px;max-width:470px;color:#68766f;line-height:1.7}.ok{display:inline-flex;gap:8px;align-items:center;margin-bottom:24px;color:#168a56;font-weight:700}.dot{width:9px;height:9px;border-radius:50%;background:#24b873;box-shadow:0 0 0 5px #e2f6ec}.button{display:inline-block;padding:13px 24px;border-radius:13px;background:#158955;color:#fff;font-weight:800;text-decoration:none}.copyright{display:block;margin-top:26px;color:#96a29c;font-size:11px}</style></head><body><main class="card"><img class="avatar" src="/panel/oldlee-avatar.png" alt="老李 Oldlee IP 形象"><span class="eyebrow">OLDLEE PRIVATE CLOUD</span><h1>老李 Oldlee · 4G 云控制台</h1><p>远程管理连接在 Windows 主机上的大疆 4G 模块：短信、电话、eSIM、卡片与网络。</p><div class="ok"><span class="dot"></span>云端中继服务在线</div><br><a class="button" href="/panel/">进入安全控制台</a><small class="copyright">© 2026 老李 Oldlee. All rights reserved.</small></main></body></html>`))

func (s *relayServer) home(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = homeTemplate.Execute(w, nil)
}

func (s *relayServer) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "service": "dj4ghub-relay", "version": stateVersion, "time": s.now().UTC()})
}

func (s *relayServer) registerHost(w http.ResponseWriter, r *http.Request) {
	if !s.isOwner(bearer(r)) {
		writeError(w, http.StatusUnauthorized, "owner authorization required")
		return
	}
	var request struct {
		DeviceID   string `json:"device_id"`
		DeviceName string `json:"device_name"`
		HostID     string `json:"host_id"`
		HostName   string `json:"host_name"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if !identifierPattern.MatchString(request.DeviceID) || !identifierPattern.MatchString(request.HostID) {
		writeError(w, http.StatusBadRequest, "invalid device or host identifier")
		return
	}
	token, err := randomToken(32)
	if err != nil {
		writeError(w, 500, "token generation failed")
		return
	}
	now := s.now().UTC()
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	device := s.store.state.Devices[request.DeviceID]
	if device == nil {
		device = &deviceState{ID: request.DeviceID}
		s.store.state.Devices[request.DeviceID] = device
	}
	device.Name = strings.TrimSpace(request.DeviceName)
	device.ActiveHostID, device.ActiveHostName = request.HostID, strings.TrimSpace(request.HostName)
	device.LeaseUntil, device.UpdatedAt = now.Add(hostLeaseDuration), now
	s.store.state.Hosts[tokenHash(token)] = hostCredential{DeviceID: request.DeviceID, HostID: request.HostID, Created: now}
	if err := s.store.saveLocked(); err != nil {
		writeError(w, 500, "state save failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"host_token": token, "device_id": request.DeviceID, "lease_seconds": int(hostLeaseDuration.Seconds())})
}

func (s *relayServer) hostHeartbeat(w http.ResponseWriter, r *http.Request) {
	credential, ok := s.activeHostCredential(r)
	if !ok {
		writeError(w, http.StatusConflict, "this computer is no longer the active device host")
		return
	}
	now := s.now().UTC()
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	device := s.store.state.Devices[credential.DeviceID]
	if device == nil {
		writeError(w, 404, "device not found")
		return
	}
	device.ActiveHostID, device.LeaseUntil, device.UpdatedAt = credential.HostID, now.Add(hostLeaseDuration), now
	if err := s.store.saveLocked(); err != nil {
		writeError(w, 500, "state save failed")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "lease_until": device.LeaseUntil})
}

func (s *relayServer) updateSnapshot(w http.ResponseWriter, r *http.Request) {
	credential, ok := s.activeHostCredential(r)
	if !ok {
		writeError(w, http.StatusConflict, "this computer is no longer the active device host")
		return
	}
	var request struct {
		Snapshot json.RawMessage `json:"snapshot"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if len(request.Snapshot) == 0 || !json.Valid(request.Snapshot) {
		writeError(w, 400, "snapshot must be valid JSON")
		return
	}
	now := s.now().UTC()
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	device := s.store.state.Devices[credential.DeviceID]
	if device == nil {
		writeError(w, 404, "device not found")
		return
	}
	device.ActiveHostID, device.LeaseUntil, device.UpdatedAt = credential.HostID, now.Add(hostLeaseDuration), now
	device.Revision++
	device.Snapshot = append(device.Snapshot[:0], request.Snapshot...)
	if err := s.store.saveLocked(); err != nil {
		writeError(w, 500, "state save failed")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "revision": device.Revision, "lease_until": device.LeaseUntil})
}

func (s *relayServer) authorizeDevice(r *http.Request) (string, string, bool) {
	credential, ok := s.clientCredential(r)
	if !ok {
		return "", "", false
	}
	if credential.Scope == "owner" {
		deviceID := strings.TrimSpace(r.URL.Query().Get("device_id"))
		return deviceID, "owner", identifierPattern.MatchString(deviceID)
	}
	return credential.DeviceID, credential.Scope, true
}

func (s *relayServer) createPairing(w http.ResponseWriter, r *http.Request) {
	deviceID, scope, authorized := s.authorizeDevice(r)
	if !authorized {
		if host, ok := s.hostCredential(r); ok {
			deviceID, scope, authorized = host.DeviceID, "control", true
		}
	}
	if !authorized || scope == "read" {
		writeError(w, 401, "device owner or host authorization required")
		return
	}
	var request struct {
		Scope string `json:"scope"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if request.Scope == "" {
		request.Scope = "read"
	}
	if request.Scope != "read" && request.Scope != "control" && request.Scope != "host" {
		writeError(w, 400, "scope must be read, control, or host")
		return
	}
	code, err := randomPairingCode()
	if err != nil {
		writeError(w, 500, "pairing generation failed")
		return
	}
	expires := s.now().UTC().Add(pairingLifetime)
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	s.store.state.Pairings[tokenHash(normalizePairingCode(code))] = pairingRecord{DeviceID: deviceID, Scope: request.Scope, Expires: expires}
	if err := s.store.saveLocked(); err != nil {
		writeError(w, 500, "state save failed")
		return
	}
	writeJSON(w, 201, map[string]any{"code": code, "expires_at": expires, "scope": request.Scope})
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func (s *relayServer) allowPairAttempt(r *http.Request) bool {
	now, key := s.now(), remoteIP(r)
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

func (s *relayServer) redeemPairing(w http.ResponseWriter, r *http.Request) {
	if !s.allowPairAttempt(r) {
		writeError(w, 429, "too many pairing attempts")
		return
	}
	var request struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	key := tokenHash(normalizePairingCode(request.Code))
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	pairing, ok := s.store.state.Pairings[key]
	if !ok || !s.now().Before(pairing.Expires) {
		delete(s.store.state.Pairings, key)
		writeError(w, 400, "pairing code is invalid or expired")
		return
	}
	if pairing.Scope == "host" {
		writeError(w, 400, "use the device-host claim endpoint for this pairing code")
		return
	}
	delete(s.store.state.Pairings, key)
	token, err := randomToken(32)
	if err != nil {
		writeError(w, 500, "token generation failed")
		return
	}
	s.store.state.Clients[tokenHash(token)] = clientCredential{DeviceID: pairing.DeviceID, Scope: pairing.Scope, Created: s.now().UTC()}
	if err := s.store.saveLocked(); err != nil {
		writeError(w, 500, "state save failed")
		return
	}
	writeJSON(w, 201, map[string]any{"client_token": token, "device_id": pairing.DeviceID, "scope": pairing.Scope})
}

func (s *relayServer) claimHost(w http.ResponseWriter, r *http.Request) {
	if !s.allowPairAttempt(r) {
		writeError(w, http.StatusTooManyRequests, "too many pairing attempts")
		return
	}
	var request struct {
		Code     string `json:"code"`
		HostID   string `json:"host_id"`
		HostName string `json:"host_name"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if !identifierPattern.MatchString(request.HostID) {
		writeError(w, http.StatusBadRequest, "invalid host identifier")
		return
	}
	key := tokenHash(normalizePairingCode(request.Code))
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	pairing, ok := s.store.state.Pairings[key]
	if !ok || pairing.Scope != "host" || !s.now().Before(pairing.Expires) {
		delete(s.store.state.Pairings, key)
		writeError(w, http.StatusBadRequest, "device-host pairing code is invalid or expired")
		return
	}
	delete(s.store.state.Pairings, key)
	device := s.store.state.Devices[pairing.DeviceID]
	if device == nil {
		writeError(w, http.StatusNotFound, "device not found")
		return
	}
	token, err := randomToken(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token generation failed")
		return
	}
	now := s.now().UTC()
	device.ActiveHostID = request.HostID
	device.ActiveHostName = strings.TrimSpace(request.HostName)
	device.LeaseUntil = now.Add(hostLeaseDuration)
	device.UpdatedAt = now
	s.store.state.Hosts[tokenHash(token)] = hostCredential{DeviceID: pairing.DeviceID, HostID: request.HostID, Created: now}
	if err := s.store.saveLocked(); err != nil {
		writeError(w, http.StatusInternalServerError, "state save failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"host_token": token, "device_id": pairing.DeviceID, "lease_seconds": int(hostLeaseDuration.Seconds())})
}

func (s *relayServer) readDevice(w http.ResponseWriter, r *http.Request) {
	deviceID, _, ok := s.authorizeDevice(r)
	if !ok {
		writeError(w, 401, "device authorization required")
		return
	}
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	device := s.store.state.Devices[deviceID]
	if device == nil {
		writeError(w, 404, "device not found")
		return
	}
	online := s.now().Before(device.LeaseUntil)
	var snapshot any
	if len(device.Snapshot) > 0 {
		_ = json.Unmarshal(device.Snapshot, &snapshot)
	}
	writeJSON(w, 200, map[string]any{"device_id": device.ID, "name": device.Name, "online": online, "host_name": device.ActiveHostName, "updated_at": device.UpdatedAt, "revision": device.Revision, "snapshot": snapshot})
}

var allowedCommands = map[string]bool{
	"refresh_status":         true,
	"refresh_sms":            true,
	"read_sms_status":        true,
	"clear_sms":              true,
	"delete_sms":             true,
	"answer":                 true,
	"hangup":                 true,
	"send_sms":               true,
	"dial":                   true,
	"dtmf":                   true,
	"read_history":           true,
	"read_network":           true,
	"read_network_local":     true,
	"read_network_activity":  true,
	"read_network_traffic":   true,
	"check_4g":               true,
	"check_proxy":            true,
	"enable_network":         true,
	"save_apn":               true,
	"set_usbnet":             true,
	"reboot_module":          true,
	"execute_at":             true,
	"read_esim":              true,
	"read_esim_health":       true,
	"probe_esim_phonebook":   true,
	"read_esim_notes":        true,
	"save_esim_note":         true,
	"read_module_esim_notes": true,
	"save_module_esim_note":  true,
	"switch_esim":            true,
	"rename_esim":            true,
	"delete_esim":            true,
	"download_esim":          true,
	"read_notifications":     true,
	"save_notifications":     true,
	"test_notification":      true,
	"read_retention":         true,
	"save_retention":         true,
	"delete_retention":       true,
	"complete_retention":     true,
	"verify_retention":       true,
}

func validateRemoteCommandPayload(name string, payload json.RawMessage) error {
	switch name {
	case "refresh_status", "refresh_sms", "read_sms_status", "clear_sms", "answer", "hangup",
		"read_network", "read_network_local", "read_network_activity", "read_network_traffic",
		"check_4g", "check_proxy", "enable_network", "reboot_module", "read_esim",
		"read_esim_health", "probe_esim_phonebook", "read_esim_notes", "read_module_esim_notes",
		"read_notifications", "read_retention":
		if len(bytes.TrimSpace(payload)) != 0 && string(bytes.TrimSpace(payload)) != "null" && string(bytes.TrimSpace(payload)) != "{}" {
			return errors.New("this command does not accept a payload")
		}
		return nil
	case "save_retention":
		var request struct {
			ID              string `json:"id"`
			Label           string `json:"label"`
			Country         string `json:"country"`
			Operator        string `json:"operator"`
			Phone           string `json:"phone"`
			ICCID           string `json:"iccid"`
			Plan            string `json:"plan"`
			NextDue         string `json:"next_due"`
			GracePeriodDays int    `json:"grace_period_days"`
			ActionNote      string `json:"action_note"`
			SourceURL       string `json:"source_url"`
			Enabled         bool   `json:"enabled"`
		}
		if json.Unmarshal(payload, &request) != nil || strings.TrimSpace(request.Label) == "" || len([]rune(request.Label)) > 80 || len(request.ID) > 64 || len([]rune(request.Country)) > 80 || len([]rune(request.Operator)) > 100 || len([]rune(request.Phone)) > 80 || len(request.ICCID) > 32 || len([]rune(request.Plan)) > 120 || len([]rune(request.ActionNote)) > 500 || len(request.SourceURL) > 1000 || request.GracePeriodDays < 0 || request.GracePeriodDays > 730 {
			return errors.New("retention card payload is invalid")
		}
		if request.ICCID != "" && !regexp.MustCompile(`^[0-9]{10,32}$`).MatchString(request.ICCID) {
			return errors.New("retention ICCID is invalid")
		}
		if request.NextDue != "" {
			if _, err := time.Parse("2006-01-02", request.NextDue); err != nil {
				return errors.New("retention due date is invalid")
			}
		}
		if request.SourceURL != "" {
			parsed, err := url.Parse(request.SourceURL)
			if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
				return errors.New("retention source URL is invalid")
			}
		}
		return nil
	case "delete_retention", "verify_retention":
		var request struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(payload, &request) != nil || !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(strings.ToLower(strings.TrimSpace(request.ID))) {
			return errors.New("retention card id is invalid")
		}
		return nil
	case "complete_retention":
		var request struct {
			ID      string `json:"id"`
			NextDue string `json:"next_due"`
		}
		if json.Unmarshal(payload, &request) != nil || !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(strings.ToLower(strings.TrimSpace(request.ID))) {
			return errors.New("retention completion payload is invalid")
		}
		if _, err := time.Parse("2006-01-02", request.NextDue); err != nil {
			return errors.New("retention completion date is invalid")
		}
		return nil
	case "save_notifications":
		var request struct {
			TelegramEnabled    bool   `json:"telegram_enabled"`
			TelegramBotToken   string `json:"telegram_bot_token"`
			TelegramChatID     string `json:"telegram_chat_id"`
			ClearTelegramToken bool   `json:"clear_telegram_token"`
			BarkEnabled        bool   `json:"bark_enabled"`
			BarkURL            string `json:"bark_url"`
			BarkGroup          string `json:"bark_group"`
			ClearBarkURL       bool   `json:"clear_bark_url"`
		}
		if json.Unmarshal(payload, &request) != nil || len(request.TelegramBotToken) > 256 || len(request.TelegramChatID) > 128 || len(request.BarkURL) > 1024 || len([]rune(request.BarkGroup)) > 80 {
			return errors.New("notification settings payload is invalid")
		}
		if request.TelegramEnabled && strings.TrimSpace(request.TelegramChatID) == "" {
			return errors.New("Telegram Chat ID is required when enabled")
		}
		if request.BarkEnabled && strings.TrimSpace(request.BarkURL) != "" {
			parsed, err := url.Parse(strings.TrimSpace(request.BarkURL))
			if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), "api.day.app") {
				return errors.New("Bark URL must use https://api.day.app")
			}
		}
		return nil
	case "test_notification":
		var request struct {
			Channel string `json:"channel"`
		}
		if json.Unmarshal(payload, &request) != nil || (request.Channel != "telegram" && request.Channel != "bark") {
			return errors.New("notification test channel is invalid")
		}
		return nil
	case "dial":
		var request struct {
			Number string `json:"number"`
		}
		if len(payload) == 0 || json.Unmarshal(payload, &request) != nil || !remotePhonePattern.MatchString(request.Number) {
			return errors.New("dial requires a valid phone number")
		}
		return nil
	case "dtmf":
		var request struct {
			Digit string `json:"digit"`
		}
		if len(payload) == 0 || json.Unmarshal(payload, &request) != nil || !remoteDTMFPattern.MatchString(request.Digit) {
			return errors.New("dtmf requires one valid digit")
		}
		return nil
	case "send_sms":
		var request struct {
			Phone   string `json:"phone"`
			Message string `json:"message"`
		}
		if len(payload) == 0 || json.Unmarshal(payload, &request) != nil || !remotePhonePattern.MatchString(request.Phone) {
			return errors.New("send_sms requires a valid phone number")
		}
		message := strings.TrimSpace(request.Message)
		if message == "" || len([]rune(message)) > 2000 || len(message) > 8192 {
			return errors.New("SMS message must contain 1 to 2000 characters")
		}
		return nil
	case "delete_sms":
		var request struct {
			ID string `json:"id"`
		}
		if len(payload) == 0 || json.Unmarshal(payload, &request) != nil || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(strings.ToLower(strings.TrimSpace(request.ID))) {
			return errors.New("delete_sms requires a valid SMS id")
		}
		return nil
	case "read_history":
		var request struct {
			Kind   string `json:"kind"`
			Offset int    `json:"offset"`
		}
		if len(payload) > 0 && string(bytes.TrimSpace(payload)) != "{}" && json.Unmarshal(payload, &request) != nil {
			return errors.New("read_history payload is invalid")
		}
		if request.Kind != "" && request.Kind != "sms" && request.Kind != "call" {
			return errors.New("history kind must be sms or call")
		}
		if request.Offset < 0 || request.Offset > 1_000_000 {
			return errors.New("history offset is invalid")
		}
		return nil
	case "save_apn":
		var request struct {
			APN string `json:"apn"`
			PDN string `json:"pdn"`
		}
		if json.Unmarshal(payload, &request) != nil || !regexp.MustCompile(`^[A-Za-z0-9.-]{1,100}$`).MatchString(request.APN) {
			return errors.New("save_apn requires a valid APN")
		}
		if request.PDN != "IP" && request.PDN != "IPV4V6" && request.PDN != "IPV6" {
			return errors.New("save_apn requires a valid PDN type")
		}
		return nil
	case "set_usbnet":
		var request struct {
			Mode int `json:"mode"`
		}
		if json.Unmarshal(payload, &request) != nil || request.Mode < 0 || request.Mode > 3 {
			return errors.New("set_usbnet mode must be 0, 1, 2 or 3")
		}
		return nil
	case "execute_at":
		var request struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(payload, &request) != nil {
			return errors.New("execute_at requires a command")
		}
		command := strings.TrimSpace(request.Command)
		if len(command) < 2 || len(command) > 512 || !strings.HasPrefix(strings.ToUpper(command), "AT") || strings.ContainsAny(command, "\r\n\x00") {
			return errors.New("AT command is invalid")
		}
		return nil
	case "switch_esim", "delete_esim":
		var request struct {
			ICCID string `json:"iccid"`
			AID   string `json:"aid"`
		}
		if json.Unmarshal(payload, &request) != nil || !regexp.MustCompile(`^[0-9]{6,32}$`).MatchString(strings.TrimSpace(request.ICCID)) || len(request.AID) > 128 {
			return errors.New("eSIM profile identity is invalid")
		}
		return nil
	case "rename_esim":
		var request struct {
			ICCID string `json:"iccid"`
			AID   string `json:"aid"`
			Name  string `json:"name"`
		}
		if json.Unmarshal(payload, &request) != nil || !regexp.MustCompile(`^[0-9]{6,32}$`).MatchString(strings.TrimSpace(request.ICCID)) || strings.TrimSpace(request.Name) == "" || len([]rune(request.Name)) > 80 || len(request.AID) > 128 {
			return errors.New("eSIM rename payload is invalid")
		}
		return nil
	case "download_esim":
		var request struct {
			SMDP             string `json:"smdp"`
			MatchingID       string `json:"matching_id"`
			ConfirmationCode string `json:"confirmation_code"`
			AID              string `json:"aid"`
			IMEI             string `json:"imei"`
		}
		if json.Unmarshal(payload, &request) != nil || strings.TrimSpace(request.SMDP) == "" || len(request.SMDP) > 255 || !regexp.MustCompile(`^[0-9]{14,17}$`).MatchString(strings.TrimSpace(request.IMEI)) || len(request.MatchingID) > 255 || len(request.ConfirmationCode) > 128 || len(request.AID) > 128 {
			return errors.New("eSIM download payload is invalid")
		}
		return nil
	case "save_esim_note", "save_module_esim_note":
		var request struct {
			ICCID string `json:"iccid"`
			Label string `json:"label"`
			Phone string `json:"phone"`
			Tags  string `json:"tags"`
		}
		if json.Unmarshal(payload, &request) != nil || strings.TrimSpace(request.ICCID) == "" || len(request.ICCID) > 64 || len([]rune(request.Label)) > 80 || len([]rune(request.Phone)) > 80 || len([]rune(request.Tags)) > 200 {
			return errors.New("eSIM note payload is invalid")
		}
		return nil
	default:
		return errors.New("command is not enabled")
	}
}

func (s *relayServer) createCommand(w http.ResponseWriter, r *http.Request) {
	deviceID, scope, ok := s.authorizeDevice(r)
	if !ok || (scope != "control" && scope != "owner") {
		writeError(w, 403, "control authorization required")
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
		writeError(w, 400, "command is not enabled")
		return
	}
	if len(request.Payload) > 0 && !json.Valid(request.Payload) {
		writeError(w, 400, "payload must be valid JSON")
		return
	}
	if err := validateRemoteCommandPayload(request.Name, request.Payload); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	device := s.store.state.Devices[deviceID]
	if device == nil {
		writeError(w, 404, "device not found")
		return
	}
	command := relayCommand{ID: s.store.state.NextID, Name: request.Name, Payload: request.Payload, CreatedAt: s.now().UTC()}
	s.store.state.NextID++
	device.Commands = append(device.Commands, command)
	if len(device.Commands) > maximumCommands {
		device.Commands = append([]relayCommand(nil), device.Commands[len(device.Commands)-maximumCommands:]...)
	}
	if err := s.store.saveLocked(); err != nil {
		writeError(w, 500, "state save failed")
		return
	}
	writeJSON(w, 201, command)
}

func (s *relayServer) hostCommands(w http.ResponseWriter, r *http.Request) {
	credential, ok := s.activeHostCredential(r)
	if !ok {
		writeError(w, http.StatusConflict, "this computer is no longer the active device host")
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	waitSeconds, _ := strconv.Atoi(r.URL.Query().Get("wait"))
	if waitSeconds < 0 {
		waitSeconds = 0
	}
	if waitSeconds > 25 {
		waitSeconds = 25
	}
	deadline := time.Now().Add(time.Duration(waitSeconds) * time.Second)
	for {
		s.store.mu.Lock()
		device := s.store.state.Devices[credential.DeviceID]
		var commands []relayCommand
		if device != nil {
			for _, command := range device.Commands {
				if command.ID > after && !command.Done {
					commands = append(commands, command)
				}
			}
		}
		s.store.mu.Unlock()
		if len(commands) > 0 || time.Now().After(deadline) || waitSeconds == 0 {
			writeJSON(w, 200, map[string]any{"commands": commands})
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func (s *relayServer) hostCommandResult(w http.ResponseWriter, r *http.Request) {
	credential, ok := s.activeHostCredential(r)
	if !ok {
		writeError(w, http.StatusConflict, "this computer is no longer the active device host")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "invalid command id")
		return
	}
	var request struct {
		Result json.RawMessage `json:"result"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if len(request.Result) > 0 && !json.Valid(request.Result) {
		writeError(w, 400, "result must be valid JSON")
		return
	}
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	device := s.store.state.Devices[credential.DeviceID]
	if device == nil {
		writeError(w, 404, "device not found")
		return
	}
	for index := range device.Commands {
		if device.Commands[index].ID == id {
			device.Commands[index].Done = true
			device.Commands[index].Result = request.Result
			if device.Commands[index].Name == "save_notifications" {
				// Notification credentials are needed only until the device host
				// consumes them. Do not retain them in the encrypted command history.
				device.Commands[index].Payload = nil
			}
			if err := s.store.saveLocked(); err != nil {
				writeError(w, 500, "state save failed")
				return
			}
			writeJSON(w, 200, map[string]bool{"ok": true})
			return
		}
	}
	writeError(w, 404, "command not found")
}

type certificateLoader struct {
	mu       sync.Mutex
	certPath string
	keyPath  string
	cert     *tls.Certificate
	stamp    time.Time
}

func (l *certificateLoader) getCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	info, err := os.Stat(l.certPath)
	if err != nil {
		return nil, err
	}
	if l.cert == nil || info.ModTime().After(l.stamp) {
		certificate, err := tls.LoadX509KeyPair(l.certPath, l.keyPath)
		if err != nil {
			return nil, err
		}
		l.cert, l.stamp = &certificate, info.ModTime()
	}
	return l.cert, nil
}

func loadSecret(path string, bytes int) (string, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		value := strings.TrimSpace(string(data))
		if len(value) < bytes {
			return "", fmt.Errorf("secret in %s is too short", path)
		}
		return value, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	value, err := randomToken(bytes)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	if _, err = file.WriteString(value + "\n"); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	return value, err
}

func loadTOTPSecret(path string) (string, error) {
	if data, err := os.ReadFile(path); err == nil {
		value := strings.ToUpper(strings.TrimSpace(string(data)))
		decoded, decodeErr := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(value)
		if decodeErr != nil || len(decoded) < 20 {
			return "", fmt.Errorf("invalid TOTP secret in %s", path)
		}
		return value, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	buffer := make([]byte, 20)
	if _, err := io.ReadFull(rand.Reader, buffer); err != nil {
		return "", err
	}
	value := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buffer)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	if _, err = file.WriteString(value + "\n"); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	return value, err
}

func loadPanelPassword(path string) (string, error) {
	if data, err := os.ReadFile(path); err == nil {
		value := strings.TrimSpace(string(data))
		if len([]rune(value)) < 8 || len([]rune(value)) > 512 {
			return "", fmt.Errorf("panel password in %s must be 8-512 characters", path)
		}
		return value, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return loadSecret(path, 24)
}

func main() {
	var listen, dataDir, certPath, keyPath, panelPasswordPath, panelTOTPPath, turnURLs, turnSecretPath string
	var httpOnly bool
	flag.BoolVar(&httpOnly, "http-only", false, "HTTP behind a trusted TLS reverse proxy only; never expose this port publicly")
	flag.StringVar(&listen, "listen", "127.0.0.1:18444", "TLS listen address")
	flag.StringVar(&dataDir, "data-dir", "/var/lib/dj4ghub-relay", "encrypted state directory")
	flag.StringVar(&certPath, "tls-cert", "/var/lib/dj4ghub-relay/tls/fullchain.pem", "TLS certificate")
	flag.StringVar(&keyPath, "tls-key", "/var/lib/dj4ghub-relay/tls/privkey.pem", "TLS private key")
	flag.StringVar(&panelPasswordPath, "panel-password", "", "panel password file; defaults to <data-dir>/panel.password")
	flag.StringVar(&panelTOTPPath, "panel-totp-secret", "", "base32 TOTP secret file; defaults to <data-dir>/panel.totp")
	flag.StringVar(&turnURLs, "turn-urls", "", "comma-separated TURN URLs advertised to WebRTC clients")
	flag.StringVar(&turnSecretPath, "turn-secret", "", "coturn shared-secret file used for short-lived credentials")
	flag.Parse()
	master, err := loadSecret(filepath.Join(dataDir, "master.key"), 32)
	if err != nil {
		log.Fatal(err)
	}
	masterBytes, err := hex.DecodeString(master)
	if err != nil || len(masterBytes) != 32 {
		log.Fatal("invalid master key")
	}
	owner, err := loadSecret(filepath.Join(dataDir, "owner.token"), 32)
	if err != nil {
		log.Fatal(err)
	}
	store, err := newEncryptedStore(filepath.Join(dataDir, "state.enc"), masterBytes)
	if err != nil {
		log.Fatal(err)
	}
	relay, err := newRelayServer(store, owner)
	if err != nil {
		log.Fatal(err)
	}
	if panelPasswordPath == "" {
		panelPasswordPath = filepath.Join(dataDir, "panel.password")
	}
	panelPassword, err := loadPanelPassword(panelPasswordPath)
	if err != nil {
		log.Fatal(err)
	}
	relay.setPanelPassword(panelPassword)
	if panelTOTPPath == "" {
		panelTOTPPath = filepath.Join(dataDir, "panel.totp")
	}
	panelTOTP, err := loadTOTPSecret(panelTOTPPath)
	if err != nil {
		log.Fatal(err)
	}
	if err := relay.setPanelTOTP(panelTOTP); err != nil {
		log.Fatal(err)
	}
	if err := relay.configureTURN(turnURLs, turnSecretPath); err != nil {
		log.Fatal(err)
	}
	loader := &certificateLoader{certPath: certPath, keyPath: keyPath}
	server := &http.Server{Addr: listen, Handler: relay.routes(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 35 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 32 << 10, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: loader.getCertificate}}
	log.Printf("DJ4GHUB relay listening on %s", listen)
	var serveErr error
	if httpOnly {
		log.Print("HTTP mode: HTTPS must terminate at your trusted reverse proxy; do not publish this listener")
		serveErr = server.ListenAndServe()
	} else {
		serveErr = server.ListenAndServeTLS("", "")
	}
	if !errors.Is(serveErr, http.ErrServerClosed) {
		log.Fatal(serveErr)
	}
}
