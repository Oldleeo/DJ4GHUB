package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRemoteClientGuardAllowsReadsAndRemoteMutations(t *testing.T) {
	instance := &app{remoteClient: true}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := instance.remoteClientGuard(next)

	for _, test := range []struct {
		method string
		path   string
		want   int
	}{
		{method: http.MethodGet, path: "/api/status", want: http.StatusNoContent},
		{method: http.MethodPost, path: "/api/remote/redeem", want: http.StatusNoContent},
		{method: http.MethodDelete, path: "/api/remote/client", want: http.StatusNoContent},
		{method: http.MethodPut, path: "/api/remote/host", want: http.StatusForbidden},
		{method: http.MethodPost, path: "/api/remote/pairing", want: http.StatusForbidden},
		{method: http.MethodPost, path: "/api/remote/host-claim", want: http.StatusForbidden},
		{method: http.MethodPost, path: "/api/sms/send", want: http.StatusForbidden},
		{method: http.MethodPost, path: "/api/calls", want: http.StatusForbidden},
	} {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d", response.Code, test.want)
			}
		})
	}
}
