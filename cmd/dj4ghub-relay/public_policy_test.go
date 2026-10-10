package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestPublicPanelAllowsOwnQRScannerButNotUSBOrGeolocation(t *testing.T) {
	_, handler, _ := testRelay(t)
	response := request(t, handler, http.MethodGet, "/panel/", "", nil)
	policy := response.Header().Get("Permissions-Policy")
	for _, required := range []string{"camera=(self)", "microphone=(self)", "usb=()", "geolocation=()", "payment=()"} {
		if !strings.Contains(policy, required) {
			t.Fatalf("missing required feature policy %q", required)
		}
	}
}
