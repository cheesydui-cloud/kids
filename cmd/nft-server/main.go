// nft-server is the panel binary: the web UI (embedded), JSON API, agent hub,
// and SQLite store. The node-side daemon/TUI lives in the separate nft-agent
// binary, which the panel pushes to managed nodes over the WS link.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"nft/internal/db"
	"nft/internal/server"
	_ "nft/internal/tlsfallback"
)

func main() {
	args := os.Args[1:]
	// Tolerate the legacy "server" subcommand from the single-binary era so old
	// systemd units (ExecStart=… server --addr) keep working through an upgrade.
	if len(args) > 0 && args[0] == "server" {
		args = args[1:]
	}
	if len(args) > 0 {
		switch args[0] {
		case "export":
			os.Exit(runExport(args[1:]))
		case "import":
			os.Exit(runImport(args[1:]))
		}
	}
	os.Exit(runServer(args))
}

func runServer(args []string) int {
	var (
		addr, dbPath, bootstrapPw    string
		resetAdminPw, resetAdminUser string
		backupInterval               time.Duration
		backupKeep                   int
		auditRetentionDays           int
		hourlyTrafficRetentionDays   int
		dailyTrafficRetentionDays    int
	)
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	fs.StringVar(&addr, "addr", ":7788", "panel HTTP address")
	fs.StringVar(&dbPath, "db", "/var/lib/nft/panel.db", "SQLite database path")
	fs.StringVar(&bootstrapPw, "bootstrap-admin-password", "", "set admin password on first boot")
	fs.StringVar(&resetAdminPw, "reset-admin-password", "", "reset admin password and exit")
	fs.StringVar(&resetAdminUser, "reset-admin-username", "admin", "admin username for reset")
	fs.DurationVar(&backupInterval, "backup-interval", 24*time.Hour, "local DB backup interval (0 disables)")
	fs.IntVar(&backupKeep, "backup-keep", 14, "number of local DB backups to retain")
	fs.IntVar(&auditRetentionDays, "audit-retention-days", 180, "audit log retention in days (0 disables cleanup)")
	fs.IntVar(&hourlyTrafficRetentionDays, "traffic-hourly-retention-days", 30, "hourly traffic retention in days (0 disables cleanup)")
	fs.IntVar(&dailyTrafficRetentionDays, "traffic-daily-retention-days", 730, "daily traffic retention in days (0 disables cleanup)")
	fs.Parse(args)
	if bootstrapPw == "" {
		bootstrapPw = os.Getenv("NFT_BOOTSTRAP_ADMIN_PASSWORD")
	}
	if bootstrapPw == "" {
		bootstrapPw = consumeBootstrapPasswordFile(dbPath)
	}
	if resetAdminPw == "" {
		resetAdminPw = os.Getenv("NFT_RESET_ADMIN_PASSWORD")
	}

	if resetAdminPw != "" {
		return runResetAdmin(dbPath, resetAdminUser, resetAdminPw)
	}

	if err := db.ApplyStagedMigrate(dbPath); err != nil {
		log.Fatalf("apply staged migrate: %v", err)
	}
	d, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer d.Close()
	if err := bootstrap(d, dbPath, bootstrapPw); err != nil {
		log.Fatalf("bootstrap: %v", err)
	}
	stopBackups := db.StartBackups(d, dbPath, backupInterval, backupKeep)
	defer stopBackups()
	stopRetention := db.StartHistoryCleanup(d, db.RetentionPolicy{
		AuditDays: auditRetentionDays, HourlyTrafficDays: hourlyTrafficRetentionDays, DailyTrafficDays: dailyTrafficRetentionDays,
	})
	defer stopRetention()
	if err := server.EnsureContainerUpgradeHelper(); err != nil {
		log.Printf("nft-upgrade helper: %v", err)
	}

	// Doc images live next to the SQLite file so backups/migrations of the
	// data directory keep text and assets together.
	docsDir := filepath.Join(filepath.Dir(dbPath), "docs-assets")
	srv, err := server.NewWithDocsDir(d, docsDir)
	if err != nil {
		log.Fatalf("server: %v", err)
	}
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.Router(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		log.Printf("nft server listening on %s", addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	srv.Hub.Close() // send StatusGoingAway to agents before tearing down
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(ctx)
	return 0
}

func runResetAdmin(dbPath, username, newPw string) int {
	d, err := db.Open(dbPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "打开数据库:", err)
		return 1
	}
	defer d.Close()

	msg, err := server.ResetAdminPassword(d, username, newPw)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println(msg)
	return 0
}

func runExport(args []string) int {
	var dbPath, out string
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	fs.StringVar(&dbPath, "db", "/var/lib/nft/panel.db", "SQLite database path")
	fs.StringVar(&out, "o", "", "output .tgz path")
	fs.Parse(args)
	if out == "" {
		out = "kids-migrate-" + time.Now().Format("20060102-150405") + ".tgz"
	}
	d, err := db.Open(dbPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "打开数据库:", err)
		return 1
	}
	defer d.Close()
	dataDir := db.DataDirFromDBPath(dbPath)
	brandDir := filepath.Join(dataDir, "brand")
	docsDir := filepath.Join(dataDir, "docs-assets")
	man, err := server.ExportPanelToFile(d, brandDir, docsDir, out, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "导出失败:", err)
		return 1
	}
	fmt.Printf("已导出 %s（用户 %d / 节点 %d / 规则 %d）\n", out, man.Users, man.Nodes, man.Rules)
	fmt.Println("文件含订阅口令和设置密钥，按机密保存。")
	return 0
}

