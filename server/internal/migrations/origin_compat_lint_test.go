package migrations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// historicalShareCRMOriginVersions are ledger keys already applied by older
// ShareCRM releases. The runner stores the full filename, so these strings
// are not 681/682. The files must not return: a database that never recorded
// that exact key would execute them again, and those numbers now belong to
// other migrations.
var historicalShareCRMOriginVersions = []string{
	"649_issue_origin_sharecrm_chat",
	"650_issue_origin_sharecrm_chat_validate",
	"657_issue_origin_sharecrm_chat",
	"658_issue_origin_sharecrm_chat_validate",
	"661_issue_origin_sharecrm_chat",
	"662_issue_origin_sharecrm_chat_validate",
	"665_issue_origin_sharecrm_chat",
	"666_issue_origin_sharecrm_chat_validate",
	"673_issue_origin_sharecrm_chat",
	"674_issue_origin_sharecrm_chat_validate",
}

func TestHistoricalShareCRMOriginFilesStayRetired(t *testing.T) {
	dir := realMigrationsDir(t)
	recorded := readMigrationFile(t, "681_issue_origin_sharecrm_chat.up.sql")
	for _, version := range historicalShareCRMOriginVersions {
		for _, suffix := range []string{".up.sql", ".down.sql"} {
			path := filepath.Join(dir, version+suffix)
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("retired migration %s must not be on disk", version+suffix)
			}
		}
		if !strings.Contains(recorded, version) {
			t.Errorf("681 up migration must name retired ledger key %s", version)
		}
	}
}

func TestIssueOriginRepairDoesNotValidateInsideTheExclusiveSwap(t *testing.T) {
	up667 := readMigrationFile(t, "667_issue_origin_weixin_chat.up.sql")
	up673 := readMigrationFile(t, "681_issue_origin_sharecrm_chat.up.sql")
	up668 := readMigrationFile(t, "668_issue_origin_weixin_chat_validate.up.sql")
	for _, sql := range []string{up667, up673} {
		if strings.Contains(sql, "VALIDATE CONSTRAINT") || strings.Contains(sql, "COMMIT") {
			t.Fatal("widen migrations must only swap the check; validation is a later file")
		}
	}
	commit := strings.Index(up668, "\nCOMMIT;")
	validate := strings.Index(up668, "VALIDATE CONSTRAINT")
	if commit < 0 || validate < 0 || commit > validate {
		t.Fatal("668 must COMMIT the origin repair before VALIDATE so the scan does not hold ACCESS EXCLUSIVE")
	}
	if !strings.Contains(up668, "DISTINCT origin_type") {
		t.Fatal("668 must read existing origin values before validating a check that dropped them")
	}
	for _, name := range []string{
		"667_issue_origin_weixin_chat.down.sql",
		"681_issue_origin_sharecrm_chat.down.sql",
	} {
		sql := readMigrationFile(t, name)
		if strings.Contains(sql, "COMMIT") {
			t.Fatalf("%s must keep the narrowing and VALIDATE in one transaction", name)
		}
		if !strings.Contains(sql, "VALIDATE CONSTRAINT") {
			t.Fatalf("%s must fail closed with VALIDATE while rows of the removed origin remain", name)
		}
	}
}

func TestRuntimeRateLimitRenumberKeepsPublishedWeixinMigration(t *testing.T) {
	dir := realMigrationsDir(t)
	if _, err := os.Stat(filepath.Join(dir, "665_weixin_state.up.sql")); err != nil {
		t.Fatalf("665_weixin_state.up.sql must stay at its published name: %v", err)
	}
	weixin := readMigrationFile(t, "665_weixin_state.up.sql")
	if strings.Contains(weixin, "IF NOT EXISTS") {
		t.Fatal("665_weixin_state is not idempotent; do not re-run it under a new filename")
	}
	for _, name := range []string{"665_runtime_rate_limit", "675_runtime_rate_limit"} {
		for _, suffix := range []string{".up.sql", ".down.sql"} {
			if _, err := os.Stat(filepath.Join(dir, name+suffix)); !os.IsNotExist(err) {
				t.Fatalf("%s%s must not return; 665 is weixin_state and 675 is chat_wakeup", name, suffix)
			}
		}
	}
	up := readMigrationFile(t, "673_runtime_rate_limit.up.sql")
	if !strings.Contains(up, "CREATE TABLE IF NOT EXISTS runtime_rate_limit") {
		t.Fatal("673_runtime_rate_limit must leave an existing table in place for 665_runtime_rate_limit and 675_runtime_rate_limit")
	}
	if !strings.Contains(up, "675_runtime_rate_limit") || !strings.Contains(up, "665_runtime_rate_limit") {
		t.Fatal("673_runtime_rate_limit must name the retired ledger keys so they are not republished")
	}
	if strings.Contains(up, "CREATE INDEX CONCURRENTLY") {
		t.Fatal("673_runtime_rate_limit does not build an index concurrently")
	}
}
