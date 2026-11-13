package channel

import (
	"bytes"
	"strings"
	"testing"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

func TestLinkReferencesContainerDestinations(t *testing.T) {
	const target = "mention://project/real-id"
	const destination = "https://deployment.example/team/projects/real-id"
	for _, body := range []string{
		"> [Project](\n> " + target + "\n> )",
		"> [Project](\n> <" + target + "> \"Title\"\n> )",
		"> > [Project](\n> > " + target + "\n> > )",
		"> - [Project](\n>   " + target + "\n>   )",
		"> [Project](\r\n> " + target + "\r\n> )",
		"- [Project](\n  " + target + "\n  )",
	} {
		t.Run(body, func(t *testing.T) {
			got := LinkReferences(body, func(kind, id string) string { return destination })
			if want := strings.Replace(body, target, destination, 1); got != want {
				t.Fatalf("rewrite=%q, want=%q", got, want)
			}
		})
	}
}

func TestLinkReferencesEscapedDestinations(t *testing.T) {
	const destination = "https://deployment.example/team/projects/real-id"
	for _, target := range []string{
		`mention\://project/real-id`,
		`mention:\/\/project/real-id`,
		`mention&#58;//project/real-id`,
		`&#109;ention://project/real-id`,
		`mention&colon;&sol;&sol;project/real-id`,
	} {
		t.Run(target, func(t *testing.T) {
			body := "[Project](" + target + " \"mention://project/real-id\")"
			got := LinkReferences(body, func(kind, id string) string {
				if kind != "project" || id != "real-id" {
					t.Fatalf("unexpected target: %s/%s", kind, id)
				}
				return destination
			})
			if want := "[Project](" + destination + " \"mention://project/real-id\")"; got != want {
				t.Fatalf("rewrite=%q, want=%q", got, want)
			}
			if unresolved := LinkReferences(body, func(string, string) string { return "" }); unresolved != body {
				t.Fatalf("unresolved destination changed: %q", unresolved)
			}
		})
	}
}

func TestLinkReferencesTableBoundaries(t *testing.T) {
	const issue = "[FEED-193](https://deployment.example/team/issues/FEED-193)"
	for _, tc := range []struct{ name, body, want string }{
		{"adjacent cells", "|docs|task|\n|---|---|\n|https://docs.example|FEED-193|", "|docs|task|\n|---|---|\n|https://docs.example|" + issue + "|"},
		{"URL query", "|docs|task|\n|---|---|\n|https://docs.example/?q=FEED-193|FEED-193|", "|docs|task|\n|---|---|\n|https://docs.example/?q=FEED-193|" + issue + "|"},
		{"emphasis in URL", "|docs|task|\n|---|---|\n|https://docs.example/?q=**FEED-193**|FEED-193|", "|docs|task|\n|---|---|\n|https://docs.example/?q=**FEED-193**|" + issue + "|"},
		{"escaped pipe in URL", "|docs|task|\n|---|---|\n|https://docs.example/?q=FEED-193\\|part|FEED-193|", "|docs|task|\n|---|---|\n|https://docs.example/?q=FEED-193\\|part|" + issue + "|"},
		{"quoted table", "> |docs|task|\n> |---|---|\n> |https://docs.example|FEED-193|", "> |docs|task|\n> |---|---|\n> |https://docs.example|" + issue + "|"},
		{"code with pipe", "|docs|task|\n|---|---|\n|`https://docs.example/FEED-193\\|FEED-193`|FEED-193|", "|docs|task|\n|---|---|\n|`https://docs.example/FEED-193\\|FEED-193`|" + issue + "|"},
		{"pipe outside table", "https://docs.example/?q=FEED-193|FEED-193", "https://docs.example/?q=FEED-193|FEED-193"},
		{"inline code", "`|docs|task|\n|---|---|\n|https://docs.example|FEED-193|`", "`|docs|task|\n|---|---|\n|https://docs.example|FEED-193|`"},
		{"without outer pipes", "docs|task\n---|---\nhttps://docs.example|FEED-193", "docs|task\n---|---\nhttps://docs.example|" + issue},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := LinkReferences(tc.body, func(kind, id string) string { return "https://deployment.example/team/issues/FEED-193" })
			if got != tc.want {
				t.Fatalf("rewrite=%q, want=%q", got, tc.want)
			}
		})
	}
}

