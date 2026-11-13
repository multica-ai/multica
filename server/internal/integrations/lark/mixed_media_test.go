package lark

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type controlledCardClient struct {
	*fakeMediaAPIClient
	patch func(context.Context, PatchCardParams) error
}

type metadataCallbackQueries struct {
	*fakePatcherQueries
	afterLookup func()
}

func (q metadataCallbackQueries) ListAttachmentsByChatMessage(ctx context.Context, params db.ListAttachmentsByChatMessageParams) ([]db.Attachment, error) {
	rows, err := q.fakePatcherQueries.ListAttachmentsByChatMessage(ctx, params)
	q.afterLookup()
	return rows, err
}

func (c controlledCardClient) PatchInteractiveCard(ctx context.Context, p PatchCardParams) error {
	_ = c.fakeMediaAPIClient.PatchInteractiveCard(ctx, p)
	return c.patch(ctx, p)
}

func TestChatReplyMixedMedia(t *testing.T) {
	t.Run("authority changes during metadata lookup", func(t *testing.T) {
		p, q, api := newMediaTestPatcher(t)
		p.spawn = func(f func()) { f() }
		p.SetAttachments(fakeObjectStore{})
		q.attachments = []db.Attachment{imageRow(t, "aaaa2222-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "chart.png")}
		p.queries = metadataCallbackQueries{fakePatcherQueries: q, afterLookup: func() { q.installation.Status = "disabled" }}
		err := p.sendChatReply(context.Background(), testCreds(), q.binding, "", q.installation.WorkspaceID, protocol.ChatDonePayload{MessageID: "dddd4444-dddd-dddd-dddd-dddddddddddd", TaskID: "ee777777-ee77-ee77-ee77-eeeeeeeeeeee", Content: "Here is the chart."})
		if err == nil || len(api.mdCardSent) != 0 || len(api.textSent) != 0 || len(api.images) != 0 {
			t.Fatal("reply used authority revoked during metadata lookup")
		}
	})
	for _, tc := range []struct{ name, content string }{
		{"plain text", "Here is the chart."},
		{"image between paragraphs", "Before\n\n![chart](/api/attachments/aaaa2222-aaaa-aaaa-aaaa-aaaaaaaaaaaa/download)\n\nAfter"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, q, api := newMediaTestPatcher(t)
			api.mdCardReturn = "om_answer"
			p.SetAttachments(fakeObjectStore{objects: map[string][]byte{"chart.png": pngBytes, "notes.txt": []byte("notes")}})
			q.attachments = []db.Attachment{
				imageRow(t, "aaaa2222-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "chart.png"),
				fileRow(t, "bbbb3333-bbbb-bbbb-bbbb-bbbbbbbbbbbb", "notes.txt", "text/plain"),
			}
			payload := protocol.ChatDonePayload{MessageID: "dddd4444-dddd-dddd-dddd-dddddddddddd", TaskID: "ee777777-ee77-ee77-ee77-eeeeeeeeeeee", Content: tc.content}
			var worker func()
			p.spawn = func(f func()) { worker = f }
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if err := p.sendChatReply(ctx, testCreds(), q.binding, "", q.installation.WorkspaceID, payload); err != nil {
				t.Fatal(err)
			}
			if len(api.mdCardSent) != 1 || len(api.images) != 0 || worker == nil {
				t.Fatalf("text card must precede background uploads: cards=%d uploads=%d worker=%v", len(api.mdCardSent), len(api.images), worker != nil)
			}
			cancel() // The event publisher may finish before background uploads.
			worker()
			if len(api.patched) != 1 || api.patched[0].LarkCardMessageID != "om_answer" || len(api.imagesOut) != 0 || len(api.filesOut) != 1 {
				t.Fatalf("image must update the answer while file is separate: patches=%+v images=%d files=%d", api.patched, len(api.imagesOut), len(api.filesOut))
			}
			var card struct {
				Config struct {
					UpdateMulti bool `json:"update_multi"`
				}
				Body struct{ Elements []struct{ Content string } }
			}
			if err := json.Unmarshal([]byte(api.patched[0].CardJSON), &card); err != nil {
				t.Fatal(err)
			}
			if !card.Config.UpdateMulti || len(card.Body.Elements) != 1 {
				t.Fatalf("invalid shared card: %+v", card)
			}
			body := card.Body.Elements[0].Content
			if !strings.Contains(body, "![chart](img_fake)") && !strings.Contains(body, "![chart.png](img_fake)") {
				t.Fatalf("missing uploaded image key: %q", body)
			}
			if tc.name == "image between paragraphs" && body != "Before\n\n![chart](img_fake)\n\nAfter" {
				t.Fatalf("image moved within answer: %q", body)
			}
			if err := p.sendChatReply(context.Background(), testCreds(), q.binding, "", q.installation.WorkspaceID, payload); err != nil {
				t.Fatal(err)
			}
			if len(api.mdCardSent) != 1 || len(api.patched) != 1 || len(api.images) != 1 || len(api.filesOut) != 1 {
				t.Fatal("duplicate completion repeated mixed reply")
			}
		})
	}
}

func TestPatchMediaReplyCard(t *testing.T) {
	for _, scenario := range []string{"success", "lost response then success", "lost responses", "refused", "lost response then refused", "revoked after first attempt", "unsafe key", "empty key", "empty key suffix"} {
		t.Run(scenario, func(t *testing.T) {
			p, q, api := newMediaTestPatcher(t)
			calls := 0
			p.client = controlledCardClient{fakeMediaAPIClient: api, patch: func(context.Context, PatchCardParams) error {
				calls++
				switch scenario {
				case "lost response then success":
					if calls == 2 {
						return nil
					}
				case "success":
					return nil
				case "refused":
					return &APIError{Code: 230002}
				case "lost response then refused":
					if calls == 2 {
						return &APIError{Code: 230002}
					}
				case "revoked after first attempt":
					q.installation.Status = "disabled"
				}
				return errors.New("response lost")
			}}
			card := &mediaReplyCard{messageID: "om_original", markdown: "**before**\n\nAfter"}
			key := "img_fake"
			if scenario == "unsafe key" {
				key = "img_fake)\n![injected](x"
			}
			if scenario == "empty key" {
				key = ""
			}
			if scenario == "empty key suffix" {
				key = "img_"
			}
			err := p.patchMediaReplyCard(context.Background(), testCreds(), q.binding, q.installation.WorkspaceID, card, imageRow(t, "aaaa2222-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "chart.png"), key)
			var unknown *mediaSendUnconfirmed
			wantUnknown := strings.HasPrefix(scenario, "lost response") && scenario != "lost response then success" || scenario == "revoked after first attempt"
			if errors.As(err, &unknown) != wantUnknown {
				t.Fatalf("wrong receipt classification: %v", err)
			}
			wantCalls := 2
			if scenario == "success" || scenario == "refused" || scenario == "revoked after first attempt" {
				wantCalls = 1
			}
			if scenario == "unsafe key" || strings.HasPrefix(scenario, "empty key") {
				wantCalls = 0
			}
			if calls != wantCalls {
				t.Fatalf("patch attempts=%d; want %d", calls, wantCalls)
			}
			if (err == nil) != (scenario == "success" || scenario == "lost response then success") {
				t.Fatalf("wrong outcome: %v", err)
			}
			for _, patch := range api.patched {
				if patch.LarkCardMessageID != "om_original" || patch.CardJSON != api.patched[0].CardJSON {
					t.Fatal("retry changed target or absolute card body")
				}
			}
			if len(api.imagesOut) != 0 || len(api.sent) != 0 || len(api.mdCardSent) != 0 {
				t.Fatal("card update escaped into a separate message")
			}
			if (len(card.images) == 1) != (err == nil || wantUnknown) {
				t.Fatal("later update would lose an accepted or uncertain image")
			}
		})
	}

	t.Run("all images accumulate without moving paragraphs", func(t *testing.T) {
		p, q, api := newMediaTestPatcher(t)
		card := &mediaReplyCard{messageID: "om_original", markdown: "Before\n\n![first](" + fakeObjectPrefix + "first.png)\n\nBetween\n\n![second](" + fakeObjectPrefix + "second.png)\n\nAfter"}
		for i, name := range []string{"first.png", "second.png"} {
			row := imageRow(t, []string{"aaaa2222-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "bbbb3333-bbbb-bbbb-bbbb-bbbbbbbbbbbb"}[i], name)
			if err := p.patchMediaReplyCard(context.Background(), testCreds(), q.binding, q.installation.WorkspaceID, card, row, []string{"img_first", "img_second"}[i]); err != nil {
				t.Fatal(err)
			}
		}
		var got struct {
			Body struct{ Elements []struct{ Content string } }
		}
		if err := json.Unmarshal([]byte(api.patched[1].CardJSON), &got); err != nil {
			t.Fatal(err)
		}
		if got.Body.Elements[0].Content != "Before\n\n![first](img_first)\n\nBetween\n\n![second](img_second)\n\nAfter" {
			t.Fatalf("image ordering lost: %+v", got)
		}
	})
}

func TestMixedMediaMarkdown(t *testing.T) {
	image := uploadedCardImage{id: "aaaa2222-aaaa-aaaa-aaaa-aaaaaaaaaaaa", url: "https://cdn.example/chart.png", name: "chart.png", key: "img_real"}
	for _, tc := range []struct{ name, input, want string }{
		{"inline title and nested label", `Before ![a [b]](<https://cdn.example/chart.png> "title") After`, `Before ![a \[b\]](img_real) After`},
		{"reference", "Before\n\n![chart][ref]\n\nAfter\n\n[ref]: https://cdn.example/chart.png", "Before\n\n![chart](img_real)\n\nAfter\n\n[ref]: https://cdn.example/chart.png"},
		{"collapsed reference", "![chart][]\n\n[chart]: https://cdn.example/chart.png", "![chart](img_real)\n\n[chart]: https://cdn.example/chart.png"},
		{"shortcut reference", "![chart]\n\n[chart]: https://cdn.example/chart.png", "![chart](img_real)\n\n[chart]: https://cdn.example/chart.png"},
		{"escaped inline attachment path", `Before ![chart](\/api\/attachments\/aaaa2222-aaaa-aaaa-aaaa-aaaaaaaaaaaa\/download) After`, `Before ![chart](img_real) After`},
		{"escaped reference attachment path", "Before ![chart][ref] After\n\n[ref]: " + `\/api\/attachments\/aaaa2222-aaaa-aaaa-aaaa-aaaaaaaaaaaa\/download`, "Before ![chart](img_real) After\n\n[ref]: " + `\/api\/attachments\/aaaa2222-aaaa-aaaa-aaaa-aaaaaaaaaaaa\/download`},
		{"escaped inline absolute url", `Before ![chart](https\:\/\/cdn\.example\/chart\.png) After`, `Before ![chart](img_real) After`},
		{"escaped reference absolute url", "Before ![chart][ref] After\n\n[ref]: " + `https\:\/\/cdn\.example\/chart\.png`, "Before ![chart](img_real) After\n\n[ref]: " + `https\:\/\/cdn\.example\/chart\.png`},
		{"named entity inline absolute url", `Before ![chart](https&colon;&sol;&sol;cdn.example&sol;chart.png) After`, `Before ![chart](img_real) After`},
		{"numeric entity reference attachment path", "Before ![chart][ref] After\n\n[ref]: &#x2f;api&#47;attachments/aaaa2222-aaaa-aaaa-aaaa-aaaaaaaaaaaa/download", "Before ![chart](img_real) After\n\n[ref]: &#x2f;api&#47;attachments/aaaa2222-aaaa-aaaa-aaaa-aaaaaaaaaaaa/download"},
		{"entity is not decoded twice", "Before ![other](&amp;sol;api/attachments/aaaa2222-aaaa-aaaa-aaaa-aaaaaaaaaaaa/download) After", "Before [other](&amp;sol;api/attachments/aaaa2222-aaaa-aaaa-aaaa-aaaaaaaaaaaa/download) After\n\n![chart.png](img_real)"},
		{"numeric entity is not decoded twice", "Before ![other](&amp;#47;api/attachments/aaaa2222-aaaa-aaaa-aaaa-aaaaaaaaaaaa/download) After", "Before [other](&amp;#47;api/attachments/aaaa2222-aaaa-aaaa-aaaa-aaaaaaaaaaaa/download) After\n\n![chart.png](img_real)"},
		{"percent encoding is not decoded twice", "Before ![other](%252Fapi%252Fattachments%252Faaaa2222-aaaa-aaaa-aaaa-aaaaaaaaaaaa%252Fdownload) After", "Before [other](%252Fapi%252Fattachments%252Faaaa2222-aaaa-aaaa-aaaa-aaaaaaaaaaaa%252Fdownload) After\n\n![chart.png](img_real)"},
		{"escaped unbound attachment", `Before ![other](\/api\/attachments\/bbbb3333-bbbb-bbbb-bbbb-bbbbbbbbbbbb\/download) After`, "Before [other](\\/api\\/attachments\\/bbbb3333-bbbb-bbbb-bbbb-bbbbbbbbbbbb\\/download) After\n\n![chart.png](img_real)"},
		{"entities cannot authorize urls or keys", "![external](https&colon;&sol;&sol;evil.example/x) ![key](img&lowbar;fake)", "[external](https&colon;&sol;&sol;evil.example/x) [key](img&lowbar;fake)\n\n![chart.png](img_real)"},
		{"escaped destinations in code and escaped image syntax", "`![code](\\/api\\/attachments\\/aaaa2222-aaaa-aaaa-aaaa-aaaaaaaaaaaa\\/download)`\n\n```md\n![fence](https&colon;&sol;&sol;cdn.example&sol;chart.png)\n```\n\\![escaped](\\/api\\/attachments\\/aaaa2222-aaaa-aaaa-aaaa-aaaaaaaaaaaa\\/download)", "`![code](\\/api\\/attachments\\/aaaa2222-aaaa-aaaa-aaaa-aaaaaaaaaaaa\\/download)`\n\n```md\n![fence](https&colon;&sol;&sol;cdn.example&sol;chart.png)\n```\n\\![escaped](\\/api\\/attachments\\/aaaa2222-aaaa-aaaa-aaaa-aaaaaaaaaaaa\\/download)\n\n![chart.png](img_real)"},
		{"unknown urls and fake keys", "![external](https://evil.example/x) ![key](img_fake)", "[external](https://evil.example/x) [key](img_fake)\n\n![chart.png](img_real)"},
		{"code and escapes", "`![code](https://cdn.example/chart.png)`\n\n```md\n![fence](https://cdn.example/chart.png)\n```\n\\![escaped](https://cdn.example/chart.png)", "`![code](https://cdn.example/chart.png)`\n\n```md\n![fence](https://cdn.example/chart.png)\n```\n\\![escaped](https://cdn.example/chart.png)\n\n![chart.png](img_real)"},
		{"unbound attachment", "![other](/api/attachments/bbbb3333-bbbb-bbbb-bbbb-bbbbbbbbbbbb/download)", "[other](/api/attachments/bbbb3333-bbbb-bbbb-bbbb-bbbbbbbbbbbb/download)\n\n![chart.png](img_real)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := mixedMediaMarkdown(tc.input, []uploadedCardImage{image}); got != tc.want {
				t.Fatalf("got %q; want %q", got, tc.want)
			}
		})
	}
	t.Run("query entity in an exact storage url", func(t *testing.T) {
		image.url = "https://cdn.example/chart.png?width=100&height=200"
		for _, tc := range []struct{ name, input, want string }{
			{"inline", "Before ![chart](https://cdn.example/chart.png?width=100&amp;height=200) After", "Before ![chart](img_real) After"},
			{"reference", "Before ![chart][ref] After\n\n[ref]: https://cdn.example/chart.png?width=100&amp;height=200", "Before ![chart](img_real) After\n\n[ref]: https://cdn.example/chart.png?width=100&amp;height=200"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if got := mixedMediaMarkdown(tc.input, []uploadedCardImage{image}); got != tc.want {
					t.Fatalf("got %q; want %q", got, tc.want)
				}
			})
		}
	})
}

func TestReplyNeedsMediaCard(t *testing.T) {
	for _, scenario := range []string{"plain", "file", "image", "unavailable metadata", "lookup admission full", "storage absent", "invalid message"} {
		t.Run(scenario, func(t *testing.T) {
			p, q, _ := newMediaTestPatcher(t)
			p.SetAttachments(fakeObjectStore{})
			messageID := "dddd4444-dddd-dddd-dddd-dddddddddddd"
			switch scenario {
			case "file":
				q.attachments = []db.Attachment{fileRow(t, "aaaa2222-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "report.txt", "text/plain")}
			case "image":
				q.attachments = []db.Attachment{imageRow(t, "aaaa2222-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "chart.png")}
			case "unavailable metadata":
				q.attachmentsErr = errors.New("database unavailable")
			case "lookup admission full":
				for i := 0; i < cap(p.mediaAdmission); i++ {
					p.mediaAdmission <- struct{}{}
				}
			case "storage absent":
				p.SetAttachments(nil)
			case "invalid message":
				messageID = "invalid"
			}
			want := scenario == "image" || scenario == "unavailable metadata" || scenario == "lookup admission full"
			if got := p.replyNeedsMediaCard(context.Background(), q.installation.WorkspaceID, protocol.ChatDonePayload{MessageID: messageID}); got != want {
				t.Fatalf("got %v; want %v", got, want)
			}
		})
	}
}

func TestChatReplyMixedMediaFailure(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(map[bool]string{false: "card refused", true: "card response lost"}[uncertain], func(t *testing.T) {
			p, q, api := newMediaTestPatcher(t)
			p.spawn = func(f func()) { f() }
			p.SetAttachments(fakeObjectStore{objects: map[string][]byte{"chart.png": pngBytes, "notes.txt": []byte("notes")}})
			q.attachments = []db.Attachment{imageRow(t, "aaaa2222-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "chart.png"), fileRow(t, "bbbb3333-bbbb-bbbb-bbbb-bbbbbbbbbbbb", "notes.txt", "text/plain")}
			api.patchErr = &APIError{Code: 230099, Msg: "card refused"}
			wantNotice, wantPatches := mediaSendFailedText, 1
			if uncertain {
				api.patchErr = errors.New("patch response lost")
				wantNotice, wantPatches = mediaSendUnknownText, 2
			}
			payload := protocol.ChatDonePayload{MessageID: "dddd4444-dddd-dddd-dddd-dddddddddddd", TaskID: "ee777777-ee77-ee77-ee77-eeeeeeeeeeee", Content: "**Here is the chart.**"}
			if err := p.sendChatReply(context.Background(), testCreds(), q.binding, "", q.installation.WorkspaceID, payload); err != nil {
				t.Fatal(err)
			}
			if len(api.mdCardSent) != 1 || len(api.patched) != wantPatches || len(api.imagesOut) != 0 || len(api.filesOut) != 1 || len(api.textSent) != 1 || api.textSent[0].Text != wantNotice {
				t.Fatalf("mixed failure lost text/file or escaped into another image: cards=%d patches=%d images=%d files=%d notices=%+v", len(api.mdCardSent), len(api.patched), len(api.imagesOut), len(api.filesOut), api.textSent)
			}
		})
	}
}
