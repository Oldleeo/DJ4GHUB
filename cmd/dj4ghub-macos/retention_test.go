package main

import (
	"path/filepath"
	"testing"
	"time"
)

func testRetentionManager(t *testing.T, now time.Time) *retentionManager {
	t.Helper()
	return &retentionManager{
		path:  filepath.Join(t.TempDir(), "retention.json"),
		now:   func() time.Time { return now },
		store: retentionStore{Version: 1, Cards: []retentionCard{}, ReminderLedger: map[string]time.Time{}},
	}
}

func TestRetentionStatusBoundaries(t *testing.T) {
	now := time.Date(2026, 9, 16, 17, 0, 0, 0, time.Local)
	tests := []struct {
		due, want string
		grace     int
	}{
		{"2026-10-17", "safe", 0},
		{"2026-10-16", "due", 0},
		{"2026-09-23", "urgent", 0},
		{"2026-09-16", "urgent", 0},
		{"2026-09-15", "grace", 10},
		{"2026-09-05", "risk", 10},
	}
	for _, test := range tests {
		view := viewRetentionCard(retentionCard{Enabled: true, NextDue: test.due, GracePeriodDays: test.grace}, now)
		if view.Status != test.want {
			t.Fatalf("due %s: got %s, want %s", test.due, view.Status, test.want)
		}
	}
}

func TestRetentionSaveCompleteAndReload(t *testing.T) {
	now := time.Date(2026, 9, 16, 17, 0, 0, 0, time.Local)
	manager := testRetentionManager(t, now)
	overview, err := manager.saveCard(retentionCard{Label: "UK main", Operator: "Example", NextDue: "2026-09-23", GracePeriodDays: 60, ActionNote: "Make one chargeable call"})
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.Cards) != 1 || overview.Cards[0].Status != "urgent" || overview.Cards[0].ID == "" {
		t.Fatalf("unexpected saved card: %#v", overview.Cards)
	}
	id := overview.Cards[0].ID
	manager.store.ReminderLedger[id+":2026-09-23:7"] = now
	overview, err = manager.completeCard(id, "2027-09-23")
	if err != nil {
		t.Fatal(err)
	}
	if overview.Cards[0].Status != "safe" || overview.Cards[0].LastActionAt.IsZero() || len(manager.store.ReminderLedger) != 0 {
		t.Fatalf("completion was not applied: %#v", overview.Cards[0])
	}
	var stored retentionStore
	if err := loadOptionalJSON(manager.path, &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored.Cards) != 1 || stored.Cards[0].NextDue != "2027-09-23" {
		t.Fatalf("unexpected persisted store: %#v", stored)
	}
}

func TestRetentionVerifyRejectsDifferentICCID(t *testing.T) {
	now := time.Date(2026, 9, 16, 17, 0, 0, 0, time.Local)
	manager := testRetentionManager(t, now)
	overview, err := manager.saveCard(retentionCard{Label: "Travel", ICCID: "89860123456789012345"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = manager.verifyCard(overview.Cards[0].ID, retentionVerification{ICCID: "89441000000000000001", SimInserted: true})
	if err == nil {
		t.Fatal("expected ICCID mismatch")
	}
}

func TestRetentionReminderUsesLatestCrossedThreshold(t *testing.T) {
	view := retentionCardView{retentionCard: retentionCard{Enabled: true}, DaysRemaining: 6, Status: "urgent"}
	if got := retentionReminderBucket(view); got != "7" {
		t.Fatalf("got bucket %q, want 7", got)
	}
	view.DaysRemaining = 20
	view.Status = "due"
	if got := retentionReminderBucket(view); got != "30" {
		t.Fatalf("got bucket %q, want 30", got)
	}
}
