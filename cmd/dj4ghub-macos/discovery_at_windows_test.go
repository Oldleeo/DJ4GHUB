//go:build windows

package main

import (
	"reflect"
	"testing"
)

func TestSelectDJIWindowsATCandidatesRequiresExactDeviceAndATRole(t *testing.T) {
	ports := []windowsSerialCandidate{
		{Name: "COM1", Product: "Communications Port", VID: "", PID: ""},
		{Name: "COM5", Product: "Quectel USB DM Port (COM5)", VID: "2CA3", PID: "4006"},
		{Name: "COM6", Product: "Quectel USB NMEA Port (COM6)", VID: "2CA3", PID: "4006"},
		{Name: "COM7", Product: "Quectel USB AT Port (COM7)", VID: "2ca3", PID: "4006"},
		{Name: "COM17", Product: "Quectel USB AT Port (COM17)", VID: "2CA3", PID: "4006"},
		{Name: "COM9", Product: "Quectel USB AT Port (COM9)", VID: "2C7C", PID: "0125"},
	}

	if got, want := selectDJIWindowsATCandidates(ports), []string{"COM17", "COM7"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("selectDJIWindowsATCandidates() = %v, want %v", got, want)
	}
}

func TestMakeLocalATDeviceSeparatesIdenticalModules(t *testing.T) {
	one := makeLocalATDevice("COM7", "", "000000000001111", nil)
	two := makeLocalATDevice("COM17", "", "000000000002222", nil)
	if one.Key == two.Key || one.Label == two.Label {
		t.Fatalf("identical module models were not separated: %#v %#v", one, two)
	}
	if one.Suffix != "1111" || two.Suffix != "2222" {
		t.Fatalf("unexpected privacy suffixes: %#v %#v", one, two)
	}
}

func TestParsePortExclusionsIsCaseInsensitive(t *testing.T) {
	excluded := parsePortExclusions(" com14, COM18 ,, ")
	if !portExcluded("COM14", excluded) || !portExcluded("com18", excluded) || portExcluded("COM20", excluded) {
		t.Fatalf("unexpected exclusion set: %#v", excluded)
	}
}
