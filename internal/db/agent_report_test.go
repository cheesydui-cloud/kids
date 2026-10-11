package db

import "testing"

func TestAgentReportRoundTrip(t *testing.T) {
	d := openTestDB(t)
	n, err := UpsertSelfNode(d)
	if err != nil {
		t.Fatal(err)
	}
	if err := SetNodeDisconnect(d, n.ID, "watchdog_stale", 1700000000); err != nil {
		t.Fatal(err)
	}
	if err := SetNodeEgressNote(d, n.ID, "出口已变，中继地址未改（这次 2.2.2.2，中继仍是 9.9.9.9）"); err != nil {
		t.Fatal(err)
	}
	if err := SetNodeResourceWarning(d, n.ID, "连接跟踪已用 80%（80/100）"); err != nil {
		t.Fatal(err)
	}
	if err := MarkNodeUpgradeRolledBack(d, n.ID); err != nil {
		t.Fatal(err)
	}
	got, err := GetNode(d, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastDisconnectReason != "watchdog_stale" || got.LastDisconnectAt != 1700000000 {
		t.Fatalf("disconnect %+v", got)
	}
	if got.EgressNote == "" || got.ResourceWarning == "" {
		t.Fatalf("notes missing: egress=%q warn=%q", got.EgressNote, got.ResourceWarning)
	}
	if got.LastUpgradeStatus != "rolled_back" || got.LastUpgradeError == "" {
		t.Fatalf("rollback status %+v", got.LastUpgradeStatus)
	}

	uid, err := CreateUser(d, "agent-report", "x", "user")
	if err != nil {
		t.Fatal(err)
	}
	if err := GrantNode(d, uid, n.ID, 1, 0); err != nil {
		t.Fatal(err)
	}
	nodes, _, err := ListNodesForUser(d, uid)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].LastDisconnectReason != "watchdog_stale" || nodes[0].ResourceWarning == "" {
		t.Fatalf("grant scan missed agent report: %+v", nodes)
	}
}
