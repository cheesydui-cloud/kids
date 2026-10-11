package main

import (
	"fmt"
	"os"
	"time"

	"nft/internal/daemon"
	"nft/internal/daemonclient"
)

// runStatus prints a Chinese snapshot from the local daemon. It does not
// open the TUI. A daemon that is up but not connected to the panel still
// prints; only a missing socket is an error.
func runStatus() int {
	client, err := daemonclient.New(daemonclient.DefaultSocketPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "无法连接 nft daemon:", err)
		return 1
	}
	st, err := client.Status()
	if err != nil {
		fmt.Fprintln(os.Stderr, "无法连接 nft daemon:", err)
		fmt.Fprintln(os.Stderr, "请先启动 daemon：sudo systemctl start nft-daemon.service")
		return 1
	}
	if st.Connected {
		name := st.NodeName
		if name == "" {
			name = "未命名"
		}
		fmt.Printf("面板：已连接（%s #%d）\n", name, st.NodeID)
	} else {
		fmt.Println("面板：未连接")
	}
	ver := st.Version
	if ver == "" {
		ver = "未知"
	}
	fmt.Printf("版本：%s\n", ver)
	if st.LastDisconnect != "" {
		fmt.Printf("上次断开：%s%s\n", daemon.DisconnectReasonLabel(st.LastDisconnect), agoSuffix(st.LastDisconnectAt))
	} else {
		fmt.Println("上次断开：无")
	}
	rev := st.LastSyncRev
	if rev == "" {
		rev = "无"
	}
	fmt.Printf("最近同步版本：%s\n", rev)
	unres := st.Unresolved
	if unres == "" {
		unres = "无"
	}
	fmt.Printf("未解析域名：%s\n", unres)
	if st.NftAvailable {
		fmt.Println("nft：可用")
	} else {
		fmt.Println("nft：不可用")
	}
	iface := st.TCIface
	if iface == "" {
		iface = "未设置"
	}
	fmt.Printf("流量控制网卡：%s\n", iface)
	if st.ResourceWarning != "" {
		fmt.Printf("资源水位：%s\n", st.ResourceWarning)
	} else {
		fmt.Println("资源水位：正常")
	}
	fmt.Printf("用户态连接：%d\n", st.RelayConns)
	ct := st.Conntrack
	if ct == "" {
		ct = "不可用"
	}
	fmt.Printf("连接跟踪：%s\n", ct)
	fds := st.FDs
	if fds == "" {
		fds = "不可用"
	}
	fmt.Printf("文件描述符：%s\n", fds)
	v4, v6 := st.ProbedV4, st.ProbedV6
	if v4 == "" {
		v4 = "无"
	}
	if v6 == "" {
		v6 = "无"
	}
	fmt.Printf("探测出口：%s / %s\n", v4, v6)
	if st.UpgradePending {
		fmt.Println("升级待确认：是")
	} else {
		fmt.Println("升级待确认：否")
	}
	return 0
}

func agoSuffix(unix int64) string {
	if unix <= 0 {
		return ""
	}
	d := time.Since(time.Unix(unix, 0))
	if d < 0 {
		return ""
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("（%d 秒前）", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("（%d 分钟前）", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("（%d 小时前）", int(d.Hours()))
	default:
		return fmt.Sprintf("（%d 天前）", int(d.Hours()/24))
	}
}
