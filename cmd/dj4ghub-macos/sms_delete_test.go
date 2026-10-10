package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSMSMessageIDIsStableAndContentSensitive(t *testing.T) {
	message := receivedSMS{Sender: "10010", Content: "balance", Timestamp: time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)}
	first := smsMessageID(message)
	second := smsMessageID(message)
	if first != second || !smsMessageIDPattern.MatchString(first) {
		t.Fatalf("unstable or invalid id: %q %q", first, second)
	}
	message.Content = "other"
	if smsMessageID(message) == first {
		t.Fatal("message id did not change with content")
	}
}

func TestParseUSBATCMGLIndexAndStorage(t *testing.T) {
	if got := parseUSBATCMGLIndex(`+CMGL: 27,1,,38`); got != 27 {
		t.Fatalf("index = %d, want 27", got)
	}
	if got := parseSMSStorage(`+CPMS: "ME",2,100,"ME",2,100,"ME",2,100`); got != "ME" {
		t.Fatalf("storage = %q, want ME", got)
	}
}

func TestDeleteSMSRemovesOnlyOneDemoMessage(t *testing.T) {
	instance := newDemoApp()
	if len(instance.sms) != 2 {
		t.Fatalf("demo messages = %d, want 2", len(instance.sms))
	}
	targetID := instance.sms[0].ID
	body, _ := json.Marshal(map[string]string{"id": targetID})
	request := httptest.NewRequest(http.MethodPost, "/api/sms/delete", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	instance.routes().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if len(instance.sms) != 1 {
		t.Fatalf("remaining messages = %d, want 1", len(instance.sms))
	}
	if instance.sms[0].ID == targetID {
		t.Fatal("target message remained in inbox")
	}
}

func TestDeleteSMSRejectsUnknownID(t *testing.T) {
	instance := newDemoApp()
	body := []byte(`{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`)
	request := httptest.NewRequest(http.MethodPost, "/api/sms/delete", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	instance.routes().ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}
