//go:build windows

package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/WongLoki/DJ4Hub/internal/modem"
	"go.bug.st/serial/enumerator"
)

const (
	djiQuectelVID = "2CA3"
	djiQuectelPID = "4006"
)

type windowsSerialCandidate struct {
	Name         string
	Product      string
	VID          string
	PID          string
	SerialNumber string
}

func selectDJIWindowsATCandidates(ports []windowsSerialCandidate) []string {
	var candidates []string
	for _, port := range ports {
		if !strings.EqualFold(strings.TrimSpace(port.VID), djiQuectelVID) ||
			!strings.EqualFold(strings.TrimSpace(port.PID), djiQuectelPID) {
			continue
		}
		if !strings.Contains(strings.ToLower(port.Product), "at port") {
			continue
		}
		if name := strings.TrimSpace(port.Name); name != "" {
			candidates = append(candidates, name)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return strings.ToUpper(candidates[i]) < strings.ToUpper(candidates[j])
	})
	return candidates
}

func discoverATPort() (string, error) {
	devices, err := discoverATDevices()
	if err != nil {
		return "", err
	}
	var attempted []string
	for _, device := range devices {
		attempted = append(attempted, device.Port)
		if device.Available {
			return device.Port, nil
		}
	}
	if len(attempted) == 0 {
		return "", errors.New("DJI/Quectel AT port not found for USB VID_2CA3&PID_4006; install the verified Quectel driver or pass -port COMx explicitly")
	}
	return "", fmt.Errorf("the exact DJI/Quectel AT port did not answer among %s", strings.Join(attempted, ", "))
}

func discoverATDevices() ([]localATDevice, error) {
	return discoverATDevicesExcluding(nil)
}

func discoverATDevicesExcluding(excluded map[string]struct{}) ([]localATDevice, error) {
	return discoverATDevicesWithProbe(excluded, true)
}

func discoverATDevicesWithProbe(excluded map[string]struct{}, probe bool) ([]localATDevice, error) {
	details, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return nil, fmt.Errorf("enumerate Windows serial ports: %w", err)
	}

	ports := make([]windowsSerialCandidate, 0, len(details))
	for _, port := range details {
		if port == nil {
			continue
		}
		ports = append(ports, windowsSerialCandidate{
			Name:         port.Name,
			Product:      port.Product,
			VID:          port.VID,
			PID:          port.PID,
			SerialNumber: port.SerialNumber,
		})
	}

	candidates := selectDJIWindowsATCandidates(ports)
	serialByPort := make(map[string]string, len(ports))
	for _, port := range ports {
		serialByPort[strings.ToUpper(strings.TrimSpace(port.Name))] = port.SerialNumber
	}
	devices := make([]localATDevice, 0, len(candidates))
	for _, port := range candidates {
		if portExcluded(port, excluded) {
			continue
		}
		var imei string
		var probeErr error
		if probe {
			imei, probeErr = modem.ProbeIMEICached(port, 2*time.Second)
		}
		devices = append(devices, makeLocalATDevice(port, serialByPort[strings.ToUpper(port)], imei, probeErr))
	}
	return devices, nil
}
