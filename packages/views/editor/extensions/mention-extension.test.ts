import { describe, it, expect } from "vitest";
import { BaseMentionExtension } from "./mention-extension";

const tokenizer = BaseMentionExtension.config.markdownTokenizer!;

// The tiptap MarkdownTokenizer/renderMarkdown types have broad signatures
// (multi-arg overloads). Our extension always provides single-argument
// implementations, so cast for test convenience.
const startFn = tokenizer.start as (src: string) => number;
const tokenizeFn = tokenizer.tokenize as (
  src: string,
) => { type: string; raw: string; attributes: Record<string, string> } | undefined;
const renderMarkdown = BaseMentionExtension.config.renderMarkdown as (
  node: { attrs: Record<string, string> },
) => string;

const JAVA_STACKTRACE_SOURCE_MARKER =
  "        at org.springframework.web.servlet.FrameworkServlet.processRequest(FrameworkServlet.java:1006) \\~\\[spring-webmvc-5.3.39.jar!/:5.3.39\\]\n";

function tokenize(src: string) {
  const start = startFn(src);
  if (start === -1) return undefined;
  return tokenizeFn(src.slice(start));
}

describe("mention tokenizer", () => {
  it("parses a plain mention", () => {
    const token = tokenize("[@Alice](mention://member/aaa-bbb)");
    expect(token).toBeDefined();
    expect(token!.attributes.label).toBe("Alice");
    expect(token!.attributes.type).toBe("member");
    expect(token!.attributes.id).toBe("aaa-bbb");
  });

  it("parses a mention with escaped brackets (round-trip from renderMarkdown)", () => {
    // renderMarkdown escapes brackets: David[TF] → David\[TF\]
    const md = renderMarkdown({
      attrs: { id: "aaa-bbb", label: "David[TF]", type: "agent" },
    });
    expect(md).toBe("[@David\\[TF\\]](mention://agent/aaa-bbb)");

    const token = tokenize(md);
    expect(token).toBeDefined();
    expect(token!.attributes.label).toBe("David[TF]");
    expect(token!.attributes.type).toBe("agent");
  });

  it.each(["A\\", "ends\\", "a\\]b", "f(x)", "back\\slash"])(
    "round-trips a label containing backslash/parens: %j",
    (label) => {
      // The linear tokenizer treats "\" as an escape lead, so renderMarkdown
      // must escape "\" too — otherwise a trailing "\" swallows the closing "]"
      // and the mention fails to parse back (regression guard for the
      // de-ambiguation fix).
      const md = renderMarkdown({
        attrs: { id: "aaa-bbb", label, type: "member" },
      });
      const token = tokenize(md);
      expect(token).toBeDefined();
      expect(token!.attributes.label).toBe(label);
      expect(token!.attributes.id).toBe("aaa-bbb");
    },
  );

  it("does not match an ordinary Markdown link before a mention", () => {
    const src =
      "Check [docs](https://example.com) - [@User](mention://agent/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa)";

    // start() must NOT land on the [docs] link at index 6
    const start = startFn(src);
    expect(start).toBeGreaterThan(6);

    // tokenize from the correct start position
    const token = tokenizeFn(src.slice(start));
    expect(token).toBeDefined();
    expect(token!.attributes.label).toBe("User");
    expect(token!.attributes.id).toBe("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa");
  });

  it("handles multiple ordinary links before a mention", () => {
    const src =
      "See [a](https://a.com) and [b](https://b.com) - [@Bot](mention://agent/abc-123)";
    const start = startFn(src);
    const token = tokenizeFn(src.slice(start));
    expect(token).toBeDefined();
    expect(token!.attributes.label).toBe("Bot");
  });

  it("round-trips an agent label with nested brackets", () => {
    const md = renderMarkdown({
      attrs: { id: "x-y-z", label: "Bot[v2][beta]", type: "agent" },
    });
    const token = tokenize(md);
    expect(token).toBeDefined();
    expect(token!.attributes.label).toBe("Bot[v2][beta]");
  });

  it("parses issue mentions without @ prefix", () => {
    const token = tokenize("[MUL-123](mention://issue/aaa-bbb)");
    expect(token).toBeDefined();
    expect(token!.attributes.label).toBe("MUL-123");
    expect(token!.attributes.type).toBe("issue");
  });

  it("finds an issue mention nested inside task list Markdown", () => {
    const token = tokenize("- [ ] [MUL-123](mention://issue/aaa-bbb)");
    expect(token).toBeDefined();
    expect(token!.attributes.label).toBe("MUL-123");
    expect(token!.attributes.type).toBe("issue");
    expect(token!.attributes.id).toBe("aaa-bbb");
  });

  it("does not start at escaped Java stacktrace source markers before a real mention", () => {
    const prefix = JAVA_STACKTRACE_SOURCE_MARKER.repeat(3);
    const src = `${prefix}[@Alice](mention://member/aaa-bbb)`;

    const start = startFn(src);
    expect(start).toBe(prefix.length);

    const token = tokenizeFn(src.slice(start));
    expect(token).toBeDefined();
    expect(token!.attributes.label).toBe("Alice");
    expect(token!.attributes.type).toBe("member");
    expect(token!.attributes.id).toBe("aaa-bbb");
  });

  it("keeps escaped Java stacktrace source marker detection fast when no mention exists", () => {
    const src = JAVA_STACKTRACE_SOURCE_MARKER.repeat(11);

    const t0 = performance.now();
    const start = startFn(src);
    const elapsed = performance.now() - t0;

    expect(start).toBe(-1);
    expect(elapsed).toBeLessThan(50);
  });

  it("rejects an unterminated mention with escape-pair runs in linear time", () => {
    // Each "\a" pair is ambiguous under (?:\\.|[^\]]) — the pre-fix regex
    // enumerates 2^28 backtrack paths (~10s) before failing. The disjoint
    // char class must fail fast instead.
    const src = `[@${"\\a".repeat(28)}](mention://member/abc`;

    const t0 = performance.now();
    const token = tokenizeFn(src);
    const elapsed = performance.now() - t0;

    expect(token).toBeUndefined();
    expect(elapsed).toBeLessThan(100);
  });
});
