package dingtalk

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

// This file is the root-cause regression guard for the /issue + media routing
// bug: the Router decides isIssueTurn / MediaChatBind from the image-STRIPPED
// Message.Text, while the session parses the /issue command from whatever the
// adapter puts in CommandText. If those two ever read different text, an
// image-first "/issue …" silently drops the issue (Router thinks issue, session
// doesn't) and orphans the attachment, or image markdown leaks into the parsed
// title. The invariant every case below asserts is: the Router's parse source
// (Message.Text) and the session's ACTUAL parse source (the appendInput output,
// mirroring session.AppendUserMessage's CommandText-or-Body selection) agree on
// issue-ness AND parse to an identical command — across every ordering and
// line-break arrangement of command / text / image.

// ti builds a richText text run.
func ti(text string) richTextItem { return richTextItem{Text: text} }

// pi builds a richText picture item with a usable download code.
func pi() richTextItem { return richTextItem{Type: "picture", DownloadCode: "code"} }

// richTextCallback builds a msgtype=richText DingTalk callback from ordered items.
func richTextCallback(items ...richTextItem) *botCallbackData {
	content, _ := json.Marshal(richTextContent{RichText: items})
	return &botCallbackData{SenderStaffId: "staff-1", Msgtype: "richText", Content: content}
}

// textCallback builds a msgtype=text DingTalk callback.
func textContentCallback(s string) *botCallbackData {
	return &botCallbackData{SenderStaffId: "staff-1", Msgtype: "text", Text: botCallbackText{Content: s}}
}

// pictureCallback builds a msgtype=picture (single image) DingTalk callback.
func pictureCallback() *botCallbackData {
	content, _ := json.Marshal(pictureContent{DownloadCode: "code"})
	return &botCallbackData{SenderStaffId: "staff-1", Msgtype: "picture", Content: content}
}

// commandSourceOf mirrors ChatSession.AppendUserMessage: the /issue command is
// parsed from CommandText, falling back to Body only when CommandText is empty.
func commandSourceOf(in engine.AppendInput) string {
	if in.CommandText != "" {
		return in.CommandText
	}
	return in.Body
}

