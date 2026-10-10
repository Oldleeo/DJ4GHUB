package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type remoteCommandReceipt struct {
	Result       json.RawMessage `json:"result"`
	Acknowledged bool            `json:"acknowledged"`
}
type remoteCommandJournal struct {
	Namespace string                         `json:"namespace"`
	Receipts  map[int64]remoteCommandReceipt `json:"receipts"`
}

func remoteCommandNeedsJournal(name string) bool {
	return !strings.HasPrefix(name, "read_") && name != "refresh_status" && name != "refresh_sms" &&
		name != "check_4g" && name != "check_proxy" && name != "probe_esim_phonebook"
}

func (m *remoteRelayManager) commandNamespace() string {
	return m.config.RelayURL + "|" + m.config.HostID + "|" + m.config.HostDeviceID
}
func (m *remoteRelayManager) journalPath() string {
	return filepath.Join(filepath.Dir(m.path), "remote-command-receipts.json")
}
func (m *remoteRelayManager) loadCommandJournal() error {
	m.commandJournal = remoteCommandJournal{Namespace: m.commandNamespace(), Receipts: map[int64]remoteCommandReceipt{}}
	data, err := os.ReadFile(m.journalPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var saved remoteCommandJournal
	if err := json.Unmarshal(data, &saved); err != nil {
		return fmt.Errorf("命令防重放记录损坏；为防止重复操作暂停云端命令: %w", err)
	}
	if saved.Namespace == m.commandNamespace() && saved.Receipts != nil {
		m.commandJournal = saved
	}
	return nil
}

func (m *remoteRelayManager) saveCommandJournalLocked() error {
	data, err := json.Marshal(m.commandJournal)
	if err != nil {
		return err
	}
	path := m.journalPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(path+".tmp", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = file.Write(data)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(path+".tmp", path)
}

// Persist intent BEFORE any side effect. A crash between sending and receiving
// the module response becomes "unknown, check manually", never an automatic retry.
// No SMS content, phone numbers, activation codes or tokens enter this journal.
func (m *remoteRelayManager) reserveRemoteCommand(command pendingRelayCommand) (json.RawMessage, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.commandJournal.Namespace != m.commandNamespace() {
		m.commandJournal = remoteCommandJournal{Namespace: m.commandNamespace(), Receipts: map[int64]remoteCommandReceipt{}}
	}
	if receipt, ok := m.commandJournal.Receipts[command.ID]; ok {
		return receipt.Result, true, nil
	}
	for len(m.commandJournal.Receipts) >= 2048 {
		var oldest int64
		for id, receipt := range m.commandJournal.Receipts {
			if receipt.Acknowledged && (oldest == 0 || id < oldest) {
				oldest = id
			}
		}
		if oldest == 0 {
			return nil, false, errors.New("云端未确认命令过多；暂停执行以防重复操作")
		}
		delete(m.commandJournal.Receipts, oldest)
	}
	marker, _ := json.Marshal(map[string]any{"ok": false, "command": command.Name, "error": "上次命令执行结果未确认；已阻止自动重试，请核对实际状态"})
	m.commandJournal.Receipts[command.ID] = remoteCommandReceipt{Result: marker}
	if err := m.saveCommandJournalLocked(); err != nil {
		delete(m.commandJournal.Receipts, command.ID)
		return nil, false, err
	}
	return nil, false, nil
}

func (m *remoteRelayManager) finishRemoteCommand(command pendingRelayCommand, result json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var minimal struct {
		OK      bool   `json:"ok"`
		Error   string `json:"error,omitempty"`
		Command string `json:"command"`
	}
	if err := json.Unmarshal(result, &minimal); err != nil {
		return err
	}
	minimal.Command = command.Name
	// Error strings from devices may contain identifiers: keep only a generic
	// failure in durable receipts; the detailed result is returned live.
	if !minimal.OK {
		minimal.Error = "上次命令执行失败或结果未确认，不会自动重试"
	}
	encoded, _ := json.Marshal(minimal)
	m.commandJournal.Receipts[command.ID] = remoteCommandReceipt{Result: encoded}
	return m.saveCommandJournalLocked()
}
func (m *remoteRelayManager) acknowledgeRemoteCommand(id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	receipt := m.commandJournal.Receipts[id]
	receipt.Acknowledged = true
	m.commandJournal.Receipts[id] = receipt
	return m.saveCommandJournalLocked()
}
