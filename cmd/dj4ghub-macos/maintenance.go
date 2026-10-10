package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/WongLoki/DJ4Hub/internal/modem"
)

const appVersion = "0.6.7"

func (a *app) healthSnapshot() map[string]any {
	usbDevice := a.currentUSBDevice()
	esimManager, _ := a.currentESIMManager()
	status := modem.DeviceStatus{}
	atReady := false
	if a.demo {
		atReady = true
		status.SimInserted = true
		status.SIMStateKnown = true
		status.RegStatus = 1
		status.RegStatusText = "已注册"
		status.NetworkMode = "LTE"
	} else if a.modem != nil {
		status = a.modem.GetFullStatus()
		atReady = a.modem.IsHealthy() && a.modem.CanExecuteAT()
	} else if a.usbAT != nil {
		atReady = strings.TrimSpace(a.discoveryError) == ""
	}

	devicePresent := a.demo || usbDevice != nil || a.modem != nil || a.usbAT != nil
	registered := status.RegStatus == 1 || status.RegStatus == 5
	overallState := "waiting_device"
	overallText := "等待 DJI 4G 模块"
	switch {
	case devicePresent && !atReady:
		overallState = "recovering_at"
		overallText = "已检测到模块，AT 接口正在恢复"
	case atReady && !status.SIMStateKnown:
		overallState = "reading_sim"
		overallText = "模块已连接，正在读取 SIM 资料"
	case atReady && !status.SimInserted:
		overallState = "waiting_sim"
		overallText = "模块已连接，等待 SIM"
	case atReady && status.SimInserted && !registered:
		overallState = "registering"
		overallText = "SIM 已就绪，正在注册网络"
	case atReady && status.SimInserted && registered:
		overallState = "ready"
		overallText = "模块正常 · " + maintenanceFirstNonEmpty(status.Operator, status.NetworkMode, status.RegStatusText)
	}

	remoteEnabled, remoteConnected, remoteLastSync, remoteError := a.remoteHealth()
	return map[string]any{
		"ok":                 true,
		"version":            appVersion,
		"device_key":         a.deviceKey,
		"device_name":        maintenanceFirstNonEmpty(a.deviceName, "DJI 4G Module"),
		"port":               a.port,
		"demo":               a.demo,
		"read_only":          a.readOnly,
		"remote_client":      a.remoteClient,
		"usb_device":         usbDevice,
		"device_present":     devicePresent,
		"at_ready":           atReady,
		"sim_ready":          status.SimInserted,
		"network_registered": registered,
		"registration":       status.RegStatusText,
		"operator":           status.Operator,
		"network_mode":       status.NetworkMode,
		"overall_state":      overallState,
		"overall_text":       overallText,
		"esim_available":     a.demo || esimManager != nil,
		"remote_enabled":     remoteEnabled,
		"remote_connected":   remoteConnected,
		"remote_last_sync":   remoteLastSync,
		"remote_error":       remoteError,
		"discovery_error":    a.discoveryError,
	}
}

func maintenanceFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return "已连接"
}

func (a *app) remoteHealth() (enabled, connected bool, lastSync time.Time, lastError string) {
	if a.remote == nil {
		return false, false, time.Time{}, ""
	}
	settings := a.remote.settings()
	enabled, _ = settings["host_enabled"].(bool)
	registered, _ := settings["host_registered"].(bool)
	lastSync, _ = settings["last_sync"].(time.Time)
	lastError, _ = settings["last_error"].(string)
	connected = enabled && registered && lastError == "" && !lastSync.IsZero() && time.Since(lastSync) < 90*time.Second
	return
}

func (a *app) diagnosticSnapshot() map[string]any {
	health := a.healthSnapshot()
	var status modem.DeviceStatus
	if a.modem != nil {
		status = a.modem.GetFullStatus()
	}
	a.smsMu.RLock()
	smsCount := len(a.sms)
	smsLastPoll := a.smsLastPoll
	smsLastError := a.smsLastPollError
	a.smsMu.RUnlock()

	report := map[string]any{
		"generated_at": time.Now(),
		"app": map[string]any{
			"name": "老李 DJ4GHUB", "version": appVersion,
			"os": runtime.GOOS, "arch": runtime.GOARCH,
		},
		"health": health,
		"module": map[string]any{
			"firmware":     status.Firmware,
			"sim_inserted": status.SimInserted,
			"operator":     status.Operator,
			"registration": status.RegStatusText,
			"network_mode": status.NetworkMode,
			"signal_dbm":   status.SignalDBM,
			"usbnet_mode":  status.USBNetMode,
		},
		"sms": map[string]any{
			"cached_count":          smsCount,
			"last_poll":             smsLastPoll,
			"last_poll_error":       smsLastError,
			"poll_interval_seconds": int(a.smsPollInterval.Seconds()),
		},
	}
	if a.notifications != nil {
		report["notifications"] = a.notifications.publicStatus()
	}
	if a.remote != nil {
		report["remote"] = a.remote.settings()
	}
	return report
}

func (a *app) diagnostics(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.diagnosticSnapshot())
}

func (a *app) exportDiagnostics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="DJ4GHUB-diagnostics-%s.json"`, time.Now().Format("20060102-150405")))
	w.WriteHeader(http.StatusOK)
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(a.diagnosticSnapshot())
}

func (a *app) exportSMS(w http.ResponseWriter, _ *http.Request) {
	a.smsMu.RLock()
	items := append([]receivedSMS(nil), a.sms...)
	a.smsMu.RUnlock()
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="DJ4GHUB-sms-%s.csv"`, time.Now().Format("20060102-150405")))
	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF})
	writer := csv.NewWriter(w)
	_ = writer.Write([]string{"时间", "发送方", "短信内容", "验证码", "存储位置", "索引"})
	for _, item := range items {
		_ = writer.Write([]string{
			item.Timestamp.Local().Format("2006-01-02 15:04:05"),
			item.Sender,
			item.Content,
			item.Code,
			item.Memory,
			strconv.Itoa(item.Index),
		})
	}
	writer.Flush()
}
