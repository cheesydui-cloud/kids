package db

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	MigrateFormatV1     = "kids-migrate/v1"
	migrateManifestName = "manifest.json"
	migrateDBName       = "panel.db"
	migrateBrandDir     = "brand"
	migrateDocsDir      = "docs-assets"
	MigrateIncomingDir  = "migrate-incoming"
	MigrateReadyName    = "ready"
	// MaxExtractBytes caps uncompressed payload so a crafted archive cannot fill the disk.
	MaxExtractBytes int64 = 2 << 30
)

// MigrateManifest describes a lossless panel snapshot. File hashes are SHA-256 of
// the archived bytes (slash-separated paths, no leading "./").
type MigrateManifest struct {
	Format       string            `json:"format"`
	CreatedAt    int64             `json:"created_at"`
	PanelVersion string            `json:"panel_version"`
	Users        int               `json:"users"`
	Nodes        int               `json:"nodes"`
	Rules        int               `json:"rules"`
	Files        map[string]string `json:"files"`
}

func CountNodes(d *sql.DB) (int, error) {
	return count(d, `SELECT COUNT(*) FROM nodes`)
}

func CountRules(d *sql.DB) (int, error) {
	return count(d, `SELECT COUNT(*) FROM rules`)
}

// BundledMigrationNames is the panel binary's migration set, sorted.
func BundledMigrationNames() ([]string, error) {
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// WriteMigrateArchive writes a gzip tar of a consistent DB snapshot plus brand
// and docs-assets directories. d must already be migrated (the live panel DB).
func WriteMigrateArchive(d *sql.DB, brandDir, docsDir, panelVersion string, w io.Writer) (*MigrateManifest, error) {
	tmp, err := os.MkdirTemp("", "kids-migrate-export-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	snap := filepath.Join(tmp, migrateDBName)
	if err := Backup(d, snap); err != nil {
		return nil, fmt.Errorf("快照数据库: %w", err)
	}

	users, err := CountUsers(d)
	if err != nil {
		return nil, err
	}
	nodes, err := CountNodes(d)
	if err != nil {
		return nil, err
	}
	rules, err := CountRules(d)
	if err != nil {
		return nil, err
	}

	files := map[string]string{}
	dbHash, err := hashFile(snap)
	if err != nil {
		return nil, err
	}
	files[migrateDBName] = dbHash

	man := &MigrateManifest{
		Format:       MigrateFormatV1,
		CreatedAt:    time.Now().Unix(),
		PanelVersion: strings.TrimSpace(panelVersion),
		Users:        users,
		Nodes:        nodes,
		Rules:        rules,
		Files:        files,
	}

	gw := gzip.NewWriter(w)
	tw := tar.NewWriter(gw)

	if err := tarRegular(tw, migrateDBName, snap, files); err != nil {
		_ = tw.Close()
		_ = gw.Close()
		return nil, err
	}
	if err := tarTree(tw, brandDir, migrateBrandDir, files); err != nil {
		_ = tw.Close()
		_ = gw.Close()
		return nil, err
	}
	if err := tarTree(tw, docsDir, migrateDocsDir, files); err != nil {
		_ = tw.Close()
		_ = gw.Close()
		return nil, err
	}

	man.Files = files
	raw, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		_ = tw.Close()
		_ = gw.Close()
		return nil, err
	}
	if err := tarBytes(tw, migrateManifestName, raw); err != nil {
		_ = tw.Close()
		_ = gw.Close()
		return nil, err
	}
	if err := tw.Close(); err != nil {
		_ = gw.Close()
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}
	return man, nil
}

func tarRegular(tw *tar.Writer, name, src string, files map[string]string) error {
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	h, err := tar.FileInfoHeader(st, "")
	if err != nil {
		return err
	}
	h.Name = name
	h.Uid, h.Gid, h.Uname, h.Gname = 0, 0, "", ""
	if err := tw.WriteHeader(h); err != nil {
		return err
	}
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(tw, f)
	if files != nil {
		sum, herr := hashFile(src)
		if herr != nil {
			return herr
		}
		files[name] = sum
	}
	return err
}

func tarBytes(tw *tar.Writer, name string, body []byte) error {
	h := &tar.Header{
		Name:     name,
		Mode:     0600,
		Size:     int64(len(body)),
		ModTime:  time.Now(),
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(h); err != nil {
		return err
	}
	_, err := tw.Write(body)
	return err
}

func tarTree(tw *tar.Writer, srcDir, prefix string, files map[string]string) error {
	srcDir = strings.TrimSpace(srcDir)
	if srcDir == "" {
		return nil
	}
	st, err := os.Stat(srcDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("%s is not a directory", srcDir)
	}
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		name := prefix + "/" + filepath.ToSlash(rel)
		if info.IsDir() {
			h, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			h.Name = name + "/"
			h.Uid, h.Gid, h.Uname, h.Gname = 0, 0, "", ""
			return tw.WriteHeader(h)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		return tarRegular(tw, name, path, files)
	})
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ExtractMigrateArchive unpacks a migrate tarball into destDir (created empty).
// It verifies format, file hashes, that panel.db opens, and that the dump is not
// from a newer panel than this binary.
func ExtractMigrateArchive(r io.Reader, destDir string) (*MigrateManifest, error) {
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		return nil, err
	}
	gr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("不是有效的搬家包（gzip）")
	}
	defer gr.Close()
	tr := tar.NewReader(gr)

	var written int64
	got := map[string]string{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("搬家包损坏: %w", err)
		}
		name := sanitizeTarName(h.Name)
		if name == "" {
			continue
		}
		target := filepath.Join(destDir, filepath.FromSlash(name))
		if !strings.HasPrefix(target, destDir+string(os.PathSeparator)) && target != destDir {
			return nil, fmt.Errorf("搬家包路径非法: %s", h.Name)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return nil, err
			}
		case tar.TypeReg:
			if h.Size < 0 {
				return nil, fmt.Errorf("搬家包文件大小非法")
			}
			if written+h.Size > MaxExtractBytes {
				return nil, fmt.Errorf("搬家包过大")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return nil, err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err != nil {
				return nil, err
			}
			n, copyErr := io.Copy(f, io.LimitReader(tr, h.Size+1))
			_ = f.Close()
			if copyErr != nil {
				return nil, copyErr
			}
			if n != h.Size {
				return nil, fmt.Errorf("搬家包文件 %s 长度不符", name)
			}
			written += n
			sum, err := hashFile(target)
			if err != nil {
				return nil, err
			}
			got[name] = sum
		default:
			// skip other types
		}
	}

	manPath := filepath.Join(destDir, migrateManifestName)
	raw, err := os.ReadFile(manPath)
	if err != nil {
		return nil, fmt.Errorf("搬家包缺少 manifest.json")
	}
	var man MigrateManifest
	if err := json.Unmarshal(raw, &man); err != nil {
		return nil, fmt.Errorf("manifest.json 无法解析")
	}
	if man.Format != MigrateFormatV1 {
		return nil, fmt.Errorf("不支持的搬家包格式 %q", man.Format)
	}
	dbPath := filepath.Join(destDir, migrateDBName)
	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("搬家包缺少 panel.db")
	}
	for name, want := range man.Files {
		if name == migrateManifestName {
			continue
		}
		have, ok := got[name]
		if !ok || !strings.EqualFold(have, want) {
			return nil, fmt.Errorf("搬家包校验失败: %s", name)
		}
	}

	if err := rejectNewerDump(dbPath); err != nil {
		return nil, err
	}

	opened, err := Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("搬家包数据库无法打开: %w", err)
	}
	users, uerr := CountUsers(opened)
	_, _ = opened.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	_ = opened.Close()
	if uerr != nil {
		return nil, uerr
	}
	if users < 1 {
		return nil, fmt.Errorf("搬家包里没有用户，拒绝导入")
	}
	man.Users = users
	return &man, nil
}

