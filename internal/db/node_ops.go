package db

import (
	"database/sql"
	"strings"
)

// UpdateNodeOpsFields writes the admin-only operations metadata for one node:
// free-text remark, renewal date (unix seconds, 0 = none) and monthly cost in
// cents (0 = unset). Folder membership is handled by SetNodeFolder.
func UpdateNodeOpsFields(d *sql.DB, id int64, remark string, expiresAt, monthlyCostCents int64) error {
	if monthlyCostCents < 0 {
		monthlyCostCents = 0
	}
	if expiresAt < 0 {
		expiresAt = 0
	}
	_, err := d.Exec(`UPDATE nodes SET remark=?, expires_at=?, monthly_cost_cents=? WHERE id=?`,
		strings.TrimSpace(remark), expiresAt, monthlyCostCents, id)
	return err
}
