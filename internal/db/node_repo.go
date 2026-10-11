package db

import (
	"database/sql"
	"net"
	"strings"
)

// NodeRepoEntry is one node in the admin-maintained node repository.
type NodeRepoEntry struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Protocol  string `json:"protocol"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	URI       string `json:"uri"`
	Remark    string `json:"remark"`
	ExpiresAt int64  `json:"expires_at"`
	CreatedAt int64  `json:"created_at"`
	// GroupID is the folder this entry sits in (0 = ungrouped). GroupName is a
	// denormalized label kept in sync for display and legacy clients.
	GroupID   int64  `json:"group_id"`
	GroupName string `json:"group_name"`
	// SortOrder is the admin list display order (drag-and-drop).
	SortOrder int64 `json:"sort_order"`
	// UserCount is how many users currently hold this endpoint via source=repo
	// present landing exits. Filled by list API; not a DB column.
	UserCount int `json:"user_count"`

	// Optional Cloudflare DNS sync: host is the stable rule target (domain);
	// BackendIP is the A-record content pushed when CFSync is on.
	BackendIP    string `json:"backend_ip"`
	CFSync       bool   `json:"cf_sync"`
	CFZoneID     string `json:"cf_zone_id"`
	CFRecordName string `json:"cf_record_name"`
	CFLastSyncAt int64  `json:"cf_last_sync_at"`
	CFLastError  string `json:"cf_last_error"`
	CFLastIP     string `json:"cf_last_ip"`

	// MachineID groups protocols that belong to one landing machine. It stays
	// put when the current IP changes, so two machines that temporarily share
	// an address are not merged.
	MachineID int64 `json:"machine_id"`
	// Last DNS probe, kept so the list still shows the result after refresh.
	DNSProbeStatus  string `json:"dns_probe_status"`
	DNSProbeMessage string `json:"dns_probe_message"`
	DNSProbeAt      int64  `json:"dns_probe_at"`
	DNSProbeIP      string `json:"dns_probe_ip"`
}

// NodeRepoCFFields are the optional DNS-sync columns written on create/update.
type NodeRepoCFFields struct {
	BackendIP    string
	CFSync       bool
	CFZoneID     string
	CFRecordName string
}

const nodeRepoCols = `id, name, protocol, host, port, uri, remark, expires_at, created_at, group_name, group_id,
		backend_ip, cf_sync, cf_zone_id, cf_record_name, cf_last_sync_at, cf_last_error, cf_last_ip, sort_order,
		machine_id, dns_probe_status, dns_probe_message, dns_probe_at, dns_probe_ip`

func scanNodeRepo(rows *sql.Rows) (NodeRepoEntry, error) {
	var n NodeRepoEntry
	var cfSync int
	err := rows.Scan(
		&n.ID, &n.Name, &n.Protocol, &n.Host, &n.Port, &n.URI, &n.Remark, &n.ExpiresAt, &n.CreatedAt, &n.GroupName, &n.GroupID,
		&n.BackendIP, &cfSync, &n.CFZoneID, &n.CFRecordName, &n.CFLastSyncAt, &n.CFLastError, &n.CFLastIP, &n.SortOrder,
		&n.MachineID, &n.DNSProbeStatus, &n.DNSProbeMessage, &n.DNSProbeAt, &n.DNSProbeIP,
	)
	n.CFSync = cfSync != 0
	return n, err
}

func scanNodeRepoRow(row *sql.Row) (NodeRepoEntry, error) {
	var n NodeRepoEntry
	var cfSync int
	err := row.Scan(
		&n.ID, &n.Name, &n.Protocol, &n.Host, &n.Port, &n.URI, &n.Remark, &n.ExpiresAt, &n.CreatedAt, &n.GroupName, &n.GroupID,
		&n.BackendIP, &cfSync, &n.CFZoneID, &n.CFRecordName, &n.CFLastSyncAt, &n.CFLastError, &n.CFLastIP, &n.SortOrder,
		&n.MachineID, &n.DNSProbeStatus, &n.DNSProbeMessage, &n.DNSProbeAt, &n.DNSProbeIP,
	)
	n.CFSync = cfSync != 0
	return n, err
}

// ListNodeRepo returns all nodes in the repository.
func ListNodeRepo(d *sql.DB) ([]NodeRepoEntry, error) {
	rows, err := d.Query(`SELECT ` + nodeRepoCols + ` FROM node_repo ORDER BY sort_order, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NodeRepoEntry
	for rows.Next() {
		n, err := scanNodeRepo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

// CreateNodeRepoEntry inserts a new node into the repository.
// groupName, when non-empty, creates/ensures a folder and assigns group_id.
func CreateNodeRepoEntry(d *sql.DB, name, protocol, host string, port int, uri, remark string, expiresAt int64, groupName string, cf NodeRepoCFFields) (NodeRepoEntry, error) {
	var groupID int64
	groupName = strings.TrimSpace(groupName)
	if groupName != "" {
		f, err := EnsureNodeRepoFolder(d, groupName)
		if err != nil {
			return NodeRepoEntry{}, err
		}
		groupID = f.ID
	}
	cfSync := 0
	if cf.CFSync {
		cfSync = 1
	}
	machineID, backendIP, err := resolveRepoMachine(d, host, cf.BackendIP)
	if err != nil {
		return NodeRepoEntry{}, err
	}
	n := NodeRepoEntry{
		Name: name, Protocol: protocol, Host: host, Port: port, URI: uri, Remark: remark,
		ExpiresAt: expiresAt, CreatedAt: now(), GroupName: groupName, GroupID: groupID,
		BackendIP: backendIP, CFSync: cf.CFSync,
		CFZoneID: strings.TrimSpace(cf.CFZoneID), CFRecordName: strings.TrimSpace(cf.CFRecordName),
		MachineID: machineID,
	}
	res, err := d.Exec(`INSERT INTO node_repo (
			name, protocol, host, port, uri, remark, expires_at, created_at, group_name, group_id,
			backend_ip, cf_sync, cf_zone_id, cf_record_name, sort_order, machine_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, (SELECT COALESCE(MAX(sort_order),0)+1 FROM node_repo), ?)`,
		n.Name, n.Protocol, n.Host, n.Port, n.URI, n.Remark, n.ExpiresAt, n.CreatedAt, n.GroupName, n.GroupID,
		n.BackendIP, cfSync, n.CFZoneID, n.CFRecordName, n.MachineID)
	if err != nil {
		return n, err
	}
	n.ID, _ = res.LastInsertId()
	_ = d.QueryRow(`SELECT sort_order FROM node_repo WHERE id=?`, n.ID).Scan(&n.SortOrder)
	return n, nil
}

// ReorderNodeRepo assigns sort_order to match the given id sequence (1-based).
// IDs absent from the list keep their previous order value.
func ReorderNodeRepo(d *sql.DB, ids []int64) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, id := range ids {
		if _, err := tx.Exec(`UPDATE node_repo SET sort_order=? WHERE id=?`, i+1, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetNodeRepoEntry returns one repository node by id.
func GetNodeRepoEntry(d *sql.DB, id int64) (NodeRepoEntry, error) {
	row := d.QueryRow(`SELECT `+nodeRepoCols+` FROM node_repo WHERE id=?`, id)
	return scanNodeRepoRow(row)
}

// UpdateNodeRepoEntry updates an existing node in the repository (including CF fields).
func UpdateNodeRepoEntry(d *sql.DB, id int64, name, protocol, host string, port int, uri, remark string, expiresAt int64, groupName string, cf NodeRepoCFFields) error {
	var groupID int64
	groupName = strings.TrimSpace(groupName)
	if groupName != "" {
		f, err := EnsureNodeRepoFolder(d, groupName)
		if err != nil {
			return err
		}
		groupID = f.ID
	}
	cfSync := 0
	if cf.CFSync {
		cfSync = 1
	}
	_, err := d.Exec(`UPDATE node_repo SET
		name=?, protocol=?, host=?, port=?, uri=?, remark=?, expires_at=?, group_name=?, group_id=?,
		backend_ip=?, cf_sync=?, cf_zone_id=?, cf_record_name=?
		WHERE id=?`,
		name, protocol, host, port, uri, remark, expiresAt, groupName, groupID,
		strings.TrimSpace(cf.BackendIP), cfSync, strings.TrimSpace(cf.CFZoneID), strings.TrimSpace(cf.CFRecordName),
		id)
	return err
}

// SetNodeRepoCFSyncResult records the outcome of a Cloudflare DNS push.
func SetNodeRepoCFSyncResult(d *sql.DB, id int64, ok bool, ip, errMsg string) error {
	if ok {
		_, err := d.Exec(`UPDATE node_repo SET cf_last_sync_at=?, cf_last_error='', cf_last_ip=? WHERE id=?`,
			now(), strings.TrimSpace(ip), id)
		return err
	}
	_, err := d.Exec(`UPDATE node_repo SET cf_last_error=? WHERE id=?`, strings.TrimSpace(errMsg), id)
	return err
}

// SetNodeRepoGroup assigns a free-form group label to a repo entry (empty = ungrouped).
func SetNodeRepoGroup(d *sql.DB, id int64, groupName string) error {
	groupName = strings.TrimSpace(groupName)
	if groupName == "" {
		return SetNodeRepoFolder(d, id, 0)
	}
	f, err := EnsureNodeRepoFolder(d, groupName)
	if err != nil {
		return err
	}
	return SetNodeRepoFolder(d, id, f.ID)
}

// SetNodeRepoGroupsBatch assigns the same group label to many repo entries.
func SetNodeRepoGroupsBatch(d *sql.DB, ids []int64, groupName string) error {
	groupName = strings.TrimSpace(groupName)
	if groupName == "" {
		return SetNodeRepoFoldersBatch(d, ids, 0)
	}
	f, err := EnsureNodeRepoFolder(d, groupName)
	if err != nil {
		return err
	}
	return SetNodeRepoFoldersBatch(d, ids, f.ID)
}

// DeleteNodeRepoEntry removes a node from the repository by ID.
func DeleteNodeRepoEntry(d *sql.DB, id int64) error {
	_, err := d.Exec(`DELETE FROM node_repo WHERE id = ?`, id)
	return err
}

// DeleteNodeRepoEntries removes several repository entries in a single
// statement and returns how many rows were deleted.
func DeleteNodeRepoEntries(d *sql.DB, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	ph := strings.Repeat("?,", len(ids)-1) + "?"
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	res, err := d.Exec(`DELETE FROM node_repo WHERE id IN (`+ph+`)`, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// SetNodeRepoBackendIP updates only the current IPv4. Other fields stay put.
func SetNodeRepoBackendIP(d *sql.DB, id int64, ip string) error {
	_, err := d.Exec(`UPDATE node_repo SET backend_ip=? WHERE id=?`, strings.TrimSpace(ip), id)
	return err
}

// ListNodeRepoByBackendIP returns entries that share a current IPv4.
func ListNodeRepoByBackendIP(d *sql.DB, ip string) ([]NodeRepoEntry, error) {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return nil, nil
	}
	rows, err := d.Query(`SELECT `+nodeRepoCols+` FROM node_repo WHERE backend_ip=? ORDER BY sort_order, id`, ip)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NodeRepoEntry
	for rows.Next() {
		n, err := scanNodeRepo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ListNodeRepoByIDs returns specific repo entries by their IDs.
func ListNodeRepoByIDs(d *sql.DB, ids []int64) ([]NodeRepoEntry, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	args := make([]any, len(ids))
	placeholders := ""
	for i, id := range ids {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args[i] = id
	}
	rows, err := d.Query(`SELECT `+nodeRepoCols+` FROM node_repo WHERE id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NodeRepoEntry
	for rows.Next() {
		n, err := scanNodeRepo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

func repoIPv4(s string) string {
	s = strings.TrimSpace(s)
	ip := net.ParseIP(s)
	if ip == nil || ip.To4() == nil {
		return ""
	}
	return ip.To4().String()
}

// resolveRepoMachine picks a stable machine id. An explicit current IPv4 joins
// the machine that already uses it. A bare IPv4 target is stored as the current
// IP so two protocols imported as raw addresses group at once. A new address
// gets a new id and is not merged later just because the IP happens to match.
func resolveRepoMachine(d DBTX, host, backendIP string) (machineID int64, ip string, err error) {
	ip = repoIPv4(backendIP)
	if ip == "" {
		ip = repoIPv4(host)
	}
	if ip != "" {
		err = d.QueryRow(`SELECT machine_id FROM node_repo WHERE backend_ip=? AND machine_id>0 ORDER BY machine_id LIMIT 1`, ip).Scan(&machineID)
		if err == nil && machineID > 0 {
			return machineID, ip, nil
		}
		if err != nil && err != sql.ErrNoRows {
			return 0, ip, err
		}
	}
	if err = d.QueryRow(`SELECT COALESCE(MAX(machine_id),0)+1 FROM node_repo`).Scan(&machineID); err != nil {
		return 0, ip, err
	}
	if machineID < 1 {
		machineID = 1
	}
	return machineID, ip, nil
}

// ListNodeRepoByMachine returns every protocol on one machine.
func ListNodeRepoByMachine(d *sql.DB, machineID int64) ([]NodeRepoEntry, error) {
	if machineID <= 0 {
		return nil, nil
	}
	rows, err := d.Query(`SELECT `+nodeRepoCols+` FROM node_repo WHERE machine_id=? ORDER BY sort_order, id`, machineID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NodeRepoEntry
	for rows.Next() {
		n, err := scanNodeRepo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// GetNodeRepoByEndpoint returns the first warehouse row at host:port.
func GetNodeRepoByEndpoint(d DBTX, host string, port int) (NodeRepoEntry, error) {
	row := d.QueryRow(`SELECT `+nodeRepoCols+` FROM node_repo WHERE host=? AND port=? ORDER BY id LIMIT 1`, strings.TrimSpace(host), port)
	return scanNodeRepoRow(row)
}

// SetNodeRepoProbe stores the last DNS check on the row.
func SetNodeRepoProbe(d *sql.DB, id int64, status, message, ip string) error {
	_, err := d.Exec(`UPDATE node_repo SET dns_probe_status=?, dns_probe_message=?, dns_probe_at=?, dns_probe_ip=? WHERE id=?`,
		strings.TrimSpace(status), strings.TrimSpace(message), now(), strings.TrimSpace(ip), id)
	return err
}

// SetNodeRepoExpires writes the warehouse template expiry only. Assigned users
// keep their own clock until an explicit sync.
func SetNodeRepoExpires(d *sql.DB, id int64, expiresAt int64) error {
	_, err := d.Exec(`UPDATE node_repo SET expires_at=? WHERE id=?`, expiresAt, id)
	return err
}

// NodeRepoImportInput is one line of a warehouse batch import.
type NodeRepoImportInput struct {
	Name      string
	Protocol  string
	Host      string
	Port      int
	URI       string
	GroupID   int64
	GroupName string
}

// NodeRepoImportResult counts warehouse rows created or updated. Import never
// rewrites exits already assigned to users.
type NodeRepoImportResult struct {
	Created int     `json:"created"`
	Updated int     `json:"updated"`
	IDs     []int64 `json:"ids"`
}

// ImportNodeRepoBatch inserts new endpoints and updates an existing host:port
// in place. Same current IPv4 in the batch shares one machine id. One failure
// rolls the whole batch back.
func ImportNodeRepoBatch(d *sql.DB, items []NodeRepoImportInput) (NodeRepoImportResult, error) {
	var out NodeRepoImportResult
	if len(items) == 0 {
		return out, nil
	}
	tx, err := d.Begin()
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	for _, it := range items {
		host := strings.TrimSpace(it.Host)
		name := strings.TrimSpace(it.Name)
		if name == "" {
			name = host
		}
		existing, err := GetNodeRepoByEndpoint(tx, host, it.Port)
		if err != nil && err != sql.ErrNoRows {
			return out, err
		}
		if err == nil && existing.ID > 0 {
			backend := existing.BackendIP
			if strings.TrimSpace(backend) == "" {
				if ip := repoIPv4(host); ip != "" {
					backend = ip
				}
			}
			groupID, groupName := existing.GroupID, existing.GroupName
			if it.GroupID > 0 {
				groupID, groupName = it.GroupID, strings.TrimSpace(it.GroupName)
			}
			if _, err := tx.Exec(`UPDATE node_repo SET name=?, protocol=?, uri=?, backend_ip=?, group_id=?, group_name=? WHERE id=?`,
				name, strings.TrimSpace(it.Protocol), strings.TrimSpace(it.URI), backend, groupID, groupName, existing.ID); err != nil {
				return out, err
			}
			out.Updated++
			out.IDs = append(out.IDs, existing.ID)
			continue
		}
		cf := NodeRepoCFFields{BackendIP: repoIPv4(host)}
		n, err := createNodeRepoEntryTx(tx, name, strings.TrimSpace(it.Protocol), host, it.Port, strings.TrimSpace(it.URI), "", 0, it.GroupID, strings.TrimSpace(it.GroupName), cf)
		if err != nil {
			return out, err
		}
		out.Created++
		out.IDs = append(out.IDs, n.ID)
	}
	if err := tx.Commit(); err != nil {
		return out, err
	}
	return out, nil
}

func createNodeRepoEntryTx(tx DBTX, name, protocol, host string, port int, uri, remark string, expiresAt, groupID int64, groupName string, cf NodeRepoCFFields) (NodeRepoEntry, error) {
	cfSync := 0
	if cf.CFSync {
		cfSync = 1
	}
	machineID, backendIP, err := resolveRepoMachine(tx, host, cf.BackendIP)
	if err != nil {
		return NodeRepoEntry{}, err
	}
	n := NodeRepoEntry{
		Name: name, Protocol: protocol, Host: host, Port: port, URI: uri, Remark: remark,
		ExpiresAt: expiresAt, CreatedAt: now(), GroupName: groupName, GroupID: groupID,
		BackendIP: backendIP, MachineID: machineID,
		CFZoneID: strings.TrimSpace(cf.CFZoneID), CFRecordName: strings.TrimSpace(cf.CFRecordName),
		CFSync: cf.CFSync,
	}
	res, err := tx.Exec(`INSERT INTO node_repo (
			name, protocol, host, port, uri, remark, expires_at, created_at, group_name, group_id,
			backend_ip, cf_sync, cf_zone_id, cf_record_name, sort_order, machine_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, (SELECT COALESCE(MAX(sort_order),0)+1 FROM node_repo), ?)`,
		n.Name, n.Protocol, n.Host, n.Port, n.URI, n.Remark, n.ExpiresAt, n.CreatedAt, n.GroupName, n.GroupID,
		n.BackendIP, cfSync, n.CFZoneID, n.CFRecordName, n.MachineID)
	if err != nil {
		return n, err
	}
	n.ID, _ = res.LastInsertId()
	return n, nil
}

// BackfillNodeRepoMachines gives every legacy row a machine id. Rows that
// already share a current IPv4 stay one machine. A bare IPv4 target is copied
// into the current IP so those protocols group too. Rows that already have an
// id are left alone.
func BackfillNodeRepoMachines(d *sql.DB) error {
	var pending int
	if err := d.QueryRow(`SELECT COUNT(*) FROM node_repo WHERE machine_id=0`).Scan(&pending); err != nil {
		return err
	}
	if pending == 0 {
		return nil
	}
	rows, err := d.Query(`SELECT id, host, backend_ip FROM node_repo WHERE machine_id=0 ORDER BY id`)
	if err != nil {
		return err
	}
	type legacy struct {
		id   int64
		host string
		ip   string
	}
	var list []legacy
	for rows.Next() {
		var row legacy
		if err := rows.Scan(&row.id, &row.host, &row.ip); err != nil {
			rows.Close()
			return err
		}
		list = append(list, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, row := range list {
		ip := repoIPv4(row.ip)
		filled := ip
		if filled == "" {
			filled = repoIPv4(row.host)
		}
		if filled != "" && strings.TrimSpace(row.ip) == "" {
			if _, err := d.Exec(`UPDATE node_repo SET backend_ip=? WHERE id=? AND backend_ip=''`, filled, row.id); err != nil {
				return err
			}
		}
		var mid int64
		if filled != "" {
			err := d.QueryRow(`SELECT machine_id FROM node_repo WHERE backend_ip=? AND machine_id>0 ORDER BY machine_id LIMIT 1`, filled).Scan(&mid)
			if err != nil && err != sql.ErrNoRows {
				return err
			}
		}
		if mid == 0 {
			if err := d.QueryRow(`SELECT COALESCE(MAX(machine_id),0)+1 FROM node_repo`).Scan(&mid); err != nil {
				return err
			}
		}
		if _, err := d.Exec(`UPDATE node_repo SET machine_id=? WHERE id=? AND machine_id=0`, mid, row.id); err != nil {
			return err
		}
	}
	return nil
}
