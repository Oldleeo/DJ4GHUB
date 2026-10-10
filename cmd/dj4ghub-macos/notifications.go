package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const notificationLedgerLimit = 2000

type notificationConfig struct {
	Telegram telegramForwardConfig `json:"telegram"`
	Bark     barkForwardConfig     `json:"bark"`
}

type telegramForwardConfig struct {
	Enabled  bool   `json:"enabled"`
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
}

type barkForwardConfig struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
	Group   string `json:"group"`
}

type notificationUpdateRequest struct {
	TelegramEnabled    bool   `json:"telegram_enabled"`
	TelegramBotToken   string `json:"telegram_bot_token"`
	TelegramChatID     string `json:"telegram_chat_id"`
	ClearTelegramToken bool   `json:"clear_telegram_token"`
	BarkEnabled        bool   `json:"bark_enabled"`
	BarkURL            string `json:"bark_url"`
	BarkGroup          string `json:"bark_group"`
	ClearBarkURL       bool   `json:"clear_bark_url"`
}

type notificationChannelStatus struct {
	Enabled    bool   `json:"enabled"`
	Configured bool   `json:"configured"`
	ChatID     string `json:"chat_id,omitempty"`
	Group      string `json:"group,omitempty"`
	LastError  string `json:"last_error,omitempty"`
}

type notificationPublicStatus struct {
	Telegram notificationChannelStatus `json:"telegram"`
	Bark     notificationChannelStatus `json:"bark"`
}

type notificationDelivery struct {
	CreatedAt         time.Time `json:"created_at"`
	Baseline          bool      `json:"baseline,omitempty"`
	TelegramRequired  bool      `json:"telegram_required,omitempty"`
	TelegramDelivered bool      `json:"telegram_delivered,omitempty"`
	BarkRequired      bool      `json:"bark_required,omitempty"`
	BarkDelivered     bool      `json:"bark_delivered,omitempty"`
}

type notificationLedger struct {
	Deliveries map[string]notificationDelivery `json:"deliveries"`
}

type notificationSMS struct {
	Key     string
	Sender  string
	Content string
	Card    string
	Time    time.Time
}

type notificationManager struct {
	mu              sync.RWMutex
	configPath      string
	ledgerPath      string
	config          notificationConfig
	ledger          notificationLedger
	telegramError   string
	barkError       string
	client          *http.Client
	telegramBaseURL string
	queue           chan notificationSMS
}

func (a *app) initNotifications() error {
	if a.notifications != nil {
		return nil
	}
	manager, err := newNotificationManager(a.dataDir)
	if err != nil {
		return err
	}
	a.notifications = manager
	return nil
}

func newNotificationManager(dataDir string) (*notificationManager, error) {
	root := strings.TrimSpace(dataDir)
	if root == "" {
		var err error
		root, err = os.UserConfigDir()
		if err != nil {
			return nil, err
		}
	}
	root = filepath.Join(root, "DJ4Hub")
	manager := &notificationManager{
		configPath: filepath.Join(root, "notifications.json"),
		ledgerPath: filepath.Join(root, "notification-ledger.json"),
		ledger: notificationLedger{
			Deliveries: make(map[string]notificationDelivery),
		},
		client:          &http.Client{Timeout: 10 * time.Second},
		telegramBaseURL: "https://api.telegram.org",
		queue:           make(chan notificationSMS, 128),
	}
	if err := loadOptionalJSON(manager.configPath, &manager.config); err != nil {
		return nil, fmt.Errorf("load notification settings: %w", err)
	}
	if err := loadOptionalJSON(manager.ledgerPath, &manager.ledger); err != nil {
		return nil, fmt.Errorf("load notification ledger: %w", err)
	}
	if manager.ledger.Deliveries == nil {
		manager.ledger.Deliveries = make(map[string]notificationDelivery)
	}
	go manager.run()
	return manager, nil
}

func loadOptionalJSON(path string, destination any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, destination); err != nil {
		return err
	}
	return nil
}

func savePrivateJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(temporary, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func (a *app) forwardSMS(messages []receivedSMS, identity string) {
	if (a.readOnly && !a.allowNotificationForwarding) || a.notifications == nil {
		return
	}
	for _, message := range messages {
		card := "当前 SIM"
		if message.Memory == "SM" {
			card = maskedCardIdentity(identity)
		}
		a.notifications.observe(notificationSMS{
			Key:     notificationSMSKey(message, identity),
			Sender:  message.Sender,
			Content: message.Content,
			Card:    card,
			Time:    message.Timestamp,
		})
	}
}

func (a *app) baselineCurrentSMS() {
	if a.notifications == nil {
		return
	}
	a.smsMu.RLock()
	messages := append([]receivedSMS(nil), a.sms...)
	a.smsMu.RUnlock()
	if len(messages) == 0 {
		return
	}
	identity := a.historyIdentity()
	for _, message := range messages {
		a.notifications.baseline(notificationSMSKey(message, identity))
	}
}

func notificationSMSKey(message receivedSMS, identity string) string {
	digest := sha256.Sum256([]byte(identity + "\x00" + smsCacheKey(message)))
	return hex.EncodeToString(digest[:])
}

func maskedCardIdentity(identity string) string {
	identity = strings.TrimSpace(identity)
	if len(identity) < 4 {
		return "当前 SIM"
	}
	return "SIM · ****" + identity[len(identity)-4:]
}

func (m *notificationManager) observe(message notificationSMS) {
	m.mu.Lock()
	delivery, exists := m.ledger.Deliveries[message.Key]
	if !exists {
		delivery = notificationDelivery{
			CreatedAt:        time.Now(),
			TelegramRequired: m.config.Telegram.Enabled,
			BarkRequired:     m.config.Bark.Enabled,
		}
		delivery.Baseline = !delivery.TelegramRequired && !delivery.BarkRequired
		m.ledger.Deliveries[message.Key] = delivery
		m.trimLedgerLocked()
		if err := savePrivateJSON(m.ledgerPath, m.ledger); err != nil {
			log.Printf("notification ledger save failed: %v", err)
		}
	}
	pending := !delivery.Baseline &&
		((delivery.TelegramRequired && !delivery.TelegramDelivered) ||
			(delivery.BarkRequired && !delivery.BarkDelivered))
	m.mu.Unlock()
	if !pending {
		return
	}
	select {
	case m.queue <- message:
	default:
		log.Printf("notification queue is full; delivery will retry after restart")
	}
}

func (m *notificationManager) baseline(key string) {
	if strings.TrimSpace(key) == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.ledger.Deliveries[key]; exists {
		return
	}
	m.ledger.Deliveries[key] = notificationDelivery{
		CreatedAt: time.Now(),
		Baseline:  true,
	}
	m.trimLedgerLocked()
	if err := savePrivateJSON(m.ledgerPath, m.ledger); err != nil {
		log.Printf("notification ledger save failed: %v", err)
	}
}

func (m *notificationManager) trimLedgerLocked() {
	for len(m.ledger.Deliveries) > notificationLedgerLimit {
		oldestKey := ""
		var oldest time.Time
		for key, delivery := range m.ledger.Deliveries {
			if oldestKey == "" || delivery.CreatedAt.Before(oldest) {
				oldestKey = key
				oldest = delivery.CreatedAt
			}
		}
		delete(m.ledger.Deliveries, oldestKey)
	}
}

func (m *notificationManager) run() {
	for message := range m.queue {
		m.deliver(message)
	}
}

func (m *notificationManager) deliver(message notificationSMS) {
	m.mu.RLock()
	config := m.config
	delivery := m.ledger.Deliveries[message.Key]
	m.mu.RUnlock()

	if delivery.TelegramRequired && !delivery.TelegramDelivered {
		err := retryNotification(func() error { return m.sendTelegram(config.Telegram, message) })
		m.finishDelivery(message.Key, "telegram", err)
	}
	if delivery.BarkRequired && !delivery.BarkDelivered {
		err := retryNotification(func() error { return m.sendBark(config.Bark, message) })
		m.finishDelivery(message.Key, "bark", err)
	}
}

func retryNotification(send func() error) error {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if err = send(); err == nil {
			return nil
		}
		if attempt < 2 {
			time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
		}
	}
	return err
}

