package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadOnlyGuardAllowsSafeMethods(t *testing.T) {
	app := &app{readOnly: true}
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		t.Run(method, func(t *testing.T) {
			called := false
			handler := app.readOnlyGuard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusNoContent)
			}))
			request := httptest.NewRequest(method, "/api/status", nil)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if !called {
				t.Fatalf("%s request did not reach the wrapped handler", method)
			}
			if response.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
			}
		})
	}
}

func TestReadOnlyGuardBlocksStateChangingMethods(t *testing.T) {
	app := &app{readOnly: true}
	handler := app.readOnlyGuard(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("state-changing request reached the wrapped handler")
	}))

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			request := httptest.NewRequest(method, "/api/esim/switch", nil)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
			}
			if !strings.Contains(response.Body.String(), `"read_only":true`) {
				t.Fatalf("response does not identify read-only mode: %s", response.Body.String())
			}
		})
	}
}

func TestReadOnlyGuardIsTransparentWhenDisabled(t *testing.T) {
	app := &app{}
	called := false
	handler := app.readOnlyGuard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusAccepted)
	}))
	request := httptest.NewRequest(http.MethodPost, "/api/sms/send", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if !called {
		t.Fatal("request did not reach the wrapped handler")
	}
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusAccepted)
	}
}

func TestReadOnlyGuardCanAllowOnlyNotificationSettings(t *testing.T) {
	app := &app{readOnly: true, allowNotificationSettings: true}
	called := false
	handler := app.readOnlyGuard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))

	allowed := httptest.NewRecorder()
	handler.ServeHTTP(allowed, httptest.NewRequest(http.MethodPut, "/api/notifications", nil))
	if !called || allowed.Code != http.StatusNoContent {
		t.Fatalf("notification settings request was not allowed: called=%v status=%d", called, allowed.Code)
	}

	for _, path := range []string{"/api/notifications/test", "/api/sms/send", "/api/esim/download"} {
		called = false
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, nil))
		if called || response.Code != http.StatusForbidden {
			t.Fatalf("%s escaped read-only guard: called=%v status=%d", path, called, response.Code)
		}
	}
}
