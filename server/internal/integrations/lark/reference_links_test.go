package lark

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const referenceUUID = "11111111-1111-4111-8111-111111111111"
const issueUUID = "44444444-4444-4444-8444-444444444444"

type referenceLinkQueryFake struct {
	*fakePatcherQueries
	workspace       db.Workspace
	workspaceErr    error
	workspaceReads  int
	workspaceReadID pgtype.UUID
}

func (f *referenceLinkQueryFake) GetWorkspace(ctx context.Context, id pgtype.UUID) (db.Workspace, error) {
	f.workspaceReads++
	f.workspaceReadID = id
	if ctx.Err() != nil {
		return db.Workspace{}, ctx.Err()
	}
	return f.workspace, f.workspaceErr
}

func referenceLinkFixture(t *testing.T) (*Patcher, *referenceLinkQueryFake) {
	t.Helper()
	p, base, _ := newTestPatcher(t)
	q := &referenceLinkQueryFake{fakePatcherQueries: base, workspace: db.Workspace{ID: uuidFromString(t, "ffffffff-ffff-ffff-ffff-ffffffffffff"), Slug: "feedback", IssuePrefix: "FEED"}}
	p.queries = q
	p.cfg.AppURL = "https://deployment.example/"
	return p, q
}

func TestLinkReplyReferencesEntities(t *testing.T) {
	for kind, route := range map[string]string{"issue": "issues", "project": "projects", "member": "members", "agent": "agents", "squad": "squads"} {
		t.Run(kind, func(t *testing.T) {
			p, q := referenceLinkFixture(t)
			q.workspace.IssuePrefix = "" // UUID references do not need an issue prefix.
			id := referenceUUID
			body := "[@Label](mention://" + kind + "/" + id + ")"
			want := "[@Label](https://deployment.example/feedback/" + route + "/" + id + ")"
			if got := p.linkReplyReferences(context.Background(), q.workspace.ID, body+" "+body); got != want+" "+want {
				t.Fatalf("reply=%q, want=%q", got, want+" "+want)
			}
			if q.workspaceReads != 1 || q.workspaceReadID != q.workspace.ID {
				t.Fatalf("workspace reads=%d, id=%v", q.workspaceReads, q.workspaceReadID)
			}
		})
	}
}

func TestLinkReplyReferencesMarkdownForms(t *testing.T) {
	for kind, route := range map[string]string{"issue": "issues", "project": "projects", "member": "members", "agent": "agents", "squad": "squads"} {
		for _, tc := range []struct {
			name, body, want string
			protected        bool
		}{
			{"formatted label", `[**Label [inner]**](%s)`, `[**Label [inner]**](%s)`, false},
			{"escaped label and title", `[Label \[inner\]](<%s> "Title")`, `[Label \[inner\]](<%s> "Title")`, false},
			{"full reference", "[Label][ref]\n\n[ref]: %s", "[Label](%s)\n\n[ref]: %s", false},
			{"collapsed reference", "[Label][]\n\n[Label]: %s", "[Label](%s)\n\n[Label]: %s", false},
			{"shortcut reference", "[Label]\n\n[Label]: %s", "[Label](%s)\n\n[Label]: %s", false},
			{"multiline quote", "> [Label](\n> %s\n> )", "> [Label](\n> %s\n> )", false},
			{"quoted angle and title", "> [Label](\n> <%s> \"Title\"\n> )", "> [Label](\n> <%s> \"Title\"\n> )", false},
			{"nested quoted list", "> - [Label](\n>   %s\n>   )", "> - [Label](\n>   %s\n>   )", false},
			{"full reference with title", "[Label][ref]\n\n[ref]: %s \"Title\"", "[Label](%s \"Title\")\n\n[ref]: %s \"Title\"", false},
			{"collapsed reference with title", "[Label][]\n\n[Label]: %s \"Title\"", "[Label](%s \"Title\")\n\n[Label]: %s \"Title\"", false},
			{"shortcut reference with title", "[Label]\n\n[Label]: %s \"Title\"", "[Label](%s \"Title\")\n\n[Label]: %s \"Title\"", false},
			{"code", "`[Label](%s)`", "", true},
			{"image", "![Label](%s)", "", true},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				p, q := referenceLinkFixture(t)
				id, resolved := referenceUUID, referenceUUID
				if kind == "issue" {
					id, resolved = issueUUID, issueUUID
				}
				target := "mention://" + kind + "/" + id
				body := fmt.Sprintf(tc.body, target)
				want := body
				if !tc.protected {
					link := "https://deployment.example/feedback/" + route + "/" + resolved
					if strings.Contains(tc.body, "\n\n") {
						want = fmt.Sprintf(tc.want, link, target)
					} else {
						want = fmt.Sprintf(tc.want, link)
					}
				}
				if got := p.linkReplyReferences(context.Background(), q.workspace.ID, body); got != want {
					t.Fatalf("reply=%q, want=%q", got, want)
				}
				if tc.protected && q.workspaceReads != 0 {
					t.Fatal("protected Markdown caused a lookup")
				}
			})
		}
	}
}

