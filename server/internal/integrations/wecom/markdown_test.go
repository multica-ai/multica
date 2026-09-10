package wecom

import (
	"net/url"
	"strings"
	"testing"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// TestBreakLinkAdjacency covers the whole contract: the only edit is a space
// between "]" and "(", nothing else in the string moves, and no backslash is
// ever produced — a backslash is what the previous mechanism emitted and what
// the live tenant showed to be unusable.
func TestBreakLinkAdjacency(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"ordinary bracketed title is untouched", "[Bug] 登录失败", "[Bug] 登录失败"},
		{"parens alone are untouched", "修复 (见 issue 12)", "修复 (见 issue 12)"},
		{"a lone bracket is untouched", "开了一个 [ 没关", "开了一个 [ 没关"},
		{"exclamation alone is untouched", "紧急!!!", "紧急!!!"},
		{"member backslash is left exactly as written", `修复路径 C:\`, `修复路径 C:\`},
		{"link", "[click here](http://evil.example)", "[click here] (http://evil.example)"},
		{"image", "![img](http://evil.example/x.png)", "![img] (http://evil.example/x.png)"},
		{"nested brackets", "[a[b]](http://evil.example)", "[a[b]] (http://evil.example)"},
		{"back to back", "](](", "] (] ("},
		{"two links", "[a](x) and [b](y)", "[a] (x) and [b] (y)"},
		{"backslash in front of the pair does not help the member", `x\](u)`, `x\] (u)`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := breakLinkAdjacency(tc.in)
			if got != tc.want {
				t.Fatalf("breakLinkAdjacency(%q):\n got %q\nwant %q", tc.in, got, tc.want)
			}
			if strings.Contains(got, "](") {
				t.Fatalf("%q still holds an adjacent \"](\" — a link can still form", got)
			}
			if !strings.Contains(tc.in, `\`) && strings.Contains(got, `\`) {
				t.Fatalf("emitted a backslash for %q: %q — WeCom reads \"\\[\" as a math delimiter", tc.in, got)
			}
		})
	}
}

// TestBreakLinkAdjacencyIsIdempotent: the output contains no "](", so a second
// pass must be a no-op. Matters because the same text can reach more than one
// builder, and a mechanism that kept inserting spaces would drift.
func TestBreakLinkAdjacencyIsIdempotent(t *testing.T) {
	for _, in := range []string{"[a](b)", "](](", "[Bug] 登录失败", "![i](u)"} {
		once := breakLinkAdjacency(in)
		if twice := breakLinkAdjacency(once); twice != once {
			t.Fatalf("second pass changed %q: %q → %q", in, once, twice)
		}
	}
}

// linkReferenceAttacks are bodies that define a link reference and then use it.
// Every one of them is a real CommonMark definition — the test below proves
// that by rendering each unguarded and requiring the attacker's host to come
// back as a destination, so the corpus cannot rot into a set of strings that
// were never dangerous.
//
// The shapes here are the ones a rule built on "]: looks like a URL" can get
// wrong: the destination hidden behind a "<", pushed onto the next line, spelt
// with backslash escapes or a character reference, or defined inside a
// blockquote or a list item where the label is no longer at column zero.
//
// The blockquote-continuation block is the one that got through: the label
// half accepted the ">" and the destination half did not expect it back on the
// next line, so the scan read the marker as the destination and cleared a
// definition that resolves. TestContainerPrefixHalvesAgree is the test for the
// disagreement itself; these pin the shapes it reached.
var linkReferenceAttacks = []struct {
	name string
	body string
}{
	{"plain", "[重置密码]: https://evil.example\n\n[重置密码]"},
	{"no space after the colon", "[重置密码]:https://evil.example\n\n[重置密码]"},
	{"indented three spaces", "   [重置密码]: https://evil.example\n\n[重置密码]"},
	{"angle bracketed", "[重置密码]: <https://evil.example>\n\n[重置密码]"},
	{"destination on the next line", "[重置密码]:\nhttps://evil.example\n\n[重置密码]"},
	{"with a title", "[重置密码]: https://evil.example \"点这里\"\n\n[重置密码]"},
	{"title on the next line", "[重置密码]: https://evil.example\n\"点这里\"\n\n[重置密码]"},
	{"scheme relative", "[重置密码]: //evil.example/reset\n\n[重置密码]"},
	{"escaped scheme colon", `[重置密码]: https\://evil.example` + "\n\n[重置密码]"},
	{"escaped slashes", `[重置密码]: \/\/evil.example/reset` + "\n\n[重置密码]"},
	{"hex character reference", "[重置密码]: &#x68;ttps://evil.example\n\n[重置密码]"},
	{"decimal character reference", "[重置密码]: &#104;ttps://evil.example\n\n[重置密码]"},
	{"inside a blockquote", "> [重置密码]: https://evil.example\n\n[重置密码]"},
	{"inside a list item", "- [重置密码]: https://evil.example\n\n[重置密码]"},
	{"inside an ordered list item", "1. [重置密码]: https://evil.example\n\n[重置密码]"},
	{"escaped bracket in the label", `[重置\]密码]: https://evil.example` + "\n\n" + `[重置\]密码]`},
	{"label across two lines", "[重置\n密码]: https://evil.example\n\n[重置\n密码]"},
	{"case folded label", "[Reset]: https://evil.example\n\n[reset]"},
	{"collapsed reference", "[重置密码]: https://evil.example\n\n[重置密码][]"},
	{"full reference", "[重置密码]: https://evil.example\n\n[点这里][重置密码]"},
	{"two definitions", "[a]: https://evil.example\n[b]: https://evil.example/2\n\n[a] [b]"},
	{"uppercase scheme", "[重置密码]: HTTPS://EVIL.EXAMPLE\n\n[重置密码]"},

	// The destination on its own line, behind the block container the
	// definition sits in.
	{"blockquote continuation", "> [重置密码]:\n> https://evil.example\n\n[重置密码]"},
	{"blockquote continuation, no space after the marker", ">[重置密码]:\n>https://evil.example\n\n[重置密码]"},
	{"blockquote continuation, tab after the marker", ">\t[重置密码]:\n>\thttps://evil.example\n\n[重置密码]"},
	{"blockquote continuation, CRLF", "> [重置密码]:\r\n> https://evil.example\r\n\r\n[重置密码]"},
	{"blockquote continuation, nested twice", "> > [重置密码]:\n> > https://evil.example\n\n[重置密码]"},
	{"blockquote continuation, nested three deep", "> > > [重置密码]:\n> > > https://evil.example\n\n[重置密码]"},
	{"blockquote continuation, indented markers", "  >  >  [重置密码]:\n  >  >  https://evil.example\n\n[重置密码]"},
	{"list inside a blockquote, continued by indentation", "> - [重置密码]:\n>   https://evil.example\n\n[重置密码]"},
	{"ordered list inside a blockquote", "> 1. [重置密码]:\n>    https://evil.example\n\n[重置密码]"},
	{"blockquote continuation, angle bracketed", "> [重置密码]:\n> <https://evil.example>\n\n[重置密码]"},
	{"blockquote continuation, scheme relative", "> [重置密码]:\n> //evil.example/x\n\n[重置密码]"},
	{"blockquote continuation, escaped scheme colon", "> [重置密码]:\n> " + `https\://evil.example` + "\n\n[重置密码]"},
	{"blockquote continuation, character reference", "> [重置密码]:\n> &#x68;ttps://evil.example\n\n[重置密码]"},
	{"blockquote lazy continuation, marker dropped", "> [重置密码]:\nhttps://evil.example\n\n[重置密码]"},
	{"list continuation by indentation", "- [重置密码]:\n  https://evil.example\n\n[重置密码]"},

	// A definition reached this way feeds the same reference map, so it is
	// worth pinning that every way of spending it is closed, not just the
	// shortcut form.
	{"blockquote continuation, collapsed reference", "> [重置密码]:\n> https://evil.example\n\n[重置密码][]"},
	{"blockquote continuation, full reference", "> [重置密码]:\n> https://evil.example\n\n[点这里][重置密码]"},
	{"blockquote continuation, image reference", "> [重置密码]:\n> https://evil.example\n\n![重置密码]"},
	{"blockquote continuation, used inside the blockquote", "> [重置密码]:\n> https://evil.example\n>\n> [重置密码]"},
}

