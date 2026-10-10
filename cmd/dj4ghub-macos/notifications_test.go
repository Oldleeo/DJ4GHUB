package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNotificationPublicStatusNeverReturnsSecrets(t *testing.T) {
	manager := &notificationManager{config: notificationConfig{
		Telegram: telegramForwardConfig{Enabled: true, BotToken: "123:secret", ChatID: "456"},
		Bark:     barkForwardConfig{Enabled: true, URL: "https://api.day.app/private-key", Group: "DJ4Hub"},
	}}
	raw, err := json.Marshal(manager.publicStatus())
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, secret := range []string{"123:secret", "private-key"} {
		if strings.Contains(text, secret) {
			t.Fatalf("public status leaked %q: %s", secret, text)
		}
	}
	if !strings.Contains(text, `"configured":true`) {
		t.Fatalf("public status did not report configured channels: %s", text)
	}
}

func TestNotificationUpdatePreservesStoredSecrets(t *testing.T) {
	dir := t.TempDir()
	manager := &notificationManager{
		configPath: filepath.Join(dir, "notifications.json"),
	}
	first := notificationUpdateRequest{
		TelegramEnabled:  true,
		TelegramBotToken: "123:secret",
		TelegramChatID:   "456",
		BarkEnabled:      true,
		BarkURL:          "https://api.day.app/private-key",
		BarkGroup:        "DJ4Hub",
	}
	if err := manager.update(first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.TelegramBotToken = ""
	second.BarkURL = ""
	if err := manager.update(second); err != nil {
		t.Fatal(err)
	}
	if manager.config.Telegram.BotToken != first.TelegramBotToken {
		t.Fatal("empty Telegram token did not preserve the saved secret")
	}
	if manager.config.Bark.URL != first.BarkURL {
		t.Fatal("empty Bark URL did not preserve the saved endpoint")
	}
	info, err := os.Stat(manager.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); runtime.GOOS != "windows" && got != 0o600 {
		t.Fatalf("settings permissions = %o, want 600", got)
	}
}

func TestNotificationTestMessagesReachTelegramAndBark(t *testing.T) {
	var mu sync.Mutex
	received := make(map[string]map[string]any)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		received[r.URL.Path] = body
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	manager := &notificationManager{
		config: notificationConfig{
			Telegram: telegramForwardConfig{Enabled: true, BotToken: "123:secret", ChatID: "456"},
			Bark:     barkForwardConfig{Enabled: true, URL: server.URL + "/bark-key", Group: "DJ4Hub"},
		},
		client:          server.Client(),
		telegramBaseURL: server.URL,
	}
	if err := manager.sendTest("telegram"); err != nil {
		t.Fatalf("Telegram test: %v", err)
	}
	if err := manager.sendTest("bark"); err != nil {
		t.Fatalf("Bark test: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 2 {
		t.Fatalf("received %d requests, want 2: %#v", len(received), received)
	}
	for path, body := range received {
		if path == "/bark-key" {
			if body["title"] != "DJ 4G Hub 新短信" || body["group"] != "DJ4Hub" {
				t.Fatalf("unexpected Bark payload: %#v", body)
			}
			continue
		}
		if body["chat_id"] != "456" {
			t.Fatalf("unexpected Telegram payload at %s: %#v", path, body)
		}
	}
}

func TestNotificationObserveCreatesBaselineWhenDisabled(t *testing.T) {
	dir := t.TempDir()
	manager := &notificationManager{
		ledgerPath: filepath.Join(dir, "notification-ledger.json"),
		ledger: notificationLedger{
			Deliveries: make(map[string]notificationDelivery),
		},
		queue: make(chan notificationSMS, 1),
	}
	message := notificationSMS{Key: "one", Sender: "10086", Content: "hello", Time: time.Now()}
	manager.observe(message)
	delivery := manager.ledger.Deliveries[message.Key]
	if !delivery.Baseline {
		t.Fatal("disabled forwarding did not create a no-backfill baseline")
	}
	select {
	case <-manager.queue:
		t.Fatal("disabled forwarding unexpectedly queued a notification")
	default:
	}
}

func TestNotificationBaselinePreventsHistoryBackfill(t *testing.T) {
	dir := t.TempDir()
	manager := &notificationManager{
		ledgerPath: filepath.Join(dir, "notification-ledger.json"),
		ledger: notificationLedger{
			Deliveries: make(map[string]notificationDelivery),
		},
		config: notificationConfig{
			Bark: barkForwardConfig{Enabled: true, URL: "https://api.day.app/private-key"},
		},
		queue: make(chan notificationSMS, 1),
	}
	manager.baseline("existing")
	manager.observe(notificationSMS{Key: "existing", Sender: "10086", Content: "history", Time: time.Now()})

	delivery := manager.ledger.Deliveries["existing"]
	if !delivery.Baseline {
		t.Fatal("existing SMS was not retained as a no-backfill baseline")
	}
	select {
	case <-manager.queue:
		t.Fatal("historical SMS unexpectedly queued a Bark notification")
	default:
	}
}