func TestLinkReplyReferencesEncodedTargets(t *testing.T) {
	for kind, route := range map[string]string{"issue": "issues", "project": "projects", "member": "members", "agent": "agents", "squad": "squads"} {
		t.Run(kind, func(t *testing.T) {
			p, q := referenceLinkFixture(t)
			id, resolved := referenceUUID, referenceUUID
			if kind == "issue" {
				id, resolved = issueUUID, issueUUID
			}
			var body, want []string
			link := "https://deployment.example/feedback/" + route + "/" + resolved
			for _, scheme := range []string{`mention\://`, `mention:\/\/`, "mention&#58;//", "&#109;ention://", "mention&colon;&sol;&sol;"} {
				body = append(body, "[@Label]("+scheme+kind+"/"+id+")")
				want = append(want, "[@Label]("+link+")")
			}
			if got := p.linkReplyReferences(context.Background(), q.workspace.ID, strings.Join(body, " ")); got != strings.Join(want, " ") {
				t.Fatalf("reply=%q, want=%q", got, strings.Join(want, " "))
			}
			if q.workspaceReads != 1 {
				t.Fatalf("encoded targets queried workspace %d times", q.workspaceReads)
			}
		})
	}
}

func TestLinkReplyReferencesPreservesUnresolved(t *testing.T) {
	for _, kind := range []string{"issue", "project", "member", "agent", "squad"} {
		for _, failure := range []string{"malformed UUID", "workspace error", "missing slug", "wrong workspace metadata"} {
			t.Run(kind+"/"+failure, func(t *testing.T) {
				p, q := referenceLinkFixture(t)
				workspaceID := q.workspace.ID
				id := referenceUUID
				switch failure {
				case "malformed UUID":
					id = "not-an-id"
				case "workspace error":
					q.workspaceErr = errors.New("unavailable")
				case "missing slug":
					q.workspace.Slug = ""
				case "wrong workspace metadata":
					q.workspace.ID = uuidFromString(t, "55555555-5555-4555-8555-555555555555")
				}
				body := "[@Label](mention://" + kind + "/" + id + ")"
				if got := p.linkReplyReferences(context.Background(), workspaceID, body); got != body {
					t.Fatalf("unresolved reference changed: %q", got)
				}
			})
		}
	}
}

func TestLinkReplyReferencesLoadsWorkspaceOnceOnFailure(t *testing.T) {
	p, q := referenceLinkFixture(t)
	q.workspaceErr = errors.New("unavailable")
	body := "FEED-193 [Project](mention://project/" + referenceUUID + ") [Member](mention://member/" + referenceUUID + ")"
	if got := p.linkReplyReferences(context.Background(), q.workspace.ID, strings.Repeat(body, 30)); got != strings.Repeat(body, 30) {
		t.Fatalf("failed workspace lookup changed reply: %q", got)
	}
	if q.workspaceReads != 1 {
		t.Fatalf("failed workspace lookup retried %d times", q.workspaceReads)
	}
}

