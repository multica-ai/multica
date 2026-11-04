package connectedapps

import (
	"testing"
	"time"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		toolkit, tool string
		tags          []string
		want          string
	}{
		{"gmail", "GMAIL_FETCH_EMAILS", nil, ClassRead},
		{"gmail", "GMAIL_FETCH_MESSAGE_BY_THREAD_ID", nil, ClassRead},
		{"gmail", "GMAIL_CREATE_EMAIL_DRAFT", nil, ClassWrite},
		{"gmail", "GMAIL_SEND_EMAIL", nil, ClassSend},
		{"gmail", "GMAIL_REPLY_TO_THREAD", nil, ClassSend},
		{"gmail", "GMAIL_MOVE_TO_TRASH", nil, ClassDelete},
		{"slack", "SLACK_CHAT_POST_MESSAGE", nil, ClassSend},
		{"slack", "SLACK_LIST_ALL_CHANNELS", nil, ClassRead},
		{"notion", "NOTION_ADD_PAGE_CONTENT", nil, ClassWrite},
		{"notion", "NOTION_DELETE_BLOCK", nil, ClassDelete},
		{"googledrive", "GOOGLEDRIVE_ADD_FILE_SHARING_PREFERENCE", nil, ClassShare},
		{"googledrive", "GOOGLEDRIVE_LIST_PERMISSIONS", nil, ClassRead},
		{"googledrive", "GOOGLEDRIVE_SHARE_FILE", nil, ClassShare},
		{"googlecalendar", "GOOGLECALENDAR_CREATE_EVENT", nil, ClassSend},
		{"googlecalendar", "GOOGLECALENDAR_FIND_FREE_SLOTS", nil, ClassRead},
		{"linear", "LINEAR_CREATE_LINEAR_ISSUE", nil, ClassWrite},
		{"someapp", "SOMEAPP_FROBNICATE", nil, ClassSend},
		{"someapp", "SOMEAPP_FROBNICATE", []string{"readOnlyHint"}, ClassRead},
		{"someapp", "SOMEAPP_FROBNICATE", []string{"destructiveHint"}, ClassDelete},
	}
	for _, c := range cases {
		if got := Classify(c.toolkit, c.tool, c.tags); got != c.want {
			t.Errorf("Classify(%s, %s) = %s, want %s", c.toolkit, c.tool, got, c.want)
		}
	}
}

func TestDecide(t *testing.T) {
	cases := []struct {
		toolkit, level, class, rule, want string
	}{
		// Defaults under the most permissive level.
		{"notion", LevelWrite, ClassRead, "", PolicyAllow},
		{"notion", LevelWrite, ClassWrite, "", PolicyAllow},
		{"notion", LevelWrite, ClassSend, "", PolicyConfirm},
		{"notion", LevelWrite, ClassDelete, "", PolicyConfirm},
		{"notion", LevelWrite, ClassShare, "", PolicyBlock},
		// The default level confirms every write.
		{"notion", LevelConfirmWrites, ClassWrite, "", PolicyConfirm},
		{"notion", LevelConfirmWrites, ClassRead, "", PolicyAllow},
		// Read-only agents never write, whatever the rule says.
		{"notion", LevelRead, ClassWrite, PolicyAllow, PolicyBlock},
		{"notion", LevelRead, ClassRead, "", PolicyAllow},
		// A rule loosens or tightens the account; the level still caps it.
		{"gmail", LevelWrite, ClassSend, PolicyAllow, PolicyAllow},
		{"gmail", LevelConfirmWrites, ClassSend, PolicyAllow, PolicyConfirm},
		{"gmail", LevelWrite, ClassRead, PolicyBlock, PolicyBlock},
		// Curated defaults: Stripe is read-only unless a rule opens it.
		{"stripe", LevelWrite, ClassWrite, "", PolicyBlock},
		{"stripe", LevelWrite, ClassWrite, PolicyConfirm, PolicyConfirm},
		// An unknown rule value falls back to the default.
		{"notion", LevelWrite, ClassShare, "bogus", PolicyBlock},
	}
	for _, c := range cases {
		if got := Decide(c.toolkit, c.level, c.class, c.rule); got != c.want {
			t.Errorf("Decide(%s, %s, %s, %q) = %s, want %s", c.toolkit, c.level, c.class, c.rule, got, c.want)
		}
	}
}

func TestTaskToken(t *testing.T) {
	secret := DeriveTokenSecret([]byte("service-secret"))
	now := time.Unix(1_800_000_000, 0)
	token, err := SignTaskToken(secret, "task-1", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	got, err := VerifyTaskToken(secret, token, now)
	if err != nil || got != "task-1" {
		t.Fatalf("verify = %q, %v", got, err)
	}
	if _, err := VerifyTaskToken(secret, token, now.Add(2*time.Hour)); err != ErrTokenExpired {
		t.Fatalf("expired token: %v", err)
	}
	if _, err := VerifyTaskToken(DeriveTokenSecret([]byte("other")), token, now); err != ErrTokenSignature {
		t.Fatalf("foreign secret: %v", err)
	}
	if _, err := VerifyTaskToken(secret, token[:len(token)-2]+"xx", now); err != ErrTokenSignature {
		t.Fatalf("tampered token: %v", err)
	}
	if _, err := VerifyTaskToken(secret, "Bearer "+token, now); err != ErrTokenMalformed {
		t.Fatalf("unprefixed token: %v", err)
	}
}

func TestDefaultLevel(t *testing.T) {
	if got := DefaultLevel("private"); got != LevelWrite {
		t.Fatalf("private agent default = %q, want %q", got, LevelWrite)
	}
	for _, mode := range []string{"public_to", ""} {
		if got := DefaultLevel(mode); got != LevelConfirmWrites {
			t.Fatalf("%q agent default = %q, want %q", mode, got, LevelConfirmWrites)
		}
	}
}
