package main

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
)

var remoteAudioSessionPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

func (a *app) remoteHostToken(w http.ResponseWriter) (string, bool) {
	if a.remote == nil {
		writeError(w, http.StatusServiceUnavailable, "远程中继未初始化")
		return "", false
	}
	a.remote.mu.Lock()
	token := a.remote.config.HostToken
	enabled := a.remote.config.HostEnabled
	a.remote.mu.Unlock()
	if !enabled || token == "" {
		writeError(w, http.StatusConflict, "本机尚未注册为远程设备主机")
		return "", false
	}
	return token, true
}

func remoteWaitQuery(r *http.Request) string {
	wait, _ := strconv.Atoi(r.URL.Query().Get("wait"))
	if wait < 0 {
		wait = 0
	}
	if wait > 15 {
		wait = 15
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	return "?wait=" + strconv.Itoa(wait) + "&after=" + strconv.FormatInt(after, 10)
}

func (a *app) remoteAudioSessions(w http.ResponseWriter, r *http.Request) {
	token, ok := a.remoteHostToken(w)
	if !ok {
		return
	}
	var response map[string]any
	if err := a.remote.relayJSON(r.Context(), http.MethodGet, "/api/v1/host/audio/sessions"+remoteWaitQuery(r), token, nil, &response); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *app) remoteAudioSignals(w http.ResponseWriter, r *http.Request) {
	token, ok := a.remoteHostToken(w)
	if !ok {
		return
	}
	sessionID := r.PathValue("session")
	if !remoteAudioSessionPattern.MatchString(sessionID) {
		writeError(w, http.StatusBadRequest, "无效音频会话")
		return
	}
	path := "/api/v1/host/audio/sessions/" + sessionID + "/signals"
	if r.Method == http.MethodGet {
		var response map[string]any
		if err := a.remote.relayJSON(r.Context(), http.MethodGet, path+remoteWaitQuery(r), token, nil, &response); err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, response)
		return
	}
	var request struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	allowed := map[string]bool{"answer": true, "candidate": true, "hangup": true}
	if !allowed[request.Type] || len(request.Payload) > 256<<10 || (len(request.Payload) > 0 && !json.Valid(request.Payload)) {
		writeError(w, http.StatusBadRequest, "无效音频信令")
		return
	}
	var response map[string]any
	if err := a.remote.relayJSON(r.Context(), http.MethodPost, path, token, request, &response); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, response)
}