func sanitizeTarName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\\", "/")
	name = strings.TrimPrefix(name, "./")
	name = strings.TrimPrefix(name, "/")
	if name == "" || strings.Contains(name, "..") {
		return ""
	}
	return name
}

func migrationSeq(name string) int {
	name = strings.TrimSpace(name)
	i := strings.IndexByte(name, '_')
	if i <= 0 {
		return 0
	}
	n, err := strconv.Atoi(name[:i])
	if err != nil {
		return 0
	}
	return n
}

func rejectNewerDump(dbPath string) error {
	bundled, err := BundledMigrationNames()
	if err != nil {
		return err
	}
	maxKnown := 0
	for _, n := range bundled {
		if s := migrationSeq(n); s > maxKnown {
			maxKnown = s
		}
	}
	d, err := sql.Open("sqlite", dbPath+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return fmt.Errorf("打开搬家包数据库: %w", err)
	}
	defer d.Close()
	rows, err := d.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("搬家包不是面板数据库")
	}
	defer rows.Close()
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return err
		}
		if s := migrationSeq(v); s > maxKnown {
			return fmt.Errorf("搬家包来自更新的面板（含迁移 %s），请先把本机升到对应版本再导入", v)
		}
	}
	return rows.Err()
}

// DataDirFromDBPath is /var/lib/nft when db is /var/lib/nft/panel.db.
func DataDirFromDBPath(dbPath string) string {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" || dbPath == ":memory:" {
		return ""
	}
	return filepath.Dir(dbPath)
}

