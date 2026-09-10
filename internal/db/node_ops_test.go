package db

import "testing"

func TestNodeOpsFieldsAndFolders(t *testing.T) {
	d := openTestDB(t)
	n, err := CreateNode(d, "ops-node", "https://panel", "tok")
	if err != nil {
		t.Fatal(err)
	}

	got, err := GetNode(d, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.GroupID != 0 || got.Remark != "" || got.ExpiresAt != 0 || got.MonthlyCostCents != 0 {
		t.Fatalf("new node should have empty ops fields: %+v", got)
	}

	f, err := CreateNodeFolder(d, "华东")
	if err != nil {
		t.Fatal(err)
	}
	if err := SetNodeFolder(d, n.ID, f.ID); err != nil {
		t.Fatal(err)
	}
	got, err = GetNode(d, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.GroupID != f.ID || got.GroupName != "华东" {
		t.Fatalf("folder not applied: id=%d name=%q", got.GroupID, got.GroupName)
	}

	if err := UpdateNodeOpsFields(d, n.ID, "  月付 5 刀  ", 1893456000, 3500); err != nil {
		t.Fatal(err)
	}
	got, err = GetNode(d, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Remark != "月付 5 刀" || got.ExpiresAt != 1893456000 || got.MonthlyCostCents != 3500 {
		t.Fatalf("ops fields not saved: %+v", got)
	}

	// Renaming the folder keeps the denormalized label on the node in sync.
	if err := RenameNodeFolder(d, f.ID, "华东-上海"); err != nil {
		t.Fatal(err)
	}
	got, _ = GetNode(d, n.ID)
	if got.GroupName != "华东-上海" {
		t.Fatalf("group name not synced after rename: %q", got.GroupName)
	}

	// Listing nodes carries the ops fields (admin list view).
	list, err := ListNodes(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].MonthlyCostCents != 3500 {
		t.Fatalf("list should expose ops fields: %+v", list)
	}

	if _, err := UpsertSelfNode(d); err != nil {
		t.Fatal(err)
	}
	folders, err := ListNodeFolders(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(folders) != 1 || folders[0].Count != 1 {
		t.Fatalf("folder count should ignore the hidden self node: %+v", folders)
	}
	ungrouped, err := UngroupedNodeCount(d)
	if err != nil {
		t.Fatal(err)
	}
	if ungrouped != 0 {
		t.Fatalf("ungrouped=%d, want 0 (self node must not count)", ungrouped)
	}

	// Deleting the folder moves the node back to ungrouped but keeps the node.
	if err := DeleteNodeFolder(d, f.ID); err != nil {
		t.Fatal(err)
	}
	got, err = GetNode(d, n.ID)
	if err != nil {
		t.Fatalf("node should survive folder delete: %v", err)
	}
	if got.GroupID != 0 || got.GroupName != "" {
		t.Fatalf("folder delete should ungroup the node: %+v", got)
	}
	if got.Remark == "" {
		t.Fatal("folder delete must not clear the remark")
	}

	ungrouped, err = UngroupedNodeCount(d)
	if err != nil {
		t.Fatal(err)
	}
	if ungrouped != 1 {
		t.Fatalf("ungrouped=%d after folder delete, want 1 (self still excluded)", ungrouped)
	}
}