// TestBreakLinkReferenceDefinitionsClosesEveryKnownDefinition is the guard's
// real assertion, and it is made against a CommonMark parser rather than
// against our own idea of one: for every attack shape, the guarded text must
// define no link that goes to the attacker's host.
//
// The unguarded half is not decoration. It pins that each entry really is a
// definition, so a later edit that makes the guard stop firing cannot pass by
// quietly shrinking what the corpus tests.
func TestBreakLinkReferenceDefinitionsClosesEveryKnownDefinition(t *testing.T) {
	for _, tc := range linkReferenceAttacks {
		t.Run(tc.name, func(t *testing.T) {
			if !hasDestinationTo(markdownDestinations(tc.body), "evil.example") {
				t.Fatalf("unguarded, %q resolves no link to evil.example — this case proves nothing", tc.body)
			}
			guarded := breakMemberLinks(tc.body)
			if dests := markdownDestinations(guarded); hasDestinationTo(dests, "evil.example") {
				t.Fatalf("a member-defined link survived the guard: %q → %q resolves %v",
					tc.body, guarded, dests)
			}
		})
	}
}

// TestBreakLinkReferenceDefinitionsLeavesProseAlone is the other half of the
// contract, and the reason the rule is not "always break ]:". Every string
// here must come back byte-identical: "[Bug]: 登录失败" is a title real people
// write, and mangling it to buy safety we can get another way is the same
// trade the backslash escaping already lost.
func TestBreakLinkReferenceDefinitionsLeavesProseAlone(t *testing.T) {
	for _, in := range []string{
		"[Bug]: 登录失败",
		"[Bug]: 登录失败\n\n[Bug] 还在复现",
		"[WIP]: 明天再看",
		"[文件]: report.pdf",
		"[页面]: /inbox",
		"[Bug]: 见 https://tracker.example/1", // destination is 见, not the URL
		// Relative destinations that happen to hold a "\" or an "&". Neither
		// character spells an escape or a character reference here, so there
		// is nothing to decode and nothing to break.
		"[Owner]: R&D",
		`[Regex]: \d+`,
		`[Path]: docs\setup`,
		"[Query]: foo&bar",
		"[Note]: see A&B for details",
		"[Bug] 登录失败",
		"修复 (见 issue 12)",
		"见 [文档]: https://wiki.example/x", // not at block position
		"**[提及你] t**",
		"[重置密码]:\n\nhttps://evil.example", // blank line: no definition
		"[ ]: https://evil.example",       // no label content
		"[Bug]:",
		"没有方括号的一行",
	} {
		if got := breakMemberLinks(in); got != in {
			t.Fatalf("member text was altered:\n got %q\nwant %q (unchanged)", got, in)
		}
	}
}

