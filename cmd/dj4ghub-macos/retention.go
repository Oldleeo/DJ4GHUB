package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const retentionDateLayout = "2006-01-02"

var retentionICCIDPattern = regexp.MustCompile(`^[0-9]{10,32}$`)

type retentionCard struct {
	ID                   string    `json:"id"`
	Label                string    `json:"label"`
	Country              string    `json:"country,omitempty"`
	Operator             string    `json:"operator,omitempty"`
	Phone                string    `json:"phone,omitempty"`
	ICCID                string    `json:"iccid,omitempty"`
	Plan                 string    `json:"plan,omitempty"`
	NextDue              string    `json:"next_due,omitempty"`
	GracePeriodDays      int       `json:"grace_period_days"`
	ActionNote           string    `json:"action_note,omitempty"`
	SourceURL            string    `json:"source_url,omitempty"`
	Enabled              bool      `json:"enabled"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
	LastActionAt         time.Time `json:"last_action_at,omitempty"`
	LastVerifiedAt       time.Time `json:"last_verified_at,omitempty"`
	LastSeenOperator     string    `json:"last_seen_operator,omitempty"`
	LastSeenRegistration string    `json:"last_seen_registration,omitempty"`
}

type retentionCardView struct {
	retentionCard
	Status        string `json:"status"`
	StatusText    string `json:"status_text"`
	DaysRemaining int    `json:"days_remaining"`
	GraceEnd      string `json:"grace_end,omitempty"`
}

type retentionStore struct {
	Version        int                  `json:"version"`
	Cards          []retentionCard      `json:"cards"`
	ReminderLedger map[string]time.Time `json:"reminder_ledger,omitempty"`
}

type retentionOverviewResponse struct {
	Cards      []retentionCardView `json:"cards"`
	Counts     map[string]int      `json:"counts"`
	ReminderAt []int               `json:"reminder_days"`
	UpdatedAt  time.Time           `json:"updated_at"`
}

type retentionManager struct {
	mu    sync.Mutex
	path  string
	store retentionStore
	now   func() time.Time
}

func (a *app) initRetention() error {
	if a.retention != nil {
		return nil
	}
	root := strings.TrimSpace(a.dataDir)
	if root == "" {
		var err error
		root, err = os.UserConfigDir()
		if err != nil {
			return err
		}
	}
	manager := &retentionManager{
		path:  filepath.Join(root, "DJ4Hub", "retention.json"),
		now:   time.Now,
		store: retentionStore{Version: 1, Cards: []retentionCard{}, ReminderLedger: map[string]time.Time{}},
	}
	if err := loadOptionalJSON(manager.path, &manager.store); err != nil {
		return err
	}
	if manager.store.Cards == nil {
		manager.store.Cards = []retentionCard{}
	}
	if manager.store.ReminderLedger == nil {
		manager.store.ReminderLedger = map[string]time.Time{}
	}
	manager.store.Version = 1
	a.retention = manager
	return nil
}

func normalizeRetentionCard(card retentionCard, now time.Time) (retentionCard, error) {
	card.ID = strings.TrimSpace(card.ID)
	card.Label = strings.TrimSpace(card.Label)
	card.Country = strings.TrimSpace(card.Country)
	card.Operator = strings.TrimSpace(card.Operator)
	card.Phone = strings.TrimSpace(card.Phone)
	card.ICCID = strings.TrimSpace(card.ICCID)
	card.Plan = strings.TrimSpace(card.Plan)
	card.NextDue = strings.TrimSpace(card.NextDue)
	card.ActionNote = strings.TrimSpace(card.ActionNote)
	card.SourceURL = strings.TrimSpace(card.SourceURL)
	if card.Label == "" || len([]rune(card.Label)) > 80 {
		return card, errors.New("卡片名称必填，且不能超过 80 个字符")
	}
	if len([]rune(card.Country)) > 80 || len([]rune(card.Operator)) > 100 || len([]rune(card.Phone)) > 80 || len([]rune(card.Plan)) > 120 || len([]rune(card.ActionNote)) > 500 {
		return card, errors.New("卡片资料过长")
	}
	if card.ICCID != "" && !retentionICCIDPattern.MatchString(card.ICCID) {
		return card, errors.New("ICCID 必须是 10 至 32 位数字")
	}
	if card.NextDue != "" {
		if _, err := time.ParseInLocation(retentionDateLayout, card.NextDue, now.Location()); err != nil {
			return card, errors.New("下次保号日期无效")
		}
	}
	if card.GracePeriodDays < 0 || card.GracePeriodDays > 730 {
		return card, errors.New("宽限期必须在 0 至 730 天之间")
	}
	if card.SourceURL != "" {
		parsed, err := url.Parse(card.SourceURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || len(card.SourceURL) > 1000 {
			return card, errors.New("规则来源必须是完整的 HTTPS 链接")
		}
	}
	return card, nil
}

func dayStart(value time.Time) time.Time {
	year, month, day := value.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, value.Location())
}

func viewRetentionCard(card retentionCard, now time.Time) retentionCardView {
	view := retentionCardView{retentionCard: card, Status: "unknown", StatusText: "待设置"}
	if !card.Enabled {
		view.Status, view.StatusText = "paused", "已暂停"
		return view
	}
	if card.NextDue == "" {
		return view
	}
	due, err := time.ParseInLocation(retentionDateLayout, card.NextDue, now.Location())
	if err != nil {
		return view
	}
	view.DaysRemaining = int(dayStart(due).Sub(dayStart(now)).Hours() / 24)
	if card.GracePeriodDays > 0 {
		view.GraceEnd = due.AddDate(0, 0, card.GracePeriodDays).Format(retentionDateLayout)
	}
	switch {
	case view.DaysRemaining > 30:
		view.Status, view.StatusText = "safe", "安全"
	case view.DaysRemaining > 7:
		view.Status, view.StatusText = "due", "即将到期"
	case view.DaysRemaining >= 0:
		view.Status, view.StatusText = "urgent", "紧急"
	case -view.DaysRemaining <= card.GracePeriodDays:
		view.Status, view.StatusText = "grace", "宽限期"
	default:
		view.Status, view.StatusText = "risk", "号码回收风险"
	}
	return view
}

func (m *retentionManager) overview() retentionOverviewResponse {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	cards := make([]retentionCardView, 0, len(m.store.Cards))
	counts := map[string]int{"total": len(m.store.Cards), "safe": 0, "due": 0, "urgent": 0, "grace": 0, "risk": 0, "unknown": 0, "paused": 0}
	for _, card := range m.store.Cards {
		view := viewRetentionCard(card, now)
		cards = append(cards, view)
		counts[view.Status]++
	}
	sort.SliceStable(cards, func(i, j int) bool {
		left, right := cards[i].NextDue, cards[j].NextDue
		if left == "" {
			return false
		}
		if right == "" {
			return true
		}
		return left < right
	})
	return retentionOverviewResponse{Cards: cards, Counts: counts, ReminderAt: []int{30, 14, 7, 1, 0}, UpdatedAt: now.UTC()}
}

func (m *retentionManager) saveCard(request retentionCard) (retentionOverviewResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	card, err := normalizeRetentionCard(request, now)
	if err != nil {
		return retentionOverviewResponse{}, err
	}
	if card.ID == "" {
		card.ID, err = randomLocalIdentifier()
		if err != nil {
			return retentionOverviewResponse{}, err
		}
		card.CreatedAt = now.UTC()
		card.Enabled = true
	} else {
		found := -1
		for i := range m.store.Cards {
			if m.store.Cards[i].ID == card.ID {
				found = i
				break
			}
		}
		if found < 0 {
			return retentionOverviewResponse{}, errors.New("保号卡片不存在")
		}
		old := m.store.Cards[found]
		card.CreatedAt, card.LastActionAt, card.LastVerifiedAt = old.CreatedAt, old.LastActionAt, old.LastVerifiedAt
		card.LastSeenOperator, card.LastSeenRegistration = old.LastSeenOperator, old.LastSeenRegistration
		if old.NextDue != card.NextDue {
			m.clearLedgerLocked(card.ID)
		}
		m.store.Cards[found] = card
		card.UpdatedAt = now.UTC()
		m.store.Cards[found] = card
		if err := savePrivateJSON(m.path, m.store); err != nil {
			return retentionOverviewResponse{}, err
		}
		return overviewRetentionLocked(m, now), nil
	}
	card.UpdatedAt = now.UTC()
	m.store.Cards = append(m.store.Cards, card)
	if err := savePrivateJSON(m.path, m.store); err != nil {
		return retentionOverviewResponse{}, err
	}
	return overviewRetentionLocked(m, now), nil
}

func overviewRetentionLocked(m *retentionManager, now time.Time) retentionOverviewResponse {
	cards := make([]retentionCardView, 0, len(m.store.Cards))
	counts := map[string]int{"total": len(m.store.Cards), "safe": 0, "due": 0, "urgent": 0, "grace": 0, "risk": 0, "unknown": 0, "paused": 0}
	for _, card := range m.store.Cards {
		view := viewRetentionCard(card, now)
		cards = append(cards, view)
		counts[view.Status]++
	}
	sort.SliceStable(cards, func(i, j int) bool {
		if cards[i].NextDue == "" {
			return false
		}
		if cards[j].NextDue == "" {
			return true
		}
		return cards[i].NextDue < cards[j].NextDue
	})
	return retentionOverviewResponse{Cards: cards, Counts: counts, ReminderAt: []int{30, 14, 7, 1, 0}, UpdatedAt: now.UTC()}
}

func (m *retentionManager) clearLedgerLocked(id string) {
	for key := range m.store.ReminderLedger {
		if strings.HasPrefix(key, id+":") {
			delete(m.store.ReminderLedger, key)
		}
	}
}

func (m *retentionManager) deleteCard(id string) (retentionOverviewResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id = strings.TrimSpace(id)
	for i := range m.store.Cards {
		if m.store.Cards[i].ID != id {
			continue
		}
		m.store.Cards = append(m.store.Cards[:i], m.store.Cards[i+1:]...)
		m.clearLedgerLocked(id)
		if err := savePrivateJSON(m.path, m.store); err != nil {
			return retentionOverviewResponse{}, err
		}
		return overviewRetentionLocked(m, m.now()), nil
	}
	return retentionOverviewResponse{}, errors.New("保号卡片不存在")
}

func (m *retentionManager) completeCard(id, nextDue string) (retentionOverviewResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	nextDue = strings.TrimSpace(nextDue)
	due, err := time.ParseInLocation(retentionDateLayout, nextDue, now.Location())
	if err != nil || dayStart(due).Before(dayStart(now)) {
		return retentionOverviewResponse{}, errors.New("请填写今天或之后的下次保号日期")
	}
	for i := range m.store.Cards {
		if m.store.Cards[i].ID != strings.TrimSpace(id) {
			continue
		}
		m.store.Cards[i].NextDue = nextDue
		m.store.Cards[i].LastActionAt = now.UTC()
		m.store.Cards[i].UpdatedAt = now.UTC()
		m.clearLedgerLocked(id)
		if err := savePrivateJSON(m.path, m.store); err != nil {
			return retentionOverviewResponse{}, err
		}
		return overviewRetentionLocked(m, now), nil
	}
	return retentionOverviewResponse{}, errors.New("保号卡片不存在")
}

func (m *retentionManager) verifyCard(id string, current retentionVerification) (retentionOverviewResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if current.ICCID == "" || !current.SimInserted {
		return retentionOverviewResponse{}, errors.New("当前模块未读到可用 SIM 卡")
	}
	for i := range m.store.Cards {
		card := &m.store.Cards[i]
		if card.ID != strings.TrimSpace(id) {
			continue
		}
		if card.ICCID != "" && card.ICCID != current.ICCID {
			return retentionOverviewResponse{}, errors.New("当前卡 ICCID 与档案不一致")
		}
		card.ICCID = current.ICCID
		card.LastVerifiedAt = m.now().UTC()
		card.LastSeenOperator = current.Operator
		card.LastSeenRegistration = current.Registration
		card.UpdatedAt = m.now().UTC()
		if err := savePrivateJSON(m.path, m.store); err != nil {
			return retentionOverviewResponse{}, err
		}
		return overviewRetentionLocked(m, m.now()), nil
	}
	return retentionOverviewResponse{}, errors.New("保号卡片不存在")
}

type retentionVerification struct {
	ICCID, Operator, Registration string
	SimInserted                   bool
}

func (a *app) currentRetentionVerification() (retentionVerification, error) {
	raw, err := captureLocalJSON(a.status, "/api/status")
	if err != nil {
		return retentionVerification{}, err
	}
	var status struct {
		ICCID        string `json:"iccid"`
		Operator     string `json:"operator"`
		Registration string `json:"reg_status_text"`
		SimInserted  bool   `json:"sim_inserted"`
	}
	if err := json.Unmarshal(raw, &status); err != nil {
		return retentionVerification{}, err
	}
	return retentionVerification{ICCID: strings.TrimSpace(status.ICCID), Operator: strings.TrimSpace(status.Operator), Registration: strings.TrimSpace(status.Registration), SimInserted: status.SimInserted}, nil
}

func (a *app) retentionOverview(w http.ResponseWriter, _ *http.Request) {
	if a.retention == nil {
		writeError(w, http.StatusServiceUnavailable, "保号助手未初始化")
		return
	}
	writeJSON(w, http.StatusOK, a.retention.overview())
}

func (a *app) retentionOverviewValue() retentionOverviewResponse {
	if a.retention == nil {
		return retentionOverviewResponse{Cards: []retentionCardView{}, Counts: map[string]int{"total": 0}, ReminderAt: []int{30, 14, 7, 1, 0}, UpdatedAt: time.Now().UTC()}
	}
	return a.retention.overview()
}

func (a *app) saveRetentionCard(w http.ResponseWriter, r *http.Request) {
	var card retentionCard
	if !decodeJSON(w, r, &card) {
		return
	}
	overview, err := a.retention.saveCard(card)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, overview)
}

func (a *app) deleteRetentionCard(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ID string `json:"id"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	overview, err := a.retention.deleteCard(request.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, overview)
}

