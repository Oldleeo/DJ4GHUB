package modem

import "testing"

func TestRefreshDeviceInfoPreservesLastGoodSubscriberStateOnATFailure(t *testing.T) {
	m := &Manager{
		atPort:        "COM14",
		healthy:       true,
		simInserted:   true,
		iccid:         "8986000000000000000",
		imsi:          "460000000000000",
		operator:      "Carrier",
		regStatus:     1,
		regStatusText: "已注册(本地)",
		networkMode:   "LTE",
	}

	// A stopped manager makes every AT query fail immediately. A transient
	// communication failure must not be presented as a physically removed SIM.
	m.RefreshDeviceInfo()
	status := m.GetFullStatus()
	if !status.SimInserted || status.ICCID == "" || status.IMSI == "" {
		t.Fatalf("subscriber state was cleared after AT failure: %#v", status)
	}
	if status.RegStatus != 1 || status.NetworkMode != "LTE" || status.Operator != "Carrier" {
		t.Fatalf("network state was cleared after AT failure: %#v", status)
	}
}