func (m *notificationManager) finishDelivery(key, channel string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delivery := m.ledger.Deliveries[key]
	if channel == "telegram" {
		if err == nil {
			delivery.TelegramDelivered = true
			m.telegramError = ""
		} else {
			m.telegramError = err.Error()
		}
	} else {
		if err == nil {
			delivery.BarkDelivered = true
			m.barkError = ""
		} else {
			m.barkError = err.Error()
		}
	}
	m.ledger.Deliveries[key] = delivery
	if err := savePrivateJSON(m.ledgerPath, m.ledger); err != nil {
		log.Printf("notification ledger save failed: %v", err)
	}
	if err != nil {
		log.Printf("%s SMS forwarding failed: %v", channel, err)
	}
}

func notificationText(message notificationSMS) string {
	timestamp := message.Time
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	return fmt.Sprintf("卡：%s\n来自：%s\n时间：%s\n\n%s",
		message.Card, message.Sender, timestamp.Format("2006-01-02 15:04:05"), message.Content)
}

func (m *notificationManager) sendTelegram(config telegramForwardConfig, message notificationSMS) error {
	if !config.Enabled || strings.TrimSpace(config.BotToken) == "" || strings.TrimSpace(config.ChatID) == "" {
		return errors.New("Telegram is not fully configured")
	}
	endpoint := strings.TrimRight(m.telegramBaseURL, "/") + "/bot" + url.PathEscape(strings.TrimSpace(config.BotToken)) + "/sendMessage"
	return m.postJSON(endpoint, map[string]any{
		"chat_id": config.ChatID,
		"text":    notificationText(message),
	})
}

func (m *notificationManager) sendBark(config barkForwardConfig, message notificationSMS) error {
	if !config.Enabled || strings.TrimSpace(config.URL) == "" {
		return errors.New("Bark is not fully configured")
	}
	payload := map[string]any{
		"title": "DJ 4G Hub 新短信",
		"body":  notificationText(message),
	}
	if strings.TrimSpace(config.Group) != "" {
		payload["group"] = strings.TrimSpace(config.Group)
	}
	return m.postJSON(config.URL, payload)
}

// sendTextNotification delivers a non-SMS operational notification through
// every enabled channel. It returns true when at least one configured channel
// accepted the message, so callers can safely deduplicate reminders.
func (m *notificationManager) sendTextNotification(title, body string) bool {
	m.mu.RLock()
	config := m.config
	m.mu.RUnlock()
	delivered := false
	if config.Telegram.Enabled && config.Telegram.BotToken != "" && config.Telegram.ChatID != "" {
		endpoint := strings.TrimRight(m.telegramBaseURL, "/") + "/bot" + url.PathEscape(strings.TrimSpace(config.Telegram.BotToken)) + "/sendMessage"
		if err := retryNotification(func() error {
			return m.postJSON(endpoint, map[string]any{"chat_id": config.Telegram.ChatID, "text": title + "\n\n" + body})
		}); err == nil {
			delivered = true
		} else {
			log.Printf("Telegram retention reminder failed: %v", err)
		}
	}
	if config.Bark.Enabled && config.Bark.URL != "" {
		payload := map[string]any{"title": title, "body": body}
		if strings.TrimSpace(config.Bark.Group) != "" {
			payload["group"] = strings.TrimSpace(config.Bark.Group)
		}
		if err := retryNotification(func() error { return m.postJSON(config.Bark.URL, payload) }); err == nil {
			delivered = true
		} else {
			log.Printf("Bark retention reminder failed: %v", err)
		}
	}
	return delivered
}

