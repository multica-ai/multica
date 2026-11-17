package lark

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestMarkdownImageLinks(t *testing.T) {
	for _, tc := range []struct{ name, input, want string }{
		{"artifact", "已交付\n![图片](https://multica.example/api/attachments/id/download)", "已交付\n[图片](https://multica.example/api/attachments/id/download)"},
		{"title and parentheses", `![a [b]](<https://example/a(b).png> "title")`, `[a [b]](<https://example/a(b).png> "title")`},
		{"multiple", "![一](a) ![二](b)", "[一](a) [二](b)"},
		{"reference", "![a][ref]\n\n[ref]: https://example/image.png", "[a][ref]\n\n[ref]: https://example/image.png"},
		{"nested", "![outer ![inner](b)](a)", "[outer [inner](b)](a)"},
		{"code", "`![inline](a)`\n\n```md\n![fenced](b)\n```", "`![inline](a)`\n\n```md\n![fenced](b)\n```"},
		{"escaped", `\![literal](a) [link](b)`, `\![literal](a) [link](b)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := markdownImageLinks(tc.input); got != tc.want {
				t.Fatalf("got %q; want %q", got, tc.want)
			}
		})
	}
}

func TestHTTPClient_MarkdownArtifactImageBecomesLink(t *testing.T) {
	fake := newLarkFake(t)
	fake.stubToken("test-token", 7200)
	fake.stubSend(map[string]any{"code": 0, "data": map[string]string{"message_id": "om_card"}}, func(r *http.Request, body map[string]string) {
		var card struct {
			Body struct {
				Elements []struct{ Content string }
			}
		}
		if err := json.Unmarshal([]byte(body["content"]), &card); err != nil {
			t.Fatal(err)
		}
		if len(card.Body.Elements) != 1 || card.Body.Elements[0].Content != "[图片](https://multica.example/api/attachments/id/download)" {
			t.Fatalf("card contains invalid image key syntax: %+v", card)
		}
	})
	_, err := newTestClient(fake, time.Now).SendMarkdownCard(context.Background(), SendMarkdownCardParams{
		InstallationID: testCreds(), ChatID: "oc_test", Markdown: "![图片](https://multica.example/api/attachments/id/download)",
	})
	if err != nil {
		t.Fatal(err)
	}
}
