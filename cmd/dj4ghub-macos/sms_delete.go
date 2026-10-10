package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
)

var smsMessageIDPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func smsMessageID(item receivedSMS) string {
	sum := sha256.Sum256([]byte(smsCacheKey(item)))
	return hex.EncodeToString(sum[:])
}

func (a *app) deleteSMS(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	body.ID = strings.ToLower(strings.TrimSpace(body.ID))
	if !smsMessageIDPattern.MatchString(body.ID) {
		writeError(w, http.StatusBadRequest, "invalid SMS id")
		return
	}

	a.smsPollMu.Lock()
	defer a.smsPollMu.Unlock()

	a.smsMu.RLock()
	var target receivedSMS
	found := false
	for _, item := range a.sms {
		if item.ID == body.ID || smsMessageID(item) == body.ID {
			target = item
			found = true
			break
		}
	}
	a.smsMu.RUnlock()
	if !found {
		writeError(w, http.StatusNotFound, "SMS not found")
		return
	}

	moduleDeleted := 0
	if !a.demo {
		var err error
		moduleDeleted, err = a.deleteSMSFromModule(target)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
	}

	a.smsMu.Lock()
	kept := a.sms[:0]
	for _, item := range a.sms {
		if item.ID == body.ID || smsMessageID(item) == body.ID {
			continue
		}
		kept = append(kept, item)
	}
	a.sms = kept
	remaining := len(a.sms)
	a.smsMu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"deleted":        true,
		"module_deleted": moduleDeleted,
		"remaining":      remaining,
	})
}

func (a *app) deleteSMSFromModule(target receivedSMS) (int, error) {
	if _, err := a.runATCommand("AT+CMGF=0", 5*time.Second); err != nil {
		return 0, fmt.Errorf("set SMS PDU mode: %w", err)
	}

	originalStorage := ""
	if response, err := a.runATCommand("AT+CPMS?", 5*time.Second); err == nil {
		originalStorage = parseSMSStorage(response)
	}
	if originalStorage != "" {
		defer func() {
			_, _ = a.runATCommand(fmt.Sprintf(`AT+CPMS="%s","%s","%s"`, originalStorage, originalStorage, originalStorage), 5*time.Second)
		}()
	}

	memories := []string{"SM", "ME"}
	if target.Memory == "ME" {
		memories = []string{"ME", "SM"}
	}
	var scanErrors []string
	scanned := 0
	deleted := 0
	for _, memory := range memories {
		candidates, err := a.scanSMSStorage(memory)
		if err != nil {
			scanErrors = append(scanErrors, fmt.Sprintf("%s: %v", memory, err))
			continue
		}
		scanned++
		for _, candidate := range candidates {
			if smsMessageID(candidate.message) != smsMessageID(target) {
				continue
			}
			sort.Sort(sort.Reverse(sort.IntSlice(candidate.indexes)))
			for _, index := range candidate.indexes {
				if index <= 0 {
					continue
				}
				if _, err := a.runATCommand(fmt.Sprintf("AT+CMGD=%d", index), 5*time.Second); err != nil {
					return deleted, fmt.Errorf("delete SMS from %s index %d: %w", memory, index, err)
				}
				deleted++
			}
		}
	}
	if scanned == 0 {
		return 0, fmt.Errorf("scan module SMS failed: %s", strings.Join(scanErrors, "; "))
	}
	return deleted, nil
}

type indexedSMS struct {
	message receivedSMS
	indexes []int
}

type smsFragmentGroup struct {
	sender  string
	total   int
	parts   map[int]string
	indexes []int
	last    receivedSMS
}

func (a *app) scanSMSStorage(memory string) ([]indexedSMS, error) {
	if memory != "SM" && memory != "ME" {
		return nil, errors.New("invalid SMS storage")
	}
	if _, err := a.runATCommand(fmt.Sprintf(`AT+CPMS="%s","%s","%s"`, memory, memory, memory), 5*time.Second); err != nil {
		return nil, fmt.Errorf("select storage: %w", err)
	}
	response, err := a.runATCommand("AT+CMGL=4", 15*time.Second)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}

	var candidates []indexedSMS
	groups := make(map[string]*smsFragmentGroup)
	for _, item := range parseUSBATCMGL(response) {
		message, concat, err := decodeUSBATPDU(item.header, item.pdu)
		if err != nil {
			continue
		}
		message.Memory = memory
		message.Index = item.index
		if !concat.IsConcat {
			candidates = append(candidates, indexedSMS{message: message, indexes: []int{item.index}})
			continue
		}
		key := fmt.Sprintf("%s\x00%d\x00%d\x00%d", message.Sender, concat.Ref, concat.RefBits, concat.Total)
		group := groups[key]
		if group == nil {
			group = &smsFragmentGroup{sender: message.Sender, total: concat.Total, parts: make(map[int]string)}
			groups[key] = group
		}
		group.parts[concat.Seq] = message.Content
		group.indexes = append(group.indexes, item.index)
		group.last = message
	}
	for _, group := range groups {
		if len(group.parts) != group.total {
			continue
		}
		var content strings.Builder
		complete := true
		for sequence := 1; sequence <= group.total; sequence++ {
			part, ok := group.parts[sequence]
			if !ok {
				complete = false
				break
			}
			content.WriteString(part)
		}
		if !complete {
			continue
		}
		message := group.last
		message.Sender = group.sender
		message.Content = content.String()
		candidates = append(candidates, indexedSMS{message: message, indexes: group.indexes})
	}
	return candidates, nil
}

func parseSMSStorage(response string) string {
	match := regexp.MustCompile(`\+CPMS:\s*"(SM|ME)"`).FindStringSubmatch(strings.ToUpper(response))
	if len(match) != 2 {
		return ""
	}
	return match[1]
}
