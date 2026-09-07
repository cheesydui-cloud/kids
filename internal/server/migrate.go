package server

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"nft/internal/db"
)

const (
	migrateUploadMax = 1 << 30 // 1 GiB
	migrateConfirm   = "覆盖本机数据"
)

var restartPanelFn = restartPanelDefault

func (s *Server) dataDir() string {
	docs := strings.TrimSpace(s.DocsDir)
	if docs == "" {
		return ""
	}
	return filepath.Dir(docs)
}

func (s *Server) brandDirForMigrate() string {
	if dir := s.brandDir(); dir != "" {
		return dir
	}
	if d := s.dataDir(); d != "" {
		return filepath.Join(d, "brand")
	}
	return ""
}

func (s *Server) apiExportMigrate(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	name := "kids-migrate-" + time.Now().Format("20060102-150405") + ".tgz"
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	man, err := db.WriteMigrateArchive(s.DB, s.brandDirForMigrate(), s.DocsDir, serverVersion(), w)
	if err != nil {
		http.Error(w, `{"error":"导出失败"}`, http.StatusInternalServerError)
		return
	}
	if u != nil && man != nil {
		db.WriteAudit(s.DB, u.ID, "settings.migrate_export", "",
			fmt.Sprintf("users=%d nodes=%d rules=%d", man.Users, man.Nodes, man.Rules))
	}
}

func (s *Server) apiImportMigrate(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	dataDir := s.dataDir()
	if dataDir == "" {
		jsonErr(w, http.StatusInternalServerError, "面板数据目录未知，无法导入")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, migrateUploadMax+512*1024)
	if err := r.ParseMultipartForm(migrateUploadMax + 512*1024); err != nil {
		jsonErr(w, http.StatusBadRequest, "文件过大或格式不对（最大 1GB）")
		return
	}
	confirm := strings.TrimSpace(r.FormValue("confirm"))
	if confirm != migrateConfirm {
		jsonErr(w, http.StatusBadRequest, "请输入「"+migrateConfirm+"」确认覆盖")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "请选择搬家包")
		return
	}
	defer file.Close()

	tmp, err := os.MkdirTemp("", "kids-migrate-upload-*")
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer os.RemoveAll(tmp)

	extract := filepath.Join(tmp, "extract")
	man, err := db.ExtractMigrateArchive(file, extract)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := db.StageIncoming(dataDir, extract); err != nil {
		jsonErr(w, http.StatusInternalServerError, "暂存导入失败: "+err.Error())
		return
	}
	if u != nil {
		db.WriteAudit(s.DB, u.ID, "settings.migrate_import", "",
			fmt.Sprintf("users=%d nodes=%d rules=%d", man.Users, man.Nodes, man.Rules))
	}
	if err := restartPanelFn(); err != nil {
		jsonOK(w, map[string]any{
			"ok":      true,
			"restart": false,
			"users":   man.Users,
			"nodes":   man.Nodes,
			"rules":   man.Rules,
			"message": "数据已就绪，但自动重启失败，请 SSH 执行 systemctl restart nft-server",
		})
		return
	}
	jsonOK(w, map[string]any{
		"ok":      true,
		"restart": true,
		"users":   man.Users,
		"nodes":   man.Nodes,
		"rules":   man.Rules,
		"message": "正在重启面板并套用数据，约 10 秒后刷新",
	})
}

func restartPanelDefault() error {
	script := `#!/usr/bin/env bash
set -e
sleep 2
if command -v systemctl >/dev/null 2>&1; then
  systemctl restart nft-server.service
  exit 0
fi
`
	tmp, err := os.CreateTemp("", "kids-migrate-restart-*.sh")
	if err != nil {
		return err
	}
	path := tmp.Name()
	if _, err := tmp.WriteString(script); err != nil {
		tmp.Close()
		_ = os.Remove(path)
		return err
	}
	if err := tmp.Chmod(0o700); err != nil {
		tmp.Close()
		_ = os.Remove(path)
		return err
	}
	_ = tmp.Close()

	if err := trySystemdRun(path); err == nil {
		return nil
	}
	cmd := exec.Command("bash", path)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("无法重启面板: %w", err)
	}
	go func() { _ = cmd.Wait(); _ = os.Remove(path) }()
	return nil
}

// ExportPanelToFile writes a migrate archive to dest (used by nft-server export).
func ExportPanelToFile(d *sql.DB, brandDir, docsDir, dest, version string) (*db.MigrateManifest, error) {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil && filepath.Dir(dest) != "." && filepath.Dir(dest) != "" {
		return nil, err
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	man, werr := db.WriteMigrateArchive(d, brandDir, docsDir, version, f)
	cerr := f.Close()
	if werr != nil {
		_ = os.Remove(dest)
		return nil, werr
	}
	if cerr != nil {
		_ = os.Remove(dest)
		return nil, cerr
	}
	return man, nil
}

// ImportPanelFromFile stages a migrate archive for the next process start.
func ImportPanelFromFile(archivePath, dataDir string) (*db.MigrateManifest, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tmp, err := os.MkdirTemp("", "kids-migrate-cli-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	extract := filepath.Join(tmp, "extract")
	man, err := db.ExtractMigrateArchive(f, extract)
	if err != nil {
		return nil, err
	}
	if err := db.StageIncoming(dataDir, extract); err != nil {
		return nil, err
	}
	return man, nil
}
