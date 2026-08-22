package server

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"nft/internal/db"
)

const (
	panelLogoSetting = "panel_logo"
	logoUploadMax    = 1 << 20 // 1 MiB
)

func (s *Server) brandDir() string {
	docs := strings.TrimSpace(s.DocsDir)
	if docs == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(docs), "brand")
}

func (s *Server) ensureBrandDir() (string, error) {
	dir := s.brandDir()
	if dir == "" {
		return "", fmt.Errorf("brand directory is not configured")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func logoURLFor(filename string) string {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return ""
	}
	return "/api/branding/logo"
}

func detectLogoExt(head []byte, filename string) (string, string, bool) {
	kind := http.DetectContentType(head)
	switch {
	case strings.HasPrefix(kind, "image/png"):
		return ".png", "image/png", true
	case kind == "image/jpeg":
		return ".jpg", "image/jpeg", true
	case kind == "image/gif":
		return ".gif", "image/gif", true
	case kind == "image/webp":
		return ".webp", "image/webp", true
	}
	trimmed := strings.TrimSpace(string(head))
	if strings.HasPrefix(trimmed, "<svg") || strings.HasPrefix(trimmed, "<?xml") {
		return ".svg", "image/svg+xml", true
	}
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".png":
		return ".png", "image/png", true
	case ".jpg", ".jpeg":
		return ".jpg", "image/jpeg", true
	case ".gif":
		return ".gif", "image/gif", true
	case ".webp":
		return ".webp", "image/webp", true
	case ".svg":
		return ".svg", "image/svg+xml", true
	}
	return "", "", false
}

func (s *Server) brandingPayload() map[string]any {
	panelName, _ := db.GetSetting(s.DB, "panel_name")
	logo, _ := db.GetSetting(s.DB, panelLogoSetting)
	return map[string]any{
		"panel_name": panelName,
		"logo_url":   logoURLFor(logo),
		"panel_skin": s.panelSkin(),
	}
}

func (s *Server) apiBranding(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, s.brandingPayload())
}

func (s *Server) apiServeBrandingLogo(w http.ResponseWriter, r *http.Request) {
	name, _ := db.GetSetting(s.DB, panelLogoSetting)
	name = strings.TrimSpace(name)
	if name == "" {
		http.NotFound(w, r)
		return
	}
	name = filepath.Base(name)
	// filepath.Base("") is ".", which would make ServeFile redirect the brand dir.
	if name == "" || name == "." || name == ".." || strings.Contains(name, "..") {
		http.NotFound(w, r)
		return
	}
	dir := s.brandDir()
	if dir == "" {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, path)
}

func (s *Server) removeStoredLogo() {
	name, _ := db.GetSetting(s.DB, panelLogoSetting)
	name = strings.TrimSpace(name)
	if name == "" {
		_ = db.SetSetting(s.DB, panelLogoSetting, "")
		return
	}
	name = filepath.Base(name)
	if name == "" || name == "." || name == ".." {
		_ = db.SetSetting(s.DB, panelLogoSetting, "")
		return
	}
	if dir := s.brandDir(); dir != "" {
		_ = os.Remove(filepath.Join(dir, name))
	}
	_ = db.SetSetting(s.DB, panelLogoSetting, "")
}

func (s *Server) apiUploadPanelLogo(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	dir, err := s.ensureBrandDir()
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, logoUploadMax+512*1024)
	if err := r.ParseMultipartForm(logoUploadMax + 512*1024); err != nil {
		jsonErr(w, http.StatusBadRequest, "图片太大或格式无效（最大 1MB）")
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "请选择图片")
		return
	}
	defer file.Close()

	head := make([]byte, 512)
	n, _ := io.ReadFull(file, head)
	head = head[:n]
	if n == 0 {
		jsonErr(w, http.StatusBadRequest, "空文件")
		return
	}
	ext, _, ok := detectLogoExt(head, hdr.Filename)
	if !ok {
		jsonErr(w, http.StatusBadRequest, "仅支持 png / jpg / gif / webp / svg")
		return
	}

	tmp, err := os.CreateTemp(dir, "logo-upload-*")
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "无法写入 Logo")
		return
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	written, err := tmp.Write(head)
	if err != nil {
		tmp.Close()
		jsonErr(w, http.StatusInternalServerError, "无法写入 Logo")
		return
	}
	rest, err := io.Copy(tmp, io.LimitReader(file, logoUploadMax+1-int64(written)))
	tmp.Close()
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "无法写入 Logo")
		return
	}
	if int64(written)+rest > logoUploadMax {
		jsonErr(w, http.StatusBadRequest, "图片太大（最大 1MB）")
		return
	}

	finalName := "logo" + ext
	finalPath := filepath.Join(dir, finalName)
	s.removeStoredLogo()
	if err := os.Rename(tmpPath, finalPath); err != nil {
		jsonErr(w, http.StatusInternalServerError, "无法保存 Logo")
		return
	}
	if err := db.SetSetting(s.DB, panelLogoSetting, finalName); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	db.WriteAudit(s.DB, u.ID, "settings.panel_logo", "updated", "")
	jsonOK(w, s.brandingPayload())
}

func (s *Server) apiClearPanelLogo(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	s.removeStoredLogo()
	db.WriteAudit(s.DB, u.ID, "settings.panel_logo", "cleared", "")
	jsonOK(w, s.brandingPayload())
}