func (a *app) completeRetentionCard(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ID      string `json:"id"`
		NextDue string `json:"next_due"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	overview, err := a.retention.completeCard(request.ID, request.NextDue)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, overview)
}

func (a *app) verifyRetentionCard(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ID string `json:"id"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	current, err := a.currentRetentionVerification()
	if err == nil {
		_, err = a.retention.verifyCard(request.ID, current)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, a.retention.overview())
}

func (a *app) monitorRetention(ctx context.Context) {
	timer := time.NewTimer(12 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			a.checkRetentionReminders()
			timer.Reset(6 * time.Hour)
		}
	}
}

func retentionReminderBucket(view retentionCardView) string {
	if view.Status == "grace" || view.Status == "risk" {
		return view.Status
	}
	switch {
	case view.DaysRemaining > 30:
		return ""
	case view.DaysRemaining > 14:
		return "30"
	case view.DaysRemaining > 7:
		return "14"
	case view.DaysRemaining > 1:
		return "7"
	case view.DaysRemaining > 0:
		return "1"
	default:
		return fmt.Sprint(0)
	}
}

func (a *app) checkRetentionReminders() {
	if a.retention == nil || a.notifications == nil {
		return
	}
	a.retention.mu.Lock()
	now := a.retention.now()
	views := make([]retentionCardView, 0, len(a.retention.store.Cards))
	for _, card := range a.retention.store.Cards {
		views = append(views, viewRetentionCard(card, now))
	}
	a.retention.mu.Unlock()
	for _, view := range views {
		if !view.Enabled || view.NextDue == "" {
			continue
		}
		bucket := retentionReminderBucket(view)
		if bucket == "" {
			continue
		}
		key := view.ID + ":" + view.NextDue + ":" + bucket
		a.retention.mu.Lock()
		_, sent := a.retention.store.ReminderLedger[key]
		a.retention.mu.Unlock()
		if sent {
			continue
		}
		body := fmt.Sprintf("%s（%s）\n状态：%s\n保号日期：%s", view.Label, firstNonEmpty(view.Operator, view.Country, "未标注运营商"), view.StatusText, view.NextDue)
		if view.ActionNote != "" {
			body += "\n建议动作：" + view.ActionNote
		}
		if !a.notifications.sendTextNotification("DJ4GHUB 保号提醒", body) {
			continue
		}
		a.retention.mu.Lock()
		a.retention.store.ReminderLedger[key] = now.UTC()
		_ = savePrivateJSON(a.retention.path, a.retention.store)
		a.retention.mu.Unlock()
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