// TestBreakLinkReferenceDefinitionsInsertsOneSpace pins where the break lands
// and that it is the only edit — one space, between the "]" and the ":".
func TestBreakLinkReferenceDefinitionsInsertsOneSpace(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "[重置密码]: https://evil.example", "[重置密码] : https://evil.example"},
		{"no space after the colon", "[a]:https://x", "[a] :https://x"},
		{"next line destination", "[a]:\nhttps://x", "[a] :\nhttps://x"},
		{"blockquote", "> [a]: https://x", "> [a] : https://x"},
		{"list item", "- [a]: https://x", "- [a] : https://x"},
		{"two definitions", "[a]: https://x\n[b]: https://y", "[a] : https://x\n[b] : https://y"},
		{"only the definition on a mixed body", "看这里\n\n[a]: https://x\n\n[a]", "看这里\n\n[a] : https://x\n\n[a]"},
		// Over-fires, kept visible on purpose. Both cost one space on a line
		// that already carries a URL, and both are the safe side of a
		// renderer we cannot read: CommonMark would call the first indented
		// code and the second a paragraph, but this one is not CommonMark.
		{"four space indent is not a definition in commonmark", "    [a]: https://x", "    [a] : https://x"},
		{"trailing prose is not a definition in commonmark", "[a]: https://x 请点击", "[a] : https://x 请点击"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := breakLinkReferenceDefinitions(tc.in)
			if got != tc.want {
				t.Fatalf("breakLinkReferenceDefinitions(%q):\n got %q\nwant %q", tc.in, got, tc.want)
			}
			if strings.Contains(got, `\`) && !strings.Contains(tc.in, `\`) {
				t.Fatalf("emitted a backslash for %q: %q — WeCom reads \"\\[\" as a math delimiter", tc.in, got)
			}
		})
	}
}

// scaffoldPrefixes enumerates block scaffolding by composing, up to depth
// pieces deep, everything a definition line can be indented and nested with.
// It is the alphabet containerPrefixBefore reads, so every prefix it accepts is
// somewhere a member can put a definition — which makes it the right thing to
// sweep both halves of the rule over.
func scaffoldPrefixes(depth int) []string {
	pieces := []string{" ", "   ", "\t", ">", "> ", ">\t", "- ", "* ", "1. "}
	var out []string
	seen := map[string]bool{}
	var build func(string, int)
	build = func(cur string, d int) {
		if !seen[cur] {
			seen[cur] = true
			out = append(out, cur)
		}
		if d == 0 {
			return
		}
		for _, p := range pieces {
			build(cur+p, d-1)
		}
	}
	build("", depth)
	return out
}

// TestContainerPrefixHalvesAgree is the test for the mistake rather than for
// its symptoms, and it is the one that would have caught the blockquote
// continuation before it shipped.
//
// Breaking a definition is one rule decided in two places. containerPrefixBefore
// says what scaffolding may precede the label; looksLikeLinkDestination scans
// what follows the colon. CommonMark carries a blockquote's marker onto every
// line the quote holds, so the moment the first half accepts a ">" the second
// half has to expect it back on a destination that sits on the next line. Teach
// one and not the other and the definition survives untouched — which is
// exactly what happened.
//
// The expected marker count is taken here from the prefix text, not read off
// containerPrefixBefore's answer, so the halves are checked against the markdown
// rule instead of against each other's idea of it.
func TestContainerPrefixHalvesAgree(t *testing.T) {
	for _, prefix := range scaffoldPrefixes(3) {
		container, ok := containerPrefixBefore(prefix+"[重置密码]", len(prefix))
		if !ok {
			continue
		}
		quotes := strings.Count(prefix, ">")
		if container.quotes != quotes {
			t.Fatalf("prefix %q opens %d blockquotes, containerPrefixBefore reports %d",
				prefix, quotes, container.quotes)
		}
		// Every nesting from a lazy continuation carrying no marker at all up
		// to the definition's own depth is a line the parser still reads as
		// this definition's destination.
		for n := 0; n <= quotes; n++ {
			for _, marker := range []string{">", "> ", ">\t", " > ", "  >"} {
				rest := "\n" + strings.Repeat(marker, n) + "https://evil.example"
				if !looksLikeLinkDestination(rest, container) {
					t.Fatalf("prefix %q may hold a definition, but the destination scan misses %q —\n"+
						"the two halves disagree about what a block container is", prefix, rest)
				}
			}
		}
		// And no deeper. A line nested further than the definition's own block
		// starts a new one rather than continuing it, so stepping over that
		// marker would be swallowing a character that happened to be in the
		// way rather than modelling the container.
		deeper := "\n" + strings.Repeat("> ", quotes+1) + "https://evil.example"
		if looksLikeLinkDestination(deeper, container) {
			t.Fatalf("prefix %q opens %d blockquotes but the destination scan stepped over %d in %q",
				prefix, quotes, quotes+1, deeper)
		}
	}
}

// TestBreakLinkReferenceDefinitionsAcrossBlockScaffolding sweeps scaffolding on
// the definition line against scaffolding on the destination line and asserts
// against a real parser rather than against a list of shapes someone thought
// of: whatever combination genuinely defines a link to the attacker's host must
// come back from the guard defining none.
//
// Self-checking the way linkReferenceAttacks is. A combination is only asserted
// on after it has been shown to resolve unguarded, and the number that do is
// pinned, so the sweep cannot come to pass by testing nothing.
func TestBreakLinkReferenceDefinitionsAcrossBlockScaffolding(t *testing.T) {
	prefixes := scaffoldPrefixes(2)
	destinations := []string{
		"https://evil.example",
		"//evil.example/x",
		"<https://evil.example>",
		`https\://evil.example`,
	}
	live := 0
	for _, def := range prefixes {
		if _, ok := containerPrefixBefore(def+"[", len(def)); !ok {
			continue
		}
		for _, cont := range prefixes {
			for _, dest := range destinations {
				for _, nl := range []string{"\n", "\r\n"} {
					body := def + "[重置密码]:" + nl + cont + dest + nl + nl + "[重置密码]"
					if !sendsReaderTo(markdownDestinations(body), "evil.example") {
						continue
					}
					live++
					guarded := breakMemberLinks(body)
					if sendsReaderTo(markdownDestinations(guarded), "evil.example") {
						t.Fatalf("definition behind %q with its destination behind %q survived the guard:\n%q → %q",
							def, cont, body, guarded)
					}
				}
			}
		}
	}
	if live < 4000 {
		t.Fatalf("only %d swept combinations defined a link at all — the sweep has stopped testing anything", live)
	}
}

