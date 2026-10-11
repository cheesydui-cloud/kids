package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"nft/internal/db"
)

const (
	nodeAlertTick         = 15 * time.Second
	nodeAlertOfflineAfter = 60 * time.Second
	nodeAlertHTTPTimeout  = 5 * time.Second
)

// nodeAlert is one operator notice. Host CPU and disk stay on the external
// monitor link; this only covers line offline, recovery, and sync failure.
type nodeAlert struct {
	Event  string `json:"event"`
	NodeID int64  `json:"node_id"`
	Name   string `json:"node_name"`
	Relay  string `json:"relay_host,omitempty"`
	Detail string `json:"detail,omitempty"`
	Text   string `json:"text"`
	At     int64  `json:"at"`
}

var alertHTTP = &http.Client{Timeout: nodeAlertHTTPTimeout}

func (s *Server) nodeAlertLoop() {
	ticker := time.NewTicker(nodeAlertTick)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopAlerts:
			return
		case <-ticker.C:
			s.scanNodeAlerts(time.Now())
		}
	}
}

func (s *Server) scanNodeAlerts(now time.Time) {
	webhook, _ := db.GetSetting(s.DB, "alert_webhook_url")
	token, _ := db.GetSetting(s.DB, "alert_telegram_bot_token")
	chat, _ := db.GetSetting(s.DB, "alert_telegram_chat_id")
	webhook = strings.TrimSpace(webhook)
	token = strings.TrimSpace(token)
	chat = strings.TrimSpace(chat)
	if webhook == "" && (token == "" || chat == "") {
		return
	}
	nodes, err := db.ListNodes(s.DB)
	if err != nil {
		log.Printf("alert: list nodes: %v", err)
		return
	}
	s.reconcileNodeOnline(nodes)
	states, err := db.LoadNodeAlertStates(s.DB)
	if err != nil {
		log.Printf("alert: load state: %v", err)
		return
	}
	nowUnix := now.Unix()
	for _, n := range nodes {
		if n == nil || n.NodeType == "self" {
			continue
		}
		prev := states[n.ID]
		next, events, drop := planNodeAlert(n, prev, nowUnix)
		if drop {
			if prev.NodeID != 0 {
				if err := db.DeleteNodeAlertState(s.DB, n.ID); err != nil {
					log.Printf("alert: clear %d: %v", n.ID, err)
				}
			}
			continue
		}
		if len(events) == 0 {
			if alertStateChanged(prev, next) {
				if err := db.SaveNodeAlertState(s.DB, next); err != nil {
					log.Printf("alert: save %d: %v", n.ID, err)
				}
			}
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), nodeAlertHTTPTimeout*2)
		err := postNodeAlerts(ctx, webhook, token, chat, events)
		cancel()
		if err != nil {
			log.Printf("alert: send node %d: %v", n.ID, err)
			fallback := prev
			fallback.NodeID = n.ID
			fallback.OfflineSince = next.OfflineSince
			if err := db.SaveNodeAlertState(s.DB, fallback); err != nil {
				log.Printf("alert: save clock %d: %v", n.ID, err)
			}
			continue
		}
		if err := db.SaveNodeAlertState(s.DB, next); err != nil {
			log.Printf("alert: save %d: %v", n.ID, err)
		}
	}
}

func alertStateChanged(a, b db.NodeAlertState) bool {
	return a.OfflineSince != b.OfflineSince || a.OfflineAlerted != b.OfflineAlerted || a.LastError != b.LastError
}

