package migrations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// historicalShareCRMOriginVersions are ledger keys already applied by older
// ShareCRM releases. The runner stores the full filename, so these strings
// are not 756/757. The files must not return: a database that never recorded
// that exact key would execute them again, and those numbers now belong to
// other migrations. 681/682 are upstream's project_folder migrations.
// 683/684 are upstream's entitlement_feature_attempt migrations.
// 685-694 are upstream's docs tables and indexes. 695/696 are upstream's
// inbox returned_at column and active-shown index. 697/698 are upstream's
// autopilot_run idempotency key and its unique index. 703-707 are
// doc_comment and its keys. 708/709 are the attachment workspace size index
// and the drop of idx_attachment_workspace. 710 adds
// autopilot_quota_period.rejection_notified_limit. 711/712 are
// dashboard_range_view and its scope index. 713-731 are doc collab, inbox
// auto-archive, remote apps, issue-description collab, and skill revisions.
// 732-734 are comment_idempotency_receipt and its indexes. 735-742 are
// task_usage_hourly squad attribution. 743-746 are issue blockers. 747 is
// squad.handoff_mode. 748-751 are app tool-call quota. 752/753 record doc
// discussion runs and idx_agent_task_queue_doc. 754 skips blocker checks on
// a top-level issue insert. 755 recomputes hourly usage rollups per hour
// key. 685/686, 695/696, 697/698, 703/704, 711/712, 732/733, 735/736,
// 743/744, and 754/755 below are previous fork ShareCRM origin filenames.
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
	"681_issue_origin_sharecrm_chat",
	"682_issue_origin_sharecrm_chat_validate",
	"683_issue_origin_sharecrm_chat",
	"684_issue_origin_sharecrm_chat_validate",
	"685_issue_origin_sharecrm_chat",
	"686_issue_origin_sharecrm_chat_validate",
	"695_issue_origin_sharecrm_chat",
	"696_issue_origin_sharecrm_chat_validate",
	"697_issue_origin_sharecrm_chat",
	"698_issue_origin_sharecrm_chat_validate",
	"703_issue_origin_sharecrm_chat",
	"704_issue_origin_sharecrm_chat_validate",
	"711_issue_origin_sharecrm_chat",
	"712_issue_origin_sharecrm_chat_validate",
	"732_issue_origin_sharecrm_chat",
	"733_issue_origin_sharecrm_chat_validate",
	"735_issue_origin_sharecrm_chat",
	"736_issue_origin_sharecrm_chat_validate",
	"743_issue_origin_sharecrm_chat",
	"744_issue_origin_sharecrm_chat_validate",
	"754_issue_origin_sharecrm_chat",
	"755_issue_origin_sharecrm_chat_validate",
}

func TestHistoricalShareCRMOriginFilesStayRetired(t *testing.T) {
	dir := realMigrationsDir(t)
	recorded := readMigrationFile(t, "756_issue_origin_sharecrm_chat.up.sql")
	for _, version := range historicalShareCRMOriginVersions {
		for _, suffix := range []string{".up.sql", ".down.sql"} {
			path := filepath.Join(dir, version+suffix)
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("retired migration %s must not be on disk", version+suffix)
			}
		}
		if !strings.Contains(recorded, version) {
			t.Errorf("756 up migration must name retired ledger key %s", version)
		}
	}
}

func TestIssueOriginRepairDoesNotValidateInsideTheExclusiveSwap(t *testing.T) {
	up667 := readMigrationFile(t, "667_issue_origin_weixin_chat.up.sql")
	up703 := readMigrationFile(t, "756_issue_origin_sharecrm_chat.up.sql")
	up668 := readMigrationFile(t, "668_issue_origin_weixin_chat_validate.up.sql")
	for _, sql := range []string{up667, up703} {
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
		"756_issue_origin_sharecrm_chat.down.sql",
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
