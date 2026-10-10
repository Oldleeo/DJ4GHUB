//go:build !windows

package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/WongLoki/DJ4Hub/internal/modem"
)

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
		return "", errors.New("no Quectel/DJI USB serial ports found; pass -port /dev/cu.* explicitly")
	}
	return "", fmt.Errorf("no AT-capable port found among %s", strings.Join(attempted, ", "))
}

func discoverATDevices() ([]localATDevice, error) {
	return discoverATDevicesExcluding(nil)
}

func discoverATDevicesExcluding(excluded map[string]struct{}) ([]localATDevice, error) {
	return discoverATDevicesWithProbe(excluded, true)
}

func discoverATDevicesWithProbe(excluded map[string]struct{}, probe bool) ([]localATDevice, error) {
	var ports []string
	for _, pattern := range []string{
		"/dev/cu.usbmodem*",
		"/dev/cu.usbserial*",
		"/dev/cu.wchusbserial*",
	} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		ports = append(ports, matches...)
	}

	sort.SliceStable(ports, func(i, j int) bool {
		return portScore(ports[i]) > portScore(ports[j])
	})
	devices := make([]localATDevice, 0, len(ports))
	for _, port := range ports {
		if portExcluded(port, excluded) {
			continue
		}
		var imei string
		var probeErr error
		if probe {
			imei, probeErr = modem.ProbeIMEICached(port, 2*time.Second)
		}
		devices = append(devices, makeLocalATDevice(port, "", imei, probeErr))
	}
	return devices, nil
}
