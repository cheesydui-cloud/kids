package db

import (
	"path/filepath"
	"testing"
)

func TestCreateNodeRepoJoinsMachineByIP(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	ss, err := CreateNodeRepoEntry(d, "ss", "ss", "1.2.3.4", 1000, "ss://a@1.2.3.4:1000", "", 0, "", NodeRepoCFFields{})
	if err != nil {
		t.Fatal(err)
	}
	vl, err := CreateNodeRepoEntry(d, "vl", "vless", "1.2.3.4", 2000, "vless://b@1.2.3.4:2000", "", 0, "", NodeRepoCFFields{})
	if err != nil {
		t.Fatal(err)
	}
	other, err := CreateNodeRepoEntry(d, "other", "ss", "9.9.9.9", 1000, "", "", 0, "", NodeRepoCFFields{})
	if err != nil {
		t.Fatal(err)
	}
	if ss.MachineID == 0 || ss.MachineID != vl.MachineID {
		t.Fatalf("same IP should share a machine, ss=%d vl=%d", ss.MachineID, vl.MachineID)
	}
	if ss.BackendIP != "1.2.3.4" || vl.BackendIP != "1.2.3.4" {
		t.Fatalf("bare IP should be stored as current IP, ss=%s vl=%s", ss.BackendIP, vl.BackendIP)
	}
	if other.MachineID == ss.MachineID {
		t.Fatalf("different IP joined the same machine %d", other.MachineID)
	}
	sibs, err := ListNodeRepoByMachine(d, ss.MachineID)
	if err != nil || len(sibs) != 2 {
		t.Fatalf("machine members=%d err=%v", len(sibs), err)
	}
}

func TestImportNodeRepoBatchDedupesWithoutTouchingUsers(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	uid := createTestUser(t, d)
	first, err := CreateNodeRepoEntry(d, "old", "ss", "1.2.3.4", 443, "ss://old@1.2.3.4:443", "", 0, "", NodeRepoCFFields{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AppendUserLandingExits(d, uid, []LandingExitInput{{
		Host: "1.2.3.4", Port: 443, Name: "user-name", Protocol: "ss", URI: "ss://old@1.2.3.4:443",
	}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := SetUserLandingExitExpires(d, uid, "1.2.3.4", 443, 1_700_000_000); err != nil {
		t.Fatal(err)
	}

	res, err := ImportNodeRepoBatch(d, []NodeRepoImportInput{
		{Name: "new-ss", Protocol: "ss", Host: "1.2.3.4", Port: 443, URI: "ss://new@1.2.3.4:443"},
		{Name: "vl", Protocol: "vless", Host: "1.2.3.4", Port: 8443, URI: "vless://x@1.2.3.4:8443"},
		{Name: "vl-again", Protocol: "vless", Host: "1.2.3.4", Port: 8443, URI: "vless://y@1.2.3.4:8443"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Created != 1 || res.Updated != 2 {
		t.Fatalf("import result %+v", res)
	}
	got, _ := GetNodeRepoEntry(d, first.ID)
	if got.Name != "new-ss" || got.URI != "ss://new@1.2.3.4:443" {
		t.Fatalf("warehouse row not updated: %+v", got)
	}
	vl, err := GetNodeRepoByEndpoint(d, "1.2.3.4", 8443)
	if err != nil {
		t.Fatal(err)
	}
	if vl.Name != "vl-again" || vl.MachineID != got.MachineID {
		t.Fatalf("second protocol machine=%d name=%s want machine %d", vl.MachineID, vl.Name, got.MachineID)
	}
	exits, _ := ListUserLandingExits(d, uid)
	if len(exits) != 1 || exits[0].Name != "user-name" || exits[0].ExpiresAt != 1_700_000_000 || exits[0].URI != "ss://old@1.2.3.4:443" {
		t.Fatalf("import rewrote the assigned exit: %+v", exits)
	}
}

func TestBackfillNodeRepoMachinesGroupsBareIP(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Exec(`INSERT INTO node_repo (name, protocol, host, port, uri, remark, created_at, group_name, group_id, backend_ip, machine_id)
		VALUES ('a','ss','8.8.8.8',1,'','',1,'',0,'',0), ('b','vless','8.8.8.8',2,'','',1,'',0,'',0), ('c','ss','c.example',3,'','',1,'',0,'',0)`); err != nil {
		t.Fatal(err)
	}
	if err := BackfillNodeRepoMachines(d); err != nil {
		t.Fatal(err)
	}
	a, err := GetNodeRepoByEndpoint(d, "8.8.8.8", 1)
	if err != nil {
		t.Fatal(err)
	}
	b, err := GetNodeRepoByEndpoint(d, "8.8.8.8", 2)
	if err != nil {
		t.Fatal(err)
	}
	c, err := GetNodeRepoByEndpoint(d, "c.example", 3)
	if err != nil {
		t.Fatal(err)
	}
	if a.MachineID == 0 || a.MachineID != b.MachineID || a.BackendIP != "8.8.8.8" {
		t.Fatalf("bare IP backfill a=%+v b=%+v", a, b)
	}
	if c.MachineID == 0 || c.MachineID == a.MachineID || c.BackendIP != "" {
		t.Fatalf("domain row should stay its own machine: %+v", c)
	}
}