func runImport(args []string) int {
	var dbPath string
	var noRestart bool
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	fs.StringVar(&dbPath, "db", "/var/lib/nft/panel.db", "SQLite database path")
	fs.BoolVar(&noRestart, "no-restart", false, "only stage; do not restart nft-server")
	fs.Parse(args)
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "用法: nft-server import [-db /var/lib/nft/panel.db] <搬家包.tgz>")
		return 2
	}
	dataDir := db.DataDirFromDBPath(dbPath)
	if dataDir == "" {
		fmt.Fprintln(os.Stderr, "无法从数据库路径推断数据目录")
		return 1
	}
	man, err := server.ImportPanelFromFile(fs.Arg(0), dataDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "导入失败:", err)
		return 1
	}
	fmt.Printf("已校验并暂存（用户 %d / 节点 %d / 规则 %d）\n", man.Users, man.Nodes, man.Rules)
	if noRestart {
		fmt.Println("下次启动面板时会覆盖本机数据。")
		return 0
	}
	fmt.Println("正在重启面板以套用数据…")
	cmd := execCommand("systemctl", "restart", "nft-server.service")
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintln(os.Stderr, "自动重启失败:", strings.TrimSpace(string(out)), err)
		fmt.Fprintln(os.Stderr, "请手动执行: systemctl restart nft-server")
		return 1
	}
	fmt.Println("已重启。约 10 秒后用原账号登录新面板。")
	return 0
}

// execCommand is os/exec.Command; tests do not import this file.
var execCommand = exec.Command

func bootstrap(d *sql.DB, dbPath, pw string) error {
	n, err := db.CountUsers(d)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	generated := pw == ""
	if generated {
		pw = db.RandToken(8)
	}
	hash, err := server.HashPassword(pw)
	if err != nil {
		return err
	}
	if _, err := db.CreateUser(d, "admin", hash, "admin"); err != nil {
		return err
	}
	fmt.Println("================================================")
	fmt.Println(" 首次启动 - 已创建管理员账号")
	fmt.Println(" 用户名: admin")
	if generated {
		notice, nerr := initialPasswordNotice(dbPath, pw, stdoutIsTTY())
		if nerr != nil {
			return nerr
		}
		fmt.Print(notice)
	} else {
		fmt.Println(" 已使用启动时提供的管理员密码。")
	}
	fmt.Println("================================================")
	return nil
}

// consumeBootstrapPasswordFile reads a one-shot password placed in the data
// volume by the Docker installer. The file is removed so later starts, and
// docker inspect, do not keep it. An empty file is ignored.
func consumeBootstrapPasswordFile(dbPath string) string {
	path := filepath.Join(filepath.Dir(dbPath), "bootstrap-admin-password")
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	_ = os.Remove(path)
	return strings.TrimSpace(string(b))
}

func stdoutIsTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// initialPasswordNotice stores a generated password beside the database.
// Interactive terminals also print it. Container logs only get the file path,
// so docker logs does not keep the password.
func initialPasswordNotice(dbPath, pw string, tty bool) (string, error) {
	path := filepath.Join(filepath.Dir(dbPath), "initial-admin-password")
	if err := os.WriteFile(path, []byte(pw+"\n"), 0o600); err != nil {
		return "", err
	}
	var b strings.Builder
	if tty {
		fmt.Fprintf(&b, " 密  码: %s\n", pw)
	}
	fmt.Fprintf(&b, " 初始密码已写入 %s\n", path)
	b.WriteString(" 请登录后立即修改，并删除该文件。\n")
	return b.String(), nil
}
