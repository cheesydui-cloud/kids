package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// disconnectRecord is the on-disk last drop, next to state.json. A separate
// file avoids rewriting state.json from the dialer while reconcile holds it.
type disconnectRecord struct {
	Reason string `json:"reason"`
	At     int64  `json:"at"`
}

// shouldPersistDisconnect skips a clean stop and a context cancel. An upgrade
// restart arrives as one of those and must not overwrite the real reason.
func shouldPersistDisconnect(reason string) bool {
	switch reason {
	case "", "agent_stop", "context_canceled":
		return false
	default:
		return true
	}
}

func saveDisconnect(path, reason string, at int64) {
	if path == "" || !shouldPersistDisconnect(reason) {
		return
	}
	if at <= 0 {
		at = time.Now().Unix()
	}
	b, err := json.Marshal(disconnectRecord{Reason: reason, At: at})
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

func loadDisconnect(path string) (string, int64) {
	if path == "" {
		return "", 0
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", 0
	}
	var rec disconnectRecord
	if json.Unmarshal(b, &rec) != nil {
		return "", 0
	}
	return rec.Reason, rec.At
}

// DisconnectReasonLabel maps a stable English reconnect code to the Chinese
// label shown by `nft-agent status`. The panel uses the same codes.
func DisconnectReasonLabel(code string) string {
	switch code {
	case "watchdog_stale":
		return "看门狗超时"
	case "dial_fail":
		return "拨号失败"
	case "hello_rejected":
		return "面板拒绝"
	case "hello_timeout", "dial_or_hello_timeout":
		return "握手超时"
	case "hello_write_fail":
		return "握手发送失败"
	case "connection_drop":
		return "连接中断"
	case "dial_drop":
		return "拨号中断"
	case "panel_going_away":
		return "面板重启"
	case "panel_close":
		return "面板关闭"
	case "panel_policy":
		return "面板拒绝策略"
	case "read_timeout":
		return "读超时"
	case "short_session":
		return "刚连上就断开"
	case "session_panic":
		return "会话异常"
	case "session_error":
		return "会话错误"
	case "pre_hello_fail":
		return "握手前失败"
	case "agent_stop":
		return "主动停止"
	case "context_canceled":
		return "进程退出"
	default:
		return code
	}
}