func TestLinkReplyReferencesBroadcastIsDisplayOnly(t *testing.T) {
	p, q := referenceLinkFixture(t)
	q.workspaceErr = errors.New("no lookup needed")
	if got := p.linkReplyReferences(context.Background(), q.workspace.ID, "FYI [@all](mention://all/all)"); got != "FYI @all" || q.workspaceReads != 0 {
		t.Fatalf("broadcast conversion=%q, reads=%d", got, q.workspaceReads)
	}
}

func TestPatcherLinksAllReferencesForFeishu(t *testing.T) {
	p, base, api := groupPatcherWithSender(t, "ou_sender")
	_, q := referenceLinkFixture(t)
	q.fakePatcherQueries = base
	base.installation.WorkspaceID = q.workspace.ID
	p.queries = q
	p.cfg.AppURL = "https://deployment.example"
	id := referenceUUID
	body := "[Issue](mention://issue/" + issueUUID + ")"
	want := "[Issue](https://deployment.example/feedback/issues/" + issueUUID + ")"
	for _, entry := range []struct{ kind, route string }{{"project", "projects"}, {"member", "members"}, {"agent", "agents"}, {"squad", "squads"}} {
		body += " [@Label](mention://" + entry.kind + "/" + id + ")"
		want += " [@Label](https://deployment.example/feedback/" + entry.route + "/" + id + ")"
	}
	body += " [@all](mention://all/all)"
	want += " @all"
	p.handleEvent(events.Event{Type: protocol.EventChatDone, TaskID: "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee", ChatSessionID: uuidString(base.binding.ChatSessionID), Payload: protocol.ChatDonePayload{Content: body}})
	if len(api.mdCardSent) != 1 || len(api.textSent) != 0 {
		t.Fatalf("expected one reference card: cards=%d, texts=%d", len(api.mdCardSent), len(api.textSent))
	}
	if got := api.mdCardSent[0]; got.Markdown != prependMarkdownMention("ou_sender", want) || got.ReplyTarget != threadReplyTarget(base.binding) {
		t.Fatalf("outbound reference conversion or routing changed: %+v", got)
	}
}

