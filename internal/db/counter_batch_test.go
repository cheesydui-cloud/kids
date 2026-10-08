package db

import "testing"

func TestUpdateUserTrafficBatchChunks(t *testing.T) {
	d := openTestDB(t)
	prev := counterBatchChunk
	counterBatchChunk = 2
	t.Cleanup(func() { counterBatchChunk = prev })

	ids := make([]int64, 3)
	rows := make([]UserTrafficUpdate, 3)
	for i := range ids {
		ids[i] = createTestUser(t, d)
		delta := int64(10 * (i + 1))
		rows[i] = UserTrafficUpdate{UserID: ids[i], Cycle: delta, Total: delta}
	}
	tx, err := d.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := UpdateUserTrafficBatch(tx, rows); err != nil {
		t.Fatal(err)
	}
	if err := AddUserDailyTrafficBatch(tx, rows); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var dailySum int64
	if err := d.QueryRow(`SELECT COALESCE(SUM(raw_bytes),0) FROM daily_user_traffic`).Scan(&dailySum); err != nil {
		t.Fatal(err)
	}
	if dailySum != 60 {
		t.Fatalf("daily sum = %d, want 60", dailySum)
	}
	for i, id := range ids {
		u, err := GetUserByID(d, id)
		if err != nil {
			t.Fatal(err)
		}
		want := int64(10 * (i + 1))
		if u.TrafficUsedBytes != want || u.TotalTrafficUsedBytes != want {
			t.Fatalf("user %d cycle=%d lifetime=%d, want %d", id, u.TrafficUsedBytes, u.TotalTrafficUsedBytes, want)
		}
	}
}