func TestIssueMediaConsistency_AllPermutations(t *testing.T) {
	cases := []struct {
		name           string
		data           *botCallbackData
		wantIssue      bool
		wantTitle      string
		wantDesc       string
		wantForceFresh bool
		wantPending    int
	}{
		// ---- command + single image: every ordering ----
		{"img-first /issue", richTextCallback(pi(), ti("/issue fix login")), true, "fix login", "", false, 1},
		{"text-first /issue", richTextCallback(ti("/issue fix login"), pi()), true, "fix login", "", false, 1},
		{"text /issue then img then text", richTextCallback(ti("/issue fix login"), pi(), ti(" and this")), true, "fix login and this", "", false, 1},

		// ---- command + newline (title/description) + image: every ordering ----
		{"img-first /issue with newline body", richTextCallback(pi(), ti("/issue fix login\nrepro steps")), true, "fix login", "repro steps", false, 1},
		{"text-first /issue with newline body", richTextCallback(ti("/issue fix login\nrepro steps"), pi()), true, "fix login", "repro steps", false, 1},
		{"img between title and description", richTextCallback(ti("/issue fix login"), pi(), ti("\nrepro steps")), true, "fix login", "repro steps", false, 1},

		// ---- multiple images ----
		{"two imgs then /issue", richTextCallback(pi(), pi(), ti("/issue multi")), true, "multi", "", false, 2},
		{"/issue then two imgs", richTextCallback(ti("/issue multi"), pi(), pi()), true, "multi", "", false, 2},

		// ---- bare /issue + image (title comes from the DB fallback, so parse title is empty) ----
		{"img-first bare /issue", richTextCallback(pi(), ti("/issue")), true, "", "", false, 1},
		{"bare /issue then img", richTextCallback(ti("/issue"), pi()), true, "", "", false, 1},

		// ---- plain text + image: NOT an issue command, either ordering ----
		{"img + plain text", richTextCallback(pi(), ti("look at this")), false, "", "", false, 1},
		{"plain text + img", richTextCallback(ti("look at this"), pi()), false, "", "", false, 1},

		// ---- /issue not at line start, or not a whole token: NOT a command ----
		{"img + inline /issue mention", richTextCallback(pi(), ti("please /issue this")), false, "", "", false, 1},
		{"/issuetracker is not /issue", richTextCallback(ti("/issuetracker foo"), pi()), false, "", "", false, 1},

		// ---- /new normalization combined with /issue and image ----
		{"img-first /new /issue", richTextCallback(pi(), ti("/new /issue x")), true, "x", "", true, 1},
		{"/new only + img (fresh, not issue)", richTextCallback(ti("/new"), pi()), false, "", "", true, 1},
		{"/new run, img, /issue run", richTextCallback(ti("/new"), pi(), ti("/issue")), true, "", "", true, 1},
		{"text /new newline /issue", textContentCallback("/new\n/issue fix"), true, "fix", "", true, 0},
		{"text bare /new bare /issue", textContentCallback("/new /issue"), true, "", "", true, 0},
		{"text double /new keeps the second literal", textContentCallback("/new /new /issue"), false, "", "", true, 0},
		{"text /new after /issue is a literal title", textContentCallback("/issue /new x"), true, "/new x", "", false, 0},

		// ---- slash-joined pseudo-tokens: '/' is not a token boundary ----
		{"/issue/issue is not a command", textContentCallback("/issue/issue"), false, "", "", false, 0},
		{"/new/new is not a command", textContentCallback("/new/new"), false, "", "", false, 0},

		// ---- images with no text at all ----
		{"two images no text", richTextCallback(pi(), pi()), false, "", "", false, 2},
		{"/new + two images", richTextCallback(ti("/new"), pi(), pi()), false, "", "", true, 2},

		// ---- leading blank text run before the command ----
		{"leading blank run then /issue then img", richTextCallback(ti("\n"), ti("/issue x"), pi()), true, "x", "", false, 1},

		// ---- richText that is all text (degrades to the plain-text path) ----
		{"all-text richText /issue", richTextCallback(ti("/issue rich only")), true, "rich only", "", false, 0},

		// ---- msgtype=text (no segments) ----
		{"text /issue title", textContentCallback("/issue fix login"), true, "fix login", "", false, 0},
		{"text /issue title+desc", textContentCallback("/issue fix login\nrepro"), true, "fix login", "repro", false, 0},
		{"text /new /issue", textContentCallback("/new /issue x"), true, "x", "", true, 0},
		{"text plain", textContentCallback("hello world"), false, "", "", false, 0},
		{"text bare /issue", textContentCallback("/issue"), true, "", "", false, 0},
		{"text /issue with padding", textContentCallback("  /issue  spaced "), true, "spaced", "", false, 0},
		{"text /issue multiline desc", textContentCallback("/issue line1\nline2\nline3"), true, "line1", "line2\nline3", false, 0},

		// ---- msgtype=picture (single image, no text) ----
		{"picture only", pictureCallback(), false, "", "", false, 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg, ok := inboundFromCallback(tc.data, "app-key")
			if !ok {
				t.Fatalf("inboundFromCallback returned ok=false for a valid message")
			}
			if len(msg.PendingMedia) != tc.wantPending {
				t.Fatalf("pending media = %d, want %d", len(msg.PendingMedia), tc.wantPending)
			}
			if msg.ForceFresh != tc.wantForceFresh {
				t.Fatalf("ForceFresh = %v, want %v", msg.ForceFresh, tc.wantForceFresh)
			}

			// Stage one object per pending image (as the ingester would), so
			// ComposeBody produces real markdown for the transcript.
			staged := make([]engine.StagedMedia, len(msg.PendingMedia))
			for i := range staged {
				staged[i] = engine.StagedMedia{
					Filename: fmt.Sprintf("image-%d.png", i+1),
					URL:      fmt.Sprintf("https://files.test/i%d.png", i+1),
				}
			}
			in := appendInput(engine.AppendParams{Message: msg, Staged: staged})

			// The Router's decision source and the session's actual parse source.
			routerCmd, routerIssue := engine.ParseIssueCommand(msg.Text)
			sessionCmd, sessionIssue := engine.ParseIssueCommand(commandSourceOf(in))

			// (1) ROOT-CAUSE INVARIANT: the two sources must never disagree.
			if routerIssue != sessionIssue {
				t.Fatalf("router/session disagree on issue-ness: router=%v session=%v (Message.Text=%q, CommandText=%q, Body=%q)",
					routerIssue, sessionIssue, msg.Text, in.CommandText, in.Body)
			}
			// (2) …and must parse to an identical command.
			if routerIssue && !reflect.DeepEqual(routerCmd, sessionCmd) {
				t.Fatalf("router/session parse different commands: router=%+v session=%+v", routerCmd, sessionCmd)
			}

			// (3) Expected issue-ness and a CLEAN parsed title/description.
			if sessionIssue != tc.wantIssue {
				t.Fatalf("issue = %v, want %v (source=%q)", sessionIssue, tc.wantIssue, commandSourceOf(in))
			}
			if sessionIssue {
				if sessionCmd.Title != tc.wantTitle {
					t.Errorf("title = %q, want %q", sessionCmd.Title, tc.wantTitle)
				}
				if sessionCmd.Description != tc.wantDesc {
					t.Errorf("description = %q, want %q", sessionCmd.Description, tc.wantDesc)
				}
				if strings.Contains(sessionCmd.Title, "![") || strings.Contains(sessionCmd.Description, "![") {
					t.Errorf("image markdown leaked into the parsed command: title=%q desc=%q", sessionCmd.Title, sessionCmd.Description)
				}
			}

			// (4) MediaChatBind is derived from the SAME issue-ness (QuickCreate
			//     wired): an /issue turn's media rides the issue (bind=false); any
			//     other turn's media is chat-bound (bind=true). Because (1) holds,
			//     the bind decision can never contradict the session's parse.
			mediaChatBind := !sessionIssue
			if mediaChatBind == tc.wantIssue {
				t.Fatalf("mediaChatBind=%v is inconsistent with wantIssue=%v", mediaChatBind, tc.wantIssue)
			}

			// (5) The image ALWAYS survives into the stored Body (transcript/UI),
			//     even though it is stripped from the command parse.
			if len(staged) > 0 {
				if !strings.Contains(in.Body, "![") {
					t.Errorf("Body dropped the image markdown: %q", in.Body)
				}
				for _, sm := range staged {
					if !strings.Contains(in.Body, sm.URL) {
						t.Errorf("Body missing staged image URL %q: %q", sm.URL, in.Body)
					}
				}
			}
		})
	}
}
