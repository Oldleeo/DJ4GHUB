package uiassets

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSharedPresentationAssets(t *testing.T) {
	for _, name := range []string{"spatial.css", "spatial.js"} {
		response := httptest.NewRecorder()
		Serve(response, httptest.NewRequest(http.MethodGet, "/panel/"+name, nil))
		if response.Code != http.StatusOK || response.Body.Len() < 1000 || response.Header().Get("Cache-Control") != "no-cache" {
			t.Fatalf("missing shared UI asset %s", name)
		}
		if name == "spatial.js" {
			for _, forbidden := range []string{"fetch(", "XMLHttpRequest", "WebSocket(", "postMessage("} {
				if strings.Contains(response.Body.String(), forbidden) {
					t.Fatalf("presentation layer must not issue commands: %s", forbidden)
				}
			}
		}
	}
	response := httptest.NewRecorder()
	Serve(response, httptest.NewRequest(http.MethodGet, "/private-data.json", nil))
	if response.Code != http.StatusNotFound {
		t.Fatal("shared UI asset handler must reject other files")
	}
}
