package dingtalk

import "testing"

func TestSealedInputQuotePreservesSourceSyntax(t *testing.T) {
	const path = "/api/attachments/01a08f95-e516-7c96-8164-5856dbee3a47/download"
	const img = "![](" + path + ")"
	for _, tc := range []struct{ name, input, want string }{
		{"empty", "", ""},
		{"plain text", "hello **world**", "hello **world**"},
		{"inline image", "前" + img + "后", "前[Image]后"},
		{"quoted image", "> old\n> " + img + "\n\ncurrent", "> old\n> [Image]\n\ncurrent"},
		{"inline code", "`" + img + "` " + img, "`" + img + "` [Image]"},
		{"fenced code", "```md\n" + img + "\n```\n\n" + img, "```md\n" + img + "\n```\n\n[Image]"},
		{"indented code", "    " + img + "\n\n" + img, "    " + img + "\n\n[Image]"},
		{"escaped image", `\` + img + " " + img, `\` + img + " [Image]"},
		{"ordinary link", "[download](" + path + ")", "[download](" + path + ")"},
		{"external image", "![](https://example.test" + path + ")", "![](https://example.test" + path + ")"},
		{"invalid attachment", "![](/api/attachments/not-an-id/download)", "![](/api/attachments/not-an-id/download)"},
		{"different route", "![](/api/attachments/01a08f95-e516-7c96-8164-5856dbee3a47/other)", "![](/api/attachments/01a08f95-e516-7c96-8164-5856dbee3a47/other)"},
		{"authored alt", "![diagram](" + path + ")", "![diagram](" + path + ")"},
		{"authored title", "![](" + path + ` "diagram")`, "![](" + path + ` "diagram")`},
		{"reference image", "![][ref]\n\n[ref]: " + path, "![][ref]\n\n[ref]: " + path},
		{"HTML code", "<pre>\n" + img + "\n</pre>\n\n" + img, "<pre>\n" + img + "\n</pre>\n\n[Image]"},
		{"image inside link", "[" + img + "](https://example.test)", "[[Image]](https://example.test)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := sealedInputQuote(tc.input); got != tc.want {
				t.Fatalf("quote = %q, want %q", got, tc.want)
			}
		})
	}
}
