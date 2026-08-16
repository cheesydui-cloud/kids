package db

import (
	"path/filepath"
	"testing"
)

func TestReorderNodes(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	var ids []int64
	for _, name := range []string{"a", "b", "c"} {
		n, err := CreateNode(d, name, "https://p", "")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, n.ID)
	}

	// New nodes default to creation order (sort_order = max+1).
	nodes, _ := ListNodes(d)
	if got := names(nodes); got != "a,b,c" {
		t.Fatalf("initial order = %s, want a,b,c", got)
	}

	// Reorder to c, a, b.
	if err := ReorderNodes(d, []int64{ids[2], ids[0], ids[1]}); err != nil {
		t.Fatal(err)
	}
	nodes, _ = ListNodes(d)
	if got := names(nodes); got != "c,a,b" {
		t.Fatalf("reordered = %s, want c,a,b", got)
	}
}

func TestReorderUsers(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	var ids []int64
	for _, name := range []string{"u-a", "u-b", "u-c"} {
		id, err := CreateUser(d, name, "hash", "user")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}

	users, err := ListUsers(d)
	if err != nil {
		t.Fatal(err)
	}
	if got := relativeIDs(users, ids); got != "u-a,u-b,u-c" {
		t.Fatalf("initial relative order = %s, want u-a,u-b,u-c", got)
	}

	if err := ReorderUsers(d, []int64{ids[2], ids[0], ids[1]}); err != nil {
		t.Fatal(err)
	}
	users, _ = ListUsers(d)
	if got := relativeIDs(users, ids); got != "u-c,u-a,u-b" {
		t.Fatalf("reordered relative = %s, want u-c,u-a,u-b", got)
	}
}

func TestReorderNodeRepo(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	var ids []int64
	for _, name := range []string{"r-a", "r-b", "r-c"} {
		n, err := CreateNodeRepoEntry(d, name, "ss", "1.2.3.4", 443, "", "", 0, "", NodeRepoCFFields{})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, n.ID)
	}

	list, err := ListNodeRepo(d)
	if err != nil {
		t.Fatal(err)
	}
	if got := repoNames(list); got != "r-a,r-b,r-c" {
		t.Fatalf("initial order = %s, want r-a,r-b,r-c", got)
	}

	if err := ReorderNodeRepo(d, []int64{ids[2], ids[0], ids[1]}); err != nil {
		t.Fatal(err)
	}
	list, _ = ListNodeRepo(d)
	if got := repoNames(list); got != "r-c,r-a,r-b" {
		t.Fatalf("reordered = %s, want r-c,r-a,r-b", got)
	}
}

func names(nodes []*Node) string {
	out := ""
	for i, n := range nodes {
		if i > 0 {
			out += ","
		}
		out += n.Name
	}
	return out
}

func repoNames(list []NodeRepoEntry) string {
	out := ""
	for i, n := range list {
		if i > 0 {
			out += ","
		}
		out += n.Name
	}
	return out
}

func relativeIDs(users []*User, ids []int64) string {
	want := map[int64]string{}
	for i, id := range ids {
		want[id] = []string{"u-a", "u-b", "u-c"}[i]
	}
	out := ""
	for _, u := range users {
		if s, ok := want[u.ID]; ok {
			if out != "" {
				out += ","
			}
			out += s
		}
	}
	return out
}