func IncomingDir(dataDir string) string {
	return filepath.Join(dataDir, MigrateIncomingDir)
}

func ReadyMarker(dataDir string) string {
	return filepath.Join(IncomingDir(dataDir), MigrateReadyName)
}

// StageIncoming copies a validated extract dir into <dataDir>/migrate-incoming
// and writes the ready marker. The next panel start applies it.
func StageIncoming(dataDir, extractDir string) error {
	if strings.TrimSpace(dataDir) == "" {
		return fmt.Errorf("数据目录未知")
	}
	inc := IncomingDir(dataDir)
	_ = os.RemoveAll(inc)
	if err := os.MkdirAll(inc, 0o700); err != nil {
		return err
	}
	if err := copyFile(filepath.Join(extractDir, migrateDBName), filepath.Join(inc, migrateDBName)); err != nil {
		return err
	}
	_ = copyDirIfExists(filepath.Join(extractDir, migrateBrandDir), filepath.Join(inc, migrateBrandDir))
	_ = copyDirIfExists(filepath.Join(extractDir, migrateDocsDir), filepath.Join(inc, migrateDocsDir))
	return os.WriteFile(filepath.Join(inc, MigrateReadyName), []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600)
}

// ApplyStagedMigrate swaps a staged incoming snapshot over the live data dir.
// Call before opening the live DB (process start). No-op if no ready marker.
func ApplyStagedMigrate(dbPath string) error {
	dataDir := DataDirFromDBPath(dbPath)
	if dataDir == "" {
		return nil
	}
	inc := IncomingDir(dataDir)
	ready := filepath.Join(inc, MigrateReadyName)
	if _, err := os.Stat(ready); err != nil {
		return nil
	}
	srcDB := filepath.Join(inc, migrateDBName)
	if _, err := os.Stat(srcDB); err != nil {
		return fmt.Errorf("待导入快照缺少 panel.db")
	}

	bakDir := filepath.Join(dataDir, "backups")
	_ = ensureDir(bakDir)
	stamp := time.Now().Format("20060102-150405")
	if _, err := os.Stat(dbPath); err == nil {
		bak := filepath.Join(bakDir, "panel-pre-migrate-"+stamp+".db")
		if err := snapshotLiveDB(dbPath, bak); err != nil {
			return fmt.Errorf("备份当前数据库失败: %w", err)
		}
	}

	_ = os.Remove(dbPath + "-wal")
	_ = os.Remove(dbPath + "-shm")
	if err := replaceFile(srcDB, dbPath); err != nil {
		return fmt.Errorf("替换数据库失败: %w", err)
	}
	_ = os.Remove(dbPath + "-wal")
	_ = os.Remove(dbPath + "-shm")

	if err := replaceDirIfPresent(filepath.Join(inc, migrateBrandDir), filepath.Join(dataDir, migrateBrandDir)); err != nil {
		return fmt.Errorf("替换 Logo 目录失败: %w", err)
	}
	if err := replaceDirIfPresent(filepath.Join(inc, migrateDocsDir), filepath.Join(dataDir, migrateDocsDir)); err != nil {
		return fmt.Errorf("替换文档资源失败: %w", err)
	}
	return os.RemoveAll(inc)
}

func snapshotLiveDB(src, dest string) error {
	d, err := sql.Open("sqlite", src+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return copyFile(src, dest)
	}
	_, _ = d.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	berr := Backup(d, dest)
	_ = d.Close()
	if berr != nil {
		return copyFile(src, dest)
	}
	return nil
}

func replaceFile(src, dest string) error {
	if err := os.Rename(src, dest); err == nil {
		return nil
	}
	if err := copyFile(src, dest); err != nil {
		return err
	}
	return os.Remove(src)
}

func replaceDirIfPresent(src, dest string) error {
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	bak := dest + ".pre-migrate"
	_ = os.RemoveAll(bak)
	if _, err := os.Stat(dest); err == nil {
		if err := os.Rename(dest, bak); err != nil {
			if err := copyDir(dest, bak); err != nil {
				return err
			}
			if err := os.RemoveAll(dest); err != nil {
				return err
			}
		}
	}
	if err := os.Rename(src, dest); err != nil {
		if err := copyDir(src, dest); err != nil {
			if _, berr := os.Stat(bak); berr == nil {
				_ = os.Rename(bak, dest)
			}
			return err
		}
		_ = os.RemoveAll(src)
	}
	_ = os.RemoveAll(bak)
	return nil
}

func copyDirIfExists(src, dest string) error {
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return copyDir(src, dest)
}

func copyDir(src, dest string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		out := filepath.Join(dest, rel)
		if info.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		return copyFile(path, out)
	})
}

func copyFile(src, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dest + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