func TestLinkReplyIssues(t *testing.T) {
	const (
		internal = "mention://issue/44444444-4444-4444-8444-444444444444"
		uuidWeb  = "https://deployment.example/feedback/issues/44444444-4444-4444-8444-444444444444"
		web      = "https://deployment.example/feedback/issues/FEED-193"
	)
	for _, tc := range []struct{ name, body, want string }{
		{"UUID link", "已记录：[FEED-193](" + internal + ")。", "已记录：[FEED-193](" + uuidWeb + ")。"},
		{"identifier link", "[反馈](mention://issue/FEED-193)", "[反馈](" + web + ")"},
		{"nested label", "[反馈 [详情]](" + internal + ")", "[反馈 [详情]](" + uuidWeb + ")"},
		{"escaped label", `[反馈 \[详情\]](` + internal + `)`, `[反馈 \[详情\]](` + uuidWeb + `)`},
		{"code in label", "[详情 `]` FEED-193](" + internal + ")", "[详情 `]` FEED-193](" + uuidWeb + ")"},
		{"title and angle destination", "[反馈](<" + internal + "> \"详情\")", "[反馈](<" + uuidWeb + "> \"详情\")"},
		{"internal full reference", "[反馈][task]\n\n[task]: " + internal, "[反馈](" + uuidWeb + ")\n\n[task]: " + internal},
		{"internal collapsed reference", "[FEED-193][]\n\n[FEED-193]: " + internal, "[FEED-193](" + uuidWeb + ")\n\n[FEED-193]: " + internal},
		{"internal shortcut reference", "[FEED-193]\n\n[FEED-193]: " + internal, "[FEED-193](" + uuidWeb + ")\n\n[FEED-193]: " + internal},
		{"bare and punctuation", "已记录：FEED-193。 FEED-193.", "已记录：[FEED-193](" + web + ")。 [FEED-193](" + web + ")."},
		{"emphasis", "**FEED-193**", "**[FEED-193](" + web + ")**"},
		{"inline code", "`FEED-193` `[反馈](" + internal + ")`", ""},
		{"fenced code", "```md\nFEED-193\n[反馈](" + internal + ")\n```", ""},
		{"indented code", "    FEED-193\n    [反馈](" + internal + ")\n", ""},
		{"existing link", "[FEED-193](https://elsewhere.example)", ""},
		{"existing image", "![FEED-193](https://elsewhere.example/image.png)", ""},
		{"reference link", "[FEED-193][docs]\n\n[docs]: https://elsewhere.example", ""},
		{"raw URL query", "https://elsewhere.example?q=FEED-193#FEED-193", ""},
		{"URL parentheses", "https://elsewhere.example?q=(FEED-193)", ""},
		{"URL angle brackets", "<https://elsewhere.example/FEED-193>", ""},
		{"email", "person+FEED-193@example.test", ""},
		{"relative path", "/path?q=FEED-193", ""},
		{"file and path tokens", "FEED-193.ts path/FEED-193 FEED-193/file", ""},
		{"escaped example", `\[FEED-193](` + internal + `)`, ""},
		{"other prefix", "OTHER-193", ""},
		{"embedded identifier", "xFEED-193 FEED-193x FEED-193-2", ""},
		{"unknown issue", "FEED-194", "[FEED-194](https://deployment.example/feedback/issues/FEED-194)"},
		{"noncanonical number", "FEED-0193 FEED-0 FEED-99999999999999999999", ""},
		{"plain reply", "已记录。", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, q := referenceLinkFixture(t)
			want := tc.want
			if want == "" {
				want = tc.body
			}
			if got := p.linkReplyReferences(context.Background(), q.workspace.ID, tc.body); got != want {
				t.Fatalf("reply = %q, want %q", got, want)
			}
		})
	}
}

func TestLinkReplyIssuesAcceptsNumericPrefixes(t *testing.T) {
	for _, prefix := range []string{"1", "123", "1A", "A1", "1234567890"} {
		t.Run(prefix, func(t *testing.T) {
			p, q := referenceLinkFixture(t)
			q.workspace.IssuePrefix = prefix
			identifier := prefix + "-193"
			link := "https://deployment.example/feedback/issues/" + identifier
			if got := p.linkReplyReferences(context.Background(), q.workspace.ID, identifier); got != "["+identifier+"]("+link+")" {
				t.Fatalf("bare identifier did not link: %q", got)
			}
			q.workspaceReads = 0
			body := identifier + " [编号](mention://issue/" + identifier + ") [UUID](mention://issue/44444444-4444-4444-8444-444444444444)"
			want := "[" + identifier + "](" + link + ") [编号](" + link + ") [UUID](https://deployment.example/feedback/issues/" + issueUUID + ")"
			if got := p.linkReplyReferences(context.Background(), q.workspace.ID, body); got != want {
				t.Fatalf("reply = %q, want %q", got, want)
			}
			if q.workspaceReads != 1 || q.workspaceReadID != q.workspace.ID {
				t.Fatalf("workspace reads=%d, id=%v", q.workspaceReads, q.workspaceReadID)
			}
		})
	}
}