func (m *notificationManager) postJSON(endpoint string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := m.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("remote service returned HTTP %d", response.StatusCode)
	}
	return nil
}

func (m *notificationManager) publicStatus() notificationPublicStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return notificationPublicStatus{
		Telegram: notificationChannelStatus{
			Enabled:    m.config.Telegram.Enabled,
			Configured: m.config.Telegram.BotToken != "" && m.config.Telegram.ChatID != "",
			ChatID:     m.config.Telegram.ChatID,
			LastError:  m.telegramError,
		},
		Bark: notificationChannelStatus{
			Enabled:    m.config.Bark.Enabled,
			Configured: m.config.Bark.URL != "",
			Group:      m.config.Bark.Group,
			LastError:  m.barkError,
		},
	}
}

func (m *notificationManager) update(request notificationUpdateRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	config := m.config
	config.Telegram.Enabled = request.TelegramEnabled
	config.Telegram.ChatID = strings.TrimSpace(request.TelegramChatID)
	if request.ClearTelegramToken {
		config.Telegram.BotToken = ""
	} else if strings.TrimSpace(request.TelegramBotToken) != "" {
		config.Telegram.BotToken = strings.TrimSpace(request.TelegramBotToken)
	}
	config.Bark.Enabled = request.BarkEnabled
	config.Bark.Group = strings.TrimSpace(request.BarkGroup)
	if request.ClearBarkURL {
		config.Bark.URL = ""
	} else if strings.TrimSpace(request.BarkURL) != "" {
		config.Bark.URL = strings.TrimSpace(request.BarkURL)
	}
	if config.Telegram.Enabled && (config.Telegram.BotToken == "" || config.Telegram.ChatID == "") {
		return errors.New("启用 Telegram 需要 Bot Token 和 Chat ID")
	}
	if config.Bark.Enabled {
		if config.Bark.URL == "" {
			return errors.New("启用 Bark 需要完整推送 URL")
		}
		parsed, err := url.Parse(config.Bark.URL)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
			return errors.New("Bark URL 必须是完整的 HTTP/HTTPS 地址")
		}
	}
	if err := savePrivateJSON(m.configPath, config); err != nil {
		return err
	}
	m.config = config
	return nil
}

func (m *notificationManager) sendTest(channel string) error {
	m.mu.RLock()
	config := m.config
	m.mu.RUnlock()
	message := notificationSMS{
		Sender:  "DJ 4G Hub",
		Content: "这是一条转发测试消息。",
		Card:    "测试",
		Time:    time.Now(),
	}
	switch strings.ToLower(strings.TrimSpace(channel)) {
	case "telegram":
		return m.sendTelegram(config.Telegram, message)
	case "bark":
		return m.sendBark(config.Bark, message)
	default:
		return errors.New("仅支持 telegram 或 bark")
	}
}

func (a *app) notificationSettings(w http.ResponseWriter, _ *http.Request) {
	if a.notifications == nil {
		writeError(w, http.StatusServiceUnavailable, "通知转发未初始化")
		return
	}
	writeJSON(w, http.StatusOK, a.notifications.publicStatus())
}

func (a *app) updateNotificationSettings(w http.ResponseWriter, r *http.Request) {
	if a.notifications == nil {
		writeError(w, http.StatusServiceUnavailable, "通知转发未初始化")
		return
	}
	var request notificationUpdateRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if err := a.notifications.update(request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.baselineCurrentSMS()
	writeJSON(w, http.StatusOK, a.notifications.publicStatus())
}

func (a *app) testNotification(w http.ResponseWriter, r *http.Request) {
	if a.notifications == nil {
		writeError(w, http.StatusServiceUnavailable, "通知转发未初始化")
		return
	}
	var request struct {
		Channel string `json:"channel"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if err := a.notifications.sendTest(request.Channel); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"sent": true})
}
