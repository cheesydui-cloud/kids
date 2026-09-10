package db

import "testing"

func TestListAuditLogsFiltersAndPaginates(t *testing.T) {
	d := openTestDB(t)
	uid := createTestUser(t, d)
	for i := 0; i < 5; i++ {
		if err := WriteAudit(d, uid, "user.set_quota_bytes", "7", "10GB"); err != nil {
			t.Fatal(err)
		}
	}
	if err := WriteAudit(d, uid, "node.create", "node-a", "edge-1"); err != nil {
		t.Fatal(err)
	}

	page1, total, err := ListAuditLogs(d, AuditFilter{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if total != 6 {
		t.Fatalf("total = %d, want 6", total)
	}
	if len(page1) != 2 {
		t.Fatalf("page size = %d, want 2", len(page1))
	}
	if page1[0].ID <= page1[1].ID {
		t.Fatalf("rows not newest-first: %d then %d", page1[0].ID, page1[1].ID)
	}
	if page1[0].Username == "" {
		t.Fatal("actor username should be joined in")
	}

	page2, _, err := ListAuditLogs(d, AuditFilter{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page2) != 2 || page2[0].ID == page1[0].ID {
		t.Fatalf("offset page did not advance: %+v vs %+v", page2, page1)
	}

	nodeOnly, nodeTotal, err := ListAuditLogs(d, AuditFilter{Query: "node.create", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if nodeTotal != 1 || len(nodeOnly) != 1 || nodeOnly[0].Action != "node.create" {
		t.Fatalf("query filter mismatch: total=%d rows=%+v", nodeTotal, nodeOnly)
	}
}

func TestAnnouncementReadReceipts(t *testing.T) {
	d := openTestDB(t)
	uid := createTestUser(t, d)
	a, err := CreateAnnouncement(d, "标题", "内容", uid, 0, "", 0, "blue", 0)
	if err != nil {
		t.Fatal(err)
	}

	if ids, err := ReadAnnouncementIDs(d, uid); err != nil || len(ids) != 0 {
		t.Fatalf("new user read ids = %v (err %v), want empty", ids, err)
	}
	// Marking twice must stay idempotent (the UI may retry optimistically).
	if err := MarkAnnouncementRead(d, uid, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := MarkAnnouncementRead(d, uid, a.ID); err != nil {
		t.Fatal(err)
	}
	ids, err := ReadAnnouncementIDs(d, uid)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != a.ID {
		t.Fatalf("read ids = %v, want [%d]", ids, a.ID)
	}

	// Deleting the notice must not leave orphan receipts behind.
	if err := DeleteAnnouncement(d, a.ID); err != nil {
		t.Fatal(err)
	}
	if ids, err := ReadAnnouncementIDs(d, uid); err != nil || len(ids) != 0 {
		t.Fatalf("read ids after delete = %v (err %v), want empty", ids, err)
	}
}