func TestLinkReplyIssuesPreservesURLs(t *testing.T) {
	for _, rawURL := range []string{
		"https://example.test/path：FEED-193",
		"https://example.test/?q=中文，FEED-193",
		"https://example.test/#上下文；FEED-193",
		"https://example.test/?q=中文。FEED-193",
		"https://example.test/?q=中文：(**FEED-193**)",
		"https://example.test/?q=中文：FEED-193&amp;other=FEED-193",
		"HTTP://localhost/?q=中文：FEED-193",
		"ftp://example.test/中文：FEED-193",
		"www.example.test/?q=中文：FEED-193",
		"//example.test/?q=中文：FEED-193",
		"/path?q=中文：FEED-193",
		`C:\path\中文：FEED-193`,
		"mailto:person@example.test?subject=中文：FEED-193",
		"person+FEED-193+tag@example.test",
		"FEED-193+person@example.test",
		"https://example.test/?q=" + strings.Repeat("中文；FEED-193", 30),
	} {
		t.Run(rawURL, func(t *testing.T) {
			p, q := referenceLinkFixture(t)
			if got := p.linkReplyReferences(context.Background(), q.workspace.ID, rawURL); got != rawURL || q.workspaceReads != 0 {
				t.Fatalf("URL-only reply changed or queried workspace: reply=%q, reads=%d", got, q.workspaceReads)
			}
			body := rawURL + "\u00a0正文：FEED-193。"
			want := rawURL + "\u00a0正文：[FEED-193](https://deployment.example/feedback/issues/FEED-193)。"
			if got := p.linkReplyReferences(context.Background(), q.workspace.ID, body); got != want {
				t.Fatalf("reply = %q, want %q", got, want)
			}
			if q.workspaceReads != 1 {
				t.Fatalf("workspace reads=%d", q.workspaceReads)
			}
		})
	}
}

func TestLinkReplyIssuesPreservesReplyWhenLookupUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*Patcher, *referenceLinkQueryFake)
	}{
		{"workspace error", func(_ *Patcher, q *referenceLinkQueryFake) { q.workspaceErr = errors.New("unavailable") }},
		{"missing slug", func(_ *Patcher, q *referenceLinkQueryFake) { q.workspace.Slug = "" }},
		{"missing app URL", func(p *Patcher, _ *referenceLinkQueryFake) { p.cfg.AppURL = "" }},
		{"non HTTP app URL", func(p *Patcher, _ *referenceLinkQueryFake) { p.cfg.AppURL = "mention://app" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, q := referenceLinkFixture(t)
			tc.setup(p, q)
			body := "FEED-193 [Issue](mention://issue/" + issueUUID + ")"
			if got := p.linkReplyReferences(context.Background(), q.workspace.ID, body); got != body {
				t.Fatalf("reply changed without workspace metadata: %q", got)
			}
		})
	}
}

func TestLinkReplyReferencesWorkspaceReadIsPerReply(t *testing.T) {
	p, q := referenceLinkFixture(t)
	body := "[Project](mention://project/" + referenceUUID + ")"
	want := "[Project](https://deployment.example/feedback/projects/" + referenceUUID + ")"
	if got := p.linkReplyReferences(context.Background(), q.workspace.ID, body); got != want {
		t.Fatalf("first reply=%q", got)
	}
	q.workspace.Slug = "renamed"
	want = "[Project](https://deployment.example/renamed/projects/" + referenceUUID + ")"
	if got := p.linkReplyReferences(context.Background(), q.workspace.ID, body); got != want {
		t.Fatalf("second reply=%q", got)
	}
	if q.workspaceReads != 2 || q.workspaceReadID != q.workspace.ID {
		t.Fatalf("workspace reads=%d", q.workspaceReads)
	}
}

