package main

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// localATDevice is intentionally privacy-limited: the desktop host only needs
// a stable opaque key and a short suffix to distinguish identical modules.
type localATDevice struct {
	Key       string `json:"key"`
	Port      string `json:"port"`
	Label     string `json:"label"`
	Suffix    string `json:"suffix,omitempty"`
	Available bool   `json:"available"`
	Error     string `json:"error,omitempty"`
}

func makeLocalATDevice(port, serialNumber, imei string, probeErr error) localATDevice {
	identity := strings.TrimSpace(imei)
	if identity == "" {
		identity = strings.TrimSpace(serialNumber)
	}
	if identity == "" {
		identity = strings.ToUpper(strings.TrimSpace(port))
	}
	sum := sha256.Sum256([]byte("dj4ghub-device:" + identity))
	key := fmt.Sprintf("%x", sum[:8])
	suffix := lastIdentifierDigits(identity, 4)
	label := "DJI 4G 模块"
	if suffix != "" {
		label += " · " + suffix
	}
	device := localATDevice{Key: key, Port: strings.TrimSpace(port), Label: label, Suffix: suffix, Available: probeErr == nil}
	if probeErr != nil {
		device.Error = probeErr.Error()
	}
	return device
}

func lastIdentifierDigits(value string, count int) string {
	var digits []rune
	for _, char := range value {
		if char >= '0' && char <= '9' {
			digits = append(digits, char)
		}
	}
	if len(digits) < count {
		return ""
	}
	return string(digits[len(digits)-count:])
}

func parsePortExclusions(raw string) map[string]struct{} {
	excluded := make(map[string]struct{})
	for _, value := range strings.Split(raw, ",") {
		if value = strings.ToUpper(strings.TrimSpace(value)); value != "" {
			excluded[value] = struct{}{}
		}
	}
	return excluded
}

func portExcluded(port string, excluded map[string]struct{}) bool {
	_, ok := excluded[strings.ToUpper(strings.TrimSpace(port))]
	return ok
}