// TestBreakMemberLinksIsIdempotent: the same text can reach more than one
// builder, and the length budget is computed once, so a mechanism that kept
// inserting spaces would drift past the cap.
func TestBreakMemberLinksIsIdempotent(t *testing.T) {
	inputs := []string{
		"[a](b)", "](](", "[Bug] 登录失败", "![i](u)", "[Bug]: 登录失败",
		// Both breaks firing on one string: neither can produce the pattern
		// the other looks for, so the pair has to settle after one pass.
		"[点这里](https://evil.example)\n\n[重置密码]: https://evil.example\n\n[重置密码]",
	}
	for _, tc := range linkReferenceAttacks {
		inputs = append(inputs, tc.body)
	}
	for _, in := range inputs {
		once := breakMemberLinks(in)
		if twice := breakMemberLinks(once); twice != once {
			t.Fatalf("second pass changed %q: %q → %q", in, once, twice)
		}
	}
}

// markdownDestinations parses md as CommonMark and returns the destination of
// every link and image — that is, every place a reader can be sent by clicking
// text the member chose the wording of.
//
// Autolinks are left out deliberately, and the distinction is the whole point
// of the guard. "<https://evil.example>" renders as a link whose visible text
// is its own URL, so it cannot lie about where it goes; the same is true of a
// bare URL the client linkifies. What the card must never carry is a label
// reading 重置密码 that quietly resolves somewhere else, and that is a Link or
// an Image, never an AutoLink.
func markdownDestinations(md string) []string {
	source := []byte(md)
	doc := goldmark.New().Parser().Parse(text.NewReader(source))
	var out []string
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch node := n.(type) {
		case *ast.Link:
			out = append(out, string(node.Destination))
		case *ast.Image:
			out = append(out, string(node.Destination))
		}
		return ast.WalkContinue, nil
	})
	return out
}

// hasDestinationTo matches case-insensitively: a scheme and host in capitals
// resolve to the same server.
func hasDestinationTo(dests []string, host string) bool {
	for _, d := range dests {
		if strings.Contains(strings.ToLower(d), strings.ToLower(host)) {
			return true
		}
	}
	return false
}

// sendsReaderTo is the stricter reading of the same question, and the sweep
// above needs it: a destination has to actually aim at host, not merely spell
// its name. A tab before a ">" on the destination line makes goldmark read
// ">https://evil.example" as the destination — it contains the name and goes
// nowhere, because a leading ">" makes it relative and the client resolves it
// against its own base. Relative destinations are let through on purpose, so a
// sweep that counted those would be demanding breaks the guard's contract
// promises not to make.
func sendsReaderTo(dests []string, host string) bool {
	for _, d := range dests {
		u, err := url.Parse(strings.TrimSpace(d))
		if err != nil || u.Host == "" {
			continue
		}
		if strings.EqualFold(u.Hostname(), host) {
			return true
		}
	}
	return false
}
