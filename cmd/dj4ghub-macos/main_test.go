package main

import (
	"errors"
	"testing"
)

func TestPortScore(t *testing.T) {
	tests := []struct {
		name string
		port string
		want int
	}{
		{name: "named Quectel port", port: "/dev/cu.Quectel-AT", want: 100},
		{name: "usb modem", port: "/dev/cu.usbmodem2101", want: 80},
		{name: "usb serial", port: "/dev/cu.usbserial-1420", want: 60},
		{name: "bluetooth", port: "/dev/cu.Bluetooth-Incoming-Port", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := portScore(tt.port); got != tt.want {
				t.Fatalf("portScore(%q) = %d, want %d", tt.port, got, tt.want)
			}
		})
	}
}

func TestParseUSBNetMode(t *testing.T) {
	for _, tt := range []struct {
		response string
		want     string
	}{
		{response: "AT+QCFG=\"usbnet\"\r\n+QCFG: \"usbnet\",0\r\nOK", want: "0"},
		{response: "+QCFG: \"usbnet\",1\r\nOK", want: "1"},
		{response: "ERROR", want: ""},
	} {
		if got := parseUSBNetMode(tt.response); got != tt.want {
			t.Fatalf("parseUSBNetMode(%q) = %q, want %q", tt.response, got, tt.want)
		}
	}
}

func TestATCommandResultSucceededForSerialManagerEmptyPayload(t *testing.T) {
	if !atCommandResultSucceeded("", true) {
		t.Fatal("serial manager nil-error response should be successful after terminal OK is stripped")
	}
	if atCommandResultSucceeded("", false) {
		t.Fatal("raw USB AT empty response should not be treated as successful")
	}
	if !atCommandResultSucceeded("+CPBS: \"ME\",0,500\r\nOK", false) {
		t.Fatal("raw USB AT response ending in OK should be successful")
	}
}

func TestParseUSBATOperator(t *testing.T) {
	for _, tt := range []struct {
		name     string
		response string
		want     string
	}{
		{
			name:     "known numeric PLMN",
			response: "AT+COPS?\r\n+COPS: 0,2,\"46015\",7\r\nOK",
			want:     "中国广电",
		},
		{
			name:     "long operator name",
			response: "+COPS: 0,0,\"CHN-UNICOM\",7\r\nOK",
			want:     "CHN-UNICOM",
		},
		{
			name:     "missing operator",
			response: "+COPS: 0\r\nOK",
			want:     "",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseUSBATOperator(tt.response); got != tt.want {
				t.Fatalf("parseUSBATOperator(%q) = %q, want %q", tt.response, got, tt.want)
			}
		})
	}
}

func TestInitUSBATESIMManagerAfterDelayedUSBOpen(t *testing.T) {
	instance := &app{}

	manager, switchAllowed := instance.currentESIMManager()
	if manager != nil || switchAllowed {
		t.Fatalf("initial eSIM state = (%v, %v), want unavailable", manager, switchAllowed)
	}

	instance.initUSBATESIMManager()
	manager, switchAllowed = instance.currentESIMManager()
	if manager == nil {
		t.Fatal("USB AT recovery did not initialize the eSIM manager")
	}
	if !switchAllowed {
		t.Fatal("USB AT eSIM manager should allow profile switching")
	}

	instance.initUSBATESIMManager()
	managerAgain, _ := instance.currentESIMManager()
	if managerAgain != manager {
		t.Fatal("repeated USB AT recovery replaced the existing eSIM manager")
	}
}

func TestIsPhysicalSIMESIMProbeError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "serial AT command rejection",
			err:  errors.New("未发现任何 eUICC: 打开 logical channel 失败: AT+CCHO returned ERROR"),
			want: true,
		},
		{
			name: "USB AT transport rejection",
			err:  errors.New("未发现任何 eUICC: 创建 LPA client 失败: 打开 logical channel 失败: 设备返回错误: ERROR"),
			want: true,
		},
		{
			name: "unrelated eUICC failure",
			err:  errors.New("未发现任何 eUICC: profile metadata checksum error"),
			want: false,
		},
		{name: "nil", err: nil, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPhysicalSIMESIMProbeError(tt.err); got != tt.want {
				t.Fatalf("isPhysicalSIMESIMProbeError(%v) = %t, want %t", tt.err, got, tt.want)
			}
		})
	}
}