func TestPatcherLinksIssueReferencesForFeishu(t *testing.T) {
	for _, tc := range []struct{ body, target string }{
		{"已记录：[DEMO-193](mention://issue/" + issueUUID + ")。", issueUUID},
		{"已记录：DEMO-193。", "DEMO-193"},
	} {
		t.Run(tc.body, func(t *testing.T) {
			p, base, api := groupPatcherWithSender(t, "ou_sender")
			wsID := uuidFromString(t, "ffffffff-ffff-ffff-ffff-ffffffffffff")
			base.installation.WorkspaceID = wsID
			q := &referenceLinkQueryFake{fakePatcherQueries: base, workspace: db.Workspace{ID: wsID, Slug: "demo", IssuePrefix: "DEMO"}}
			p.queries = q
			p.cfg.AppURL = "https://multica.example"
			p.handleEvent(events.Event{Type: protocol.EventChatDone, TaskID: "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee", ChatSessionID: uuidString(base.binding.ChatSessionID), Payload: protocol.ChatDonePayload{Content: tc.body}})
			if len(api.mdCardSent) != 1 || len(api.textSent) != 0 {
				t.Fatalf("cards=%d, texts=%d", len(api.mdCardSent), len(api.textSent))
			}
			got := api.mdCardSent[0]
			want := prependMarkdownMention("ou_sender", "已记录：[DEMO-193](https://multica.example/demo/issues/"+tc.target+")。")
			if got.Markdown != want || got.ReplyTarget != threadReplyTarget(base.binding) {
				t.Fatalf("outbound=%+v, want=%q", got, want)
			}
			if q.workspaceReads != 1 {
				t.Fatalf("workspace reads=%d", q.workspaceReads)
			}
		})
	}
}

func TestLinkReplyReferencesNonexistentTargets(t *testing.T) {
	const id = "99999999-9999-4999-8999-999999999999"
	for kind, route := range map[string]string{"issue": "issues", "project": "projects", "member": "members", "agent": "agents", "squad": "squads"} {
		t.Run(kind, func(t *testing.T) {
			p, q := referenceLinkFixture(t)
			body := "[Missing](mention://" + kind + "/" + id + ")"
			want := "[Missing](https://deployment.example/feedback/" + route + "/" + id + ")"
			if got := p.linkReplyReferences(context.Background(), q.workspace.ID, body); got != want {
				t.Fatalf("reply=%q, want=%q", got, want)
			}
			if q.workspaceReads != 1 {
				t.Fatalf("workspace reads=%d", q.workspaceReads)
			}
		})
	}
	t.Run("matching issue identifier", func(t *testing.T) {
		p, q := referenceLinkFixture(t)
		body := "FEED-194 [Missing](mention://issue/FEED-194)"
		link := "https://deployment.example/feedback/issues/FEED-194"
		want := "[FEED-194](" + link + ") [Missing](" + link + ")"
		if got := p.linkReplyReferences(context.Background(), q.workspace.ID, body); got != want {
			t.Fatalf("reply=%q, want=%q", got, want)
		}
		if q.workspaceReads != 1 {
			t.Fatalf("workspace reads=%d", q.workspaceReads)
		}
	})
}

func TestLinkReplyReferencesManyTargetsUseOneWorkspaceRead(t *testing.T) {
	for _, count := range []int{1, 32, 80} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			p, q := referenceLinkFixture(t)
			var body, want strings.Builder
			for i := 1; i <= count; i++ {
				id := fmt.Sprintf("%08x-9999-4999-8999-999999999999", i)
				for _, entry := range []struct{ kind, route string }{{"issue", "issues"}, {"project", "projects"}, {"member", "members"}, {"agent", "agents"}, {"squad", "squads"}} {
					fmt.Fprintf(&body, "[Label](mention://%s/%s) ", entry.kind, id)
					fmt.Fprintf(&want, "[Label](https://deployment.example/feedback/%s/%s) ", entry.route, id)
				}
				fmt.Fprintf(&body, "FEED-%d ", i)
				fmt.Fprintf(&want, "[FEED-%d](https://deployment.example/feedback/issues/FEED-%d) ", i, i)
			}
			if got := p.linkReplyReferences(context.Background(), q.workspace.ID, body.String()); got != want.String() {
				t.Fatal("distinct nonexistent targets were not all linked")
			}
			if q.workspaceReads != 1 {
				t.Fatalf("workspace reads=%d for %d targets", q.workspaceReads, count*6)
			}
		})
	}
}