func TestLinkReferencesReferenceTitles(t *testing.T) {
	const target = "mention://project/real-id"
	const destination = "https://deployment.example/team/projects/real-id"
	for _, form := range []string{"[Project][ref]", "[Project][]", "[Project]"} {
		for _, title := range []string{`"Title"`, `'A "quote" &amp; value'`, `"escaped \&amp; and \\path"`, "\"multi\nline\""} {
			t.Run(form+title, func(t *testing.T) {
				label := "ref"
				if form != "[Project][ref]" {
					label = "Project"
				}
				body := form + "\n\n[" + label + "]: " + target + " " + title
				for _, body := range []string{body, "> " + strings.ReplaceAll(body, "\n", "\n> ")} {
					converted := LinkReferences(body, func(string, string) string { return destination })
					var originalHTML, convertedHTML bytes.Buffer
					md := goldmark.New(goldmark.WithExtensions(extension.Table))
					if err := md.Convert([]byte(body), &originalHTML); err != nil {
						t.Fatal(err)
					}
					if err := md.Convert([]byte(converted), &convertedHTML); err != nil {
						t.Fatal(err)
					}
					if want := strings.Replace(originalHTML.String(), target, destination, 1); convertedHTML.String() != want {
						t.Fatalf("rendered=%q, want=%q; source=%q", convertedHTML.String(), want, converted)
					}
				}
			})
		}
	}
}

func TestLinkReferences(t *testing.T) {
	const destination = "https://deployment.example/team/projects/real-id"
	for _, tc := range []struct{ name, body, want string }{
		{"inline", "[Project](mention://project/real-id)", "[Project](" + destination + ")"},
		{"angle and title", "[Project](<mention://project/real-id> \"Title\")", "[Project](<" + destination + "> \"Title\")"},
		{"nested label", "[A [B]](mention://project/real-id)", "[A [B]](" + destination + ")"},
		{"full reference", "[Project][p]\n\n[p]: mention://project/real-id", "[Project](" + destination + ")\n\n[p]: mention://project/real-id"},
		{"collapsed reference", "[Project][]\n\n[Project]: mention://project/real-id", "[Project](" + destination + ")\n\n[Project]: mention://project/real-id"},
		{"shortcut reference", "[Project]\n\n[Project]: mention://project/real-id", "[Project](" + destination + ")\n\n[Project]: mention://project/real-id"},
		{"unresolved", "[Unknown](mention://future/real-id)", ""},
		{"code", "`[Project](mention://project/real-id)`", ""},
		{"image", "![Project](mention://project/real-id)", ""},
		{"external label", "[FEED-193](https://external.example)", ""},
		{"broadcast", "[@all](mention://all/all)", "@all"},
		{"broadcast escaped label", `[@All \[members\]](mention://all/all)`, `@All \[members\]`},
		{"broadcast reference", "[@all][everyone]\n\n[everyone]: mention://all/all", "@all\n\n[everyone]: mention://all/all"},
		{"broadcast code", "`[@all](mention://all/all)`", ""},
		{"invalid broadcast", "[@all](mention://all/not-all)", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := tc.want
			if want == "" {
				want = tc.body
			}
			got := LinkReferences(tc.body, func(kind, id string) string {
				if kind == "all" && id == "all" {
					t.Fatal("broadcast reached resolver")
				}
				if kind == "project" && id == "real-id" {
					return destination
				}
				return ""
			})
			if got != want {
				t.Fatalf("rewrite=%q, want=%q", got, want)
			}
		})
	}
}