// planNodeAlert decides the next stored clock and any notices to send.
// drop means the row should be deleted (disabled, composite, or not an agent).
func planNodeAlert(n *db.Node, prev db.NodeAlertState, now int64) (next db.NodeAlertState, events []nodeAlert, drop bool) {
	if n == nil || n.Disabled || n.NodeType == "composite" || n.NodeType == "self" {
		return db.NodeAlertState{}, nil, true
	}
	errText := ""
	if n.LastError.Valid {
		errText = firstAlertLine(n.LastError.String)
	}
	relay := strings.TrimSpace(n.RelayHost)
	if relay == "" {
		relay = strings.TrimSpace(n.RelayHostV6)
	}
	next = prev
	next.NodeID = n.ID
	if n.Online == 1 {
		if prev.OfflineAlerted {
			events = append(events, makeAlert("online", n, relay, "", now))
		}
		next.OfflineSince = 0
		next.OfflineAlerted = false
		if errText != "" && errText != prev.LastError {
			events = append(events, makeAlert("sync_error", n, relay, errText, now))
		}
		next.LastError = errText
		return next, events, false
	}
	if next.OfflineSince == 0 {
		if n.LastSeen != nil && *n.LastSeen > 0 {
			next.OfflineSince = *n.LastSeen
		} else {
			next.OfflineSince = now
		}
	}
	if !prev.OfflineAlerted && now-next.OfflineSince >= int64(nodeAlertOfflineAfter/time.Second) {
		events = append(events, makeAlert("offline", n, relay, "", now))
		next.OfflineAlerted = true
	}
	if errText != "" && errText != prev.LastError {
		events = append(events, makeAlert("sync_error", n, relay, errText, now))
	}
	next.LastError = errText
	return next, events, false
}

func makeAlert(event string, n *db.Node, relay, detail string, now int64) nodeAlert {
	ev := nodeAlert{
		Event: event, NodeID: n.ID, Name: n.Name, Relay: relay, Detail: detail, At: now,
	}
	ev.Text = ev.text()
	return ev
}

func (ev nodeAlert) text() string {
	name := ev.Name
	if name == "" {
		name = fmt.Sprintf("#%d", ev.NodeID)
	}
	relay := ""
	if ev.Relay != "" {
		relay = "（" + ev.Relay + "）"
	}
	switch ev.Event {
	case "offline":
		return fmt.Sprintf("节点 %s%s 已离线超过 1 分钟", name, relay)
	case "online":
		return fmt.Sprintf("节点 %s%s 已恢复在线", name, relay)
	case "sync_error":
		detail := ev.Detail
		if detail == "" {
			detail = "同步失败"
		}
		return fmt.Sprintf("节点 %s%s 同步失败：%s", name, relay, detail)
	default:
		return fmt.Sprintf("节点 %s%s %s", name, relay, ev.Event)
	}
}

func firstAlertLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if utf8.RuneCountInString(s) > 180 {
		r := []rune(s)
		s = string(r[:180]) + "…"
	}
	return s
}

func postNodeAlerts(ctx context.Context, webhook, token, chat string, events []nodeAlert) error {
	if webhook == "" && (token == "" || chat == "") {
		return errors.New("no alert channel")
	}
	var errs []error
	for _, ev := range events {
		if webhook != "" {
			if err := postAlertWebhook(ctx, webhook, ev); err != nil {
				errs = append(errs, err)
			}
		}
		if token != "" && chat != "" {
			if err := postAlertTelegram(ctx, token, chat, ev.text()); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func postAlertWebhook(ctx context.Context, webhook string, ev nodeAlert) error {
	body, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := alertHTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1024))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("webhook status %d", res.StatusCode)
	}
	return nil
}

func postAlertTelegram(ctx context.Context, token, chat, text string) error {
	payload, _ := json.Marshal(map[string]string{"chat_id": chat, "text": text})
	url := "https://api.telegram.org/bot" + token + "/sendMessage"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := alertHTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1024))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("telegram status %d", res.StatusCode)
	}
	return nil
}

func maskedSecret(raw string) (bool, string) {
	t := strings.TrimSpace(raw)
	if t == "" {
		return false, ""
	}
	if len(t) > 8 {
		return true, t[:4] + "…" + t[len(t)-4:]
	}
	return true, "****"
}

func normalizeTelegramToken(raw string) (string, error) {
	t := strings.TrimSpace(raw)
	if t == "" {
		return "", nil
	}
	if strings.ContainsAny(t, " \t\r\n") || len(t) < 20 || len(t) > 128 {
		return "", errors.New("Telegram Bot Token 格式不对")
	}
	return t, nil
}

func normalizeTelegramChat(raw string) (string, error) {
	t := strings.TrimSpace(raw)
	if t == "" {
		return "", nil
	}
	if len(t) > 64 || strings.ContainsAny(t, " \t\r\n") {
		return "", errors.New("Telegram Chat ID 格式不对")
	}
	return t, nil
}