func TestLinkReplyReferencesNoCandidatesAvoidWorkspaceRead(t *testing.T) {
	for _, body := range []string{
		"Plain reply.",
		"`FEED-193 [Project](mention://project/11111111-1111-4111-8111-111111111111)`",
		"```md\nFEED-193\n[Project](mention://project/11111111-1111-4111-8111-111111111111)\n```",
		"![FEED-193](mention://project/11111111-1111-4111-8111-111111111111)",
		"[FEED-193](https://docs.example/FEED-193)",
		"https://docs.example/?q=FEED-193",
		"[Label](mention://unknown/11111111-1111-4111-8111-111111111111)",
		"[Label](mention://project/not-a-uuid)",
		"[Label](mention://issue/not-a-uuid)",
		"FEED-0 FEED-0193 FEED-999999999999999999999",
	} {
		t.Run(body, func(t *testing.T) {
			p, q := referenceLinkFixture(t)
			if got := p.linkReplyReferences(context.Background(), q.workspace.ID, body); got != body {
				t.Fatalf("reply changed: %q", got)
			}
			if q.workspaceReads != 0 {
				t.Fatalf("nonconvertible content queried workspace %d times", q.workspaceReads)
			}
		})
	}
}

func TestLinkReplyReferencesUnrelatedPrefixes(t *testing.T) {
	for _, tc := range []struct{ prefix, body, want string }{
		{"FEED", "UTF-8 SHA-256 OTHER-193", "UTF-8 SHA-256 OTHER-193"},
		{"FEED", "UTF-8 FEED-194 SHA-256", "UTF-8 [FEED-194](https://deployment.example/feedback/issues/FEED-194) SHA-256"},
		{"UTF", "UTF-8 SHA-256", "[UTF-8](https://deployment.example/feedback/issues/UTF-8) SHA-256"},
	} {
		t.Run(tc.prefix+"/"+tc.body, func(t *testing.T) {
			p, q := referenceLinkFixture(t)
			q.workspace.IssuePrefix = tc.prefix
			if got := p.linkReplyReferences(context.Background(), q.workspace.ID, tc.body); got != tc.want {
				t.Fatalf("reply=%q, want=%q", got, tc.want)
			}
			if q.workspaceReads != 1 || q.workspaceReadID != q.workspace.ID {
				t.Fatalf("workspace reads=%d, id=%v", q.workspaceReads, q.workspaceReadID)
			}
		})
	}
}

func TestLinkReplyReferencesUUIDsWithoutIssuePrefix(t *testing.T) {
	p, q := referenceLinkFixture(t)
	q.workspace.IssuePrefix = ""
	body := "FEED-193 [Issue](mention://issue/" + issueUUID + ")"
	want := "FEED-193 [Issue](https://deployment.example/feedback/issues/" + issueUUID + ")"
	if got := p.linkReplyReferences(context.Background(), q.workspace.ID, body); got != want {
		t.Fatalf("reply=%q, want=%q", got, want)
	}
	if q.workspaceReads != 1 {
		t.Fatalf("workspace reads=%d", q.workspaceReads)
	}
}

func TestLinkReplyReferencesMemberUsesMentionUserID(t *testing.T) {
	p, q := referenceLinkFixture(t)
	const userID = "22222222-2222-4222-8222-222222222222"
	body := "[@User](mention://member/" + userID + ")"
	want := "[@User](https://deployment.example/feedback/members/" + userID + ")"
	if got := p.linkReplyReferences(context.Background(), q.workspace.ID, body); got != want {
		t.Fatalf("reply=%q, want=%q", got, want)
	}
}

func TestLinkReplyReferencesCanceledContext(t *testing.T) {
	p, q := referenceLinkFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	body := "FEED-193 [Project](mention://project/" + referenceUUID + ")"
	if got := p.linkReplyReferences(ctx, q.workspace.ID, body); got != body || q.workspaceReads != 1 {
		t.Fatalf("reply=%q, reads=%d", got, q.workspaceReads)
	}
}