func TestReferenceWebLink(t *testing.T) {
	for kind, route := range map[string]string{"issue": "issues", "project": "projects", "agent": "agents", "member": "members", "squad": "squads"} {
		t.Run(kind, func(t *testing.T) {
			if got := ReferenceWebLink("https://deployment.example/", "team", ""+kind, "id"); got != "https://deployment.example/team/"+route+"/id" {
				t.Fatal(got)
			}
		})
	}
	if got := ReferenceWebLink("https://deployment.example", "team", "all", "all"); got != "" {
		t.Fatalf("invented broadcast route: %q", got)
	}
	if got := ReferenceWebLink("https://deployment.example", "team space", "project", "id/with space"); !strings.Contains(got, "team%20space/projects/id%2Fwith%20space") {
		t.Fatalf("unsafe route escaping: %q", got)
	}
}

func TestLinkReferencesMarkdownBoundaries(t *testing.T) {
	const (
		docs    = "[docs](https://external.example)"
		issue   = "[FEED-193](https://deployment.example/team/issues/FEED-193)"
		project = "https://deployment.example/team/projects/real-id"
	)
	for _, tc := range []struct{ name, body, want string }{
		{"after link", docs + "FEED-193", docs + issue},
		{"after link with prose", docs + "后续任务：FEED-193", docs + "后续任务：" + issue},
		{"after parenthesized destination", "[docs](https://external.example/a_(b))FEED-193", "[docs](https://external.example/a_(b))" + issue},
		{"after image", "![preview](https://external.example/image.png)FEED-193", "![preview](https://external.example/image.png)" + issue},
		{"after inline code", "`https://external.example`FEED-193", "`https://external.example`" + issue},
		{"after autolink", "<https://external.example>FEED-193", "<https://external.example>" + issue},
		{"after reference", "[docs][d]FEED-193\n\n[d]: https://external.example", "[docs][d]" + issue + "\n\n[d]: https://external.example"},
		{"successive links", docs + "FEED-193" + docs + "FEED-193", docs + issue + docs + issue},
		{"next raw URL", docs + "https://next.example/?q=中文：FEED-193", ""},
		{"next email URL", docs + "mailto:a@example.test?subject=中文：FEED-193", ""},
		{"internal reference", "[Project](mention://project/real-id)FEED-193", "[Project](" + project + ")" + issue},
		{"broadcast", "[@all](mention://all/all)FEED-193", "@all" + issue},
		{"list", "- " + docs + "FEED-193", "- " + docs + issue},
		{"blockquote", "> " + docs + "FEED-193", "> " + docs + issue},
		{"raw URL spanning emphasis", "https://external.example/?q=中文：(**FEED-193**)", ""},
		{"underscore emphasis", "_FEED-193_", "_" + issue + "_"},
		{"underscore strong emphasis", "__FEED-193__", "__" + issue + "__"},
		{"nested emphasis", "**_FEED-193_**", "**_" + issue + "_**"},
		{"emphasis with prose", "_任务 FEED-193_ 和 _FEED-193 任务_", "_任务 " + issue + "_ 和 _" + issue + " 任务_"},
		{"emphasis after link", docs + "_FEED-193_", docs + "_" + issue + "_"},
		{"underscore word tokens", "some_FEED-193 FEED-193_suffix _some_FEED-193_ _FEED-193_suffix_", ""},
		{"raw URL spanning underscore emphasis", "https://external.example/?q=中文：(_FEED-193_)", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := tc.want
			if want == "" {
				want = tc.body
			}
			got := LinkReferences(tc.body, func(kind, id string) string {
				if kind == "issue" && id == "FEED-193" {
					return "https://deployment.example/team/issues/FEED-193"
				}
				if kind == "project" && id == "real-id" {
					return project
				}
				return ""
			})
			if got != want {
				t.Fatalf("rewrite=%q, want=%q", got, want)
			}
		})
	}
}
