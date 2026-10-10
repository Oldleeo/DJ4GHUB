package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestCommandJournalPreventsReplayAfterCrashAndDoesNotSavePayload(t *testing.T) {
	root := t.TempDir()
	m, err := newRemoteRelayManager(root)
	if err != nil {
		t.Fatal(err)
	}
	cmd := pendingRelayCommand{ID: 42, Name: "send_sms", Payload: json.RawMessage(`{"phone":"secret-number","message":"secret-content"}`)}
	if _, duplicate, err := m.reserveRemoteCommand(cmd); err != nil || duplicate {
		t.Fatalf("reserve: %v %v", duplicate, err)
	}
	restarted, err := newRemoteRelayManager(root)
	if err != nil {
		t.Fatal(err)
	}
	result, duplicate, err := restarted.reserveRemoteCommand(cmd)
	if err != nil || !duplicate || !strings.Contains(string(result), "阻止自动重试") {
		t.Fatalf("crash receipt: %s %v %v", result, duplicate, err)
	}
	if err := restarted.finishRemoteCommand(cmd, json.RawMessage(`{"ok":true,"command":"send_sms","phone":"secret-number"}`)); err != nil {
		t.Fatal(err)
	}
	if err := restarted.acknowledgeRemoteCommand(cmd.ID); err != nil {
		t.Fatal(err)
	}
	again, err := newRemoteRelayManager(root)
	if err != nil {
		t.Fatal(err)
	}
	result, duplicate, err = again.reserveRemoteCommand(cmd)
	if err != nil || !duplicate || !strings.Contains(string(result), `"ok":true`) {
		t.Fatalf("finished receipt: %s %v %v", result, duplicate, err)
	}
	saved, _ := os.ReadFile(again.journalPath())
	if strings.Contains(string(saved), "secret-number") || strings.Contains(string(saved), "secret-content") {
		t.Fatal("journal saved private payload")
	}
}

func TestCorruptCommandJournalFailsClosed(t *testing.T) {
	root := t.TempDir()
	m, err := newRemoteRelayManager(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.journalPath(), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := newRemoteRelayManager(root); err == nil {
		t.Fatal("corrupt journal discarded, enabling replay")
	}
}
