package qoderruntime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestReplyFileNoticeLocaleSurvivesClaimAndJournal(t *testing.T) {
	for _, tc := range []struct{ locale, title string }{
		{"zh-Hans", "附件发送提示："},
		{"zh-Hant", "附件傳送提示："},
		{"ja", "添付ファイルの送信状況："},
		{"ko", "첨부 파일 전송 안내:"},
		{"fr", "État de l’envoi des pièces jointes :"},
		{"en", "Attachment delivery notice:"},
		{"", "Attachment delivery notice:"},
		{"unknown", "Attachment delivery notice:"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			h := setup(t)
			chatTask(h)
			raw, err := json.Marshal(h.task)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			if tc.locale != "" {
				fields["chat_notice_locale"], _ = json.Marshal(tc.locale)
			}
			raw, _ = json.Marshal(fields)
			var task claimedTask
			if err := json.Unmarshal(raw, &task); err != nil {
				t.Fatal(err)
			}
			r, err := h.b.prepare(context.Background(), &task)
			if err != nil {
				t.Fatal(err)
			}
			reasons := []string{
				"not attached because the bridge restarted during the run",
				"not attached because it exceeds the 10 MB limit",
				"not attached because Qoder did not offer the file for download",
				"not attached because access to the file could not be verified",
				"not attached because the download was refused",
				"not attached because Multica refused the upload",
				"delivery could not be confirmed; check the attachments before retrying",
				"not attached because Multica could not save the attachment",
				"not attached because the file transfer failed after 3 attempts",
			}
			for _, reason := range reasons {
				r.Reply.Skipped = append(r.Reply.Skipped, replyFileSkip{Name: "report.pdf", Reason: reason})
			}
			raw, err = json.Marshal(r.Reply)
			if err != nil {
				t.Fatal(err)
			}
			var restored replyFiles
			if err := json.Unmarshal(raw, &restored); err != nil {
				t.Fatal(err)
			}
			output := restored.output("Report")
			if !strings.Contains(output, tc.title) || strings.Count(output, "report.pdf") != len(reasons) || restored.token != "" || restored.Locale != tc.locale {
				t.Fatalf("wrong restored notice: locale=%q token persisted=%v output=%s", restored.Locale, restored.token != "", output)
			}
			if tc.locale != "en" && tc.locale != "" && tc.locale != "unknown" {
				for _, reason := range reasons {
					if strings.Contains(output, reason) {
						t.Fatalf("untranslated reason %q in %s", reason, output)
					}
				}
			}
		})
	}
}
