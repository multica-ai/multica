import { describe, expect, it } from "vitest";
import {
  collapseDiffContext,
  parseUnifiedDiff,
  stripShellWrapper,
  traceEventCopyText,
  readImageResult,
  base64ByteLength,
  traceEventDetail,
  traceEventHasDetail,
  traceEventKind,
  traceEventLabel,
  traceEventSummary,
  traceToolArgSummary,
  unwrapToolOutput,
} from "./trace-event-presenter";

describe("traceEventKind / traceEventLabel", () => {
  it("maps the five persisted types and keeps unknown types as generic", () => {
    expect(traceEventKind({ type: "text" })).toBe("agent");
    expect(traceEventKind({ type: "thinking" })).toBe("thinking");
    expect(traceEventKind({ type: "tool_use" })).toBe("tool_use");
    expect(traceEventKind({ type: "tool_result" })).toBe("tool_result");
    expect(traceEventKind({ type: "error" })).toBe("error");
    expect(traceEventKind({ type: "provider_custom" })).toBe("generic");
  });

  it("shows provider-native tool names verbatim and surfaces raw unknown types", () => {
    expect(traceEventLabel({ type: "tool_use", tool: "exec_command" })).toBe("exec_command");
    expect(traceEventLabel({ type: "tool_result", tool: "patch_apply" })).toBe("patch_apply");
    expect(traceEventLabel({ type: "tool_use" })).toBe("Tool");
    expect(traceEventLabel({ type: "provider_custom" })).toBe("provider_custom");
  });
});

describe("stripShellWrapper", () => {
  it("strips login-shell wrappers but keeps bare commands", () => {
    expect(stripShellWrapper("/bin/zsh -lc 'rm ./reply.md'")).toBe("rm ./reply.md");
    expect(stripShellWrapper('/bin/bash -c "git status"')).toBe("git status");
    expect(stripShellWrapper("sh -c 'ls -la'")).toBe("ls -la");
    expect(stripShellWrapper("pnpm test")).toBe("pnpm test");
    // Mismatched quotes are not a wrapper match.
    expect(stripShellWrapper("/bin/zsh -lc 'echo hi\"")).toBe("/bin/zsh -lc 'echo hi\"");
  });
});

describe("traceToolArgSummary", () => {
  it("prefers query, then paths (shortened), then command with wrapper stripped", () => {
    expect(traceToolArgSummary({ query: "flaky tests", command: "x" })).toBe("flaky tests");
    expect(traceToolArgSummary({ file_path: "/a/b/c/d/e.ts" })).toBe(".../d/e.ts");
    expect(traceToolArgSummary({ command: "/bin/zsh -lc 'kubectl get pods -n prd'" })).toBe(
      "kubectl get pods -n prd",
    );
  });

  it("falls back to the first short string value and tolerates empty input", () => {
    expect(traceToolArgSummary({ n: 3, note: "short value" })).toBe("short value");
    expect(traceToolArgSummary(undefined)).toBe("");
    expect(traceToolArgSummary({})).toBe("");
  });
});

describe("traceEventSummary", () => {
  it("takes the first non-empty line for agent text", () => {
    expect(traceEventSummary({ type: "text", content: "\n\nFirst line\nrest" })).toBe(
      "First line",
    );
  });

  it("collapses pretty-printed JSON output to a content preview, not a lone bracket", () => {
    const output = '[\n  {\n    "id": "694c",\n    "title": "x"\n  }\n]';
    expect(traceEventSummary({ type: "tool_result", output })).toBe(
      '[ { "id": "694c", "title": "x" } ]',
    );
  });

  it("unwraps a JSON-encoded result so the collapsed row shows no transport escaping", () => {
    expect(
      traceEventSummary({
        type: "tool_result",
        tool: "Bash",
        output: '"target/release/deps/acceptance\\n 0 page faults\\n 0 swaps"',
      }),
    ).toBe("target/release/deps/acceptance 0 page faults 0 swaps");
  });

  it("retains unknown events instead of dropping them", () => {
    expect(traceEventSummary({ type: "custom", content: "payload" })).toBe("payload");
  });
});

describe("traceEventCopyText", () => {
  it("copies the full untruncated body, not the one-line summary", () => {
    const longOutput = "line 1\n".repeat(60);
    expect(traceEventCopyText({ type: "tool_result", tool: "Bash", output: longOutput })).toBe(
      `[Bash] ${longOutput}`,
    );
    expect(
      traceEventCopyText({ type: "tool_use", tool: "Bash", input: { command: "ls" } }),
    ).toBe('[Bash] {\n  "command": "ls"\n}');
    expect(traceEventCopyText({ type: "text", content: "full\nagent\nreply" })).toBe(
      "[Agent] full\nagent\nreply",
    );
  });

  it("copies a result as the terminal output it was, not its transport encoding", () => {
    expect(
      traceEventCopyText({ type: "tool_result", tool: "Bash", output: '"line 1\\nline 2"' }),
    ).toBe("[Bash] line 1\nline 2");
  });

  it("emits a bare label when the event has no body", () => {
    expect(traceEventCopyText({ type: "tool_use", tool: "Bash" })).toBe("[Bash]");
  });
});

describe("readImageResult", () => {
  it("reads a base64 image block so a screenshot renders as a picture", () => {
    const output = JSON.stringify([
      { type: "image", source: { type: "base64", media_type: "image/jpeg", data: "AAAA" } },
    ]);

    expect(readImageResult(output)).toEqual({ mediaType: "image/jpeg", base64: "AAAA" });
  });

  it("defaults the media type when the provider omits it", () => {
    const output = JSON.stringify({ type: "image", source: { type: "base64", data: "BBBB" } });

    expect(readImageResult(output)?.mediaType).toBe("image/png");
  });

  it("ignores ordinary output without paying for a parse", () => {
    expect(readImageResult('{"ok":true}')).toBeNull();
    expect(readImageResult("plain terminal output")).toBeNull();
    expect(readImageResult(undefined)).toBeNull();
    // Shaped like an image block but not valid JSON.
    expect(readImageResult('[{"type":"image","source":{"type":"base64"')).toBeNull();
  });

  it("reports the decoded size so the caption is not a base64 length", () => {
    expect(base64ByteLength("AAAA")).toBe(3);
    expect(base64ByteLength("AAA=")).toBe(2);
  });
});

describe("traceEventHasDetail", () => {
  it("is false for an empty body", () => {
    expect(traceEventHasDetail({ type: "tool_use", input: {} })).toBe(false);
    expect(traceEventHasDetail({ type: "text" })).toBe(false);
  });
});

describe("unwrapToolOutput", () => {
  it("decodes the one JSON string layer a result arrives wrapped in", () => {
    expect(unwrapToolOutput('"line one\\nline two"')).toBe("line one\nline two");
    expect(unwrapToolOutput('"{\\n  \\"id\\": 1\\n}"')).toBe('{\n  "id": 1\n}');
  });

  it("leaves anything that is not a wrapped string untouched", () => {
    expect(unwrapToolOutput("plain text")).toBe("plain text");
    expect(unwrapToolOutput('{"id": 1}')).toBe('{"id": 1}');
    // A quoted-looking body that is not valid JSON must survive verbatim.
    expect(unwrapToolOutput('"unterminated')).toBe('"unterminated');
    expect(unwrapToolOutput("")).toBe("");
  });

  it("decodes only one layer, so a JSON document stays a document", () => {
    expect(unwrapToolOutput('"[1, 2]"')).toBe("[1, 2]");
  });
});

describe("traceEventDetail", () => {
  it("renders a replacement edit as a diff, keyed off input shape not tool name", () => {
    // Provider-native names differ (Edit, patch_apply, str_replace...), so the
    // shape of the input is what identifies an edit.
    const detail = traceEventDetail({
      type: "tool_use",
      tool: "patch_apply",
      input: {
        file_path: "/repo/src/counter.rs",
        old_string: "let a = 1;\nlet b = 2;",
        new_string: "let a = 1;\nlet b = 3;\nlet c = 4;",
      },
    });
    expect(detail.kind).toBe("diff");
    if (detail.kind !== "diff") return;
    expect(detail.path).toBe("/repo/src/counter.rs");
    expect(detail.lines).toEqual([
      { kind: "context", text: "let a = 1;" },
      { kind: "remove", text: "let b = 2;" },
      { kind: "add", text: "let b = 3;" },
      { kind: "add", text: "let c = 4;" },
    ]);
  });

  it("renders a whole-file write as plain content, not an all-additions diff", () => {
    const detail = traceEventDetail({
      type: "tool_use",
      tool: "Write",
      input: { file_path: "/repo/new.txt", content: "alpha\nbeta" },
    });
    expect(detail).toEqual({
      kind: "file",
      path: "/repo/new.txt",
      text: "alpha\nbeta",
      lineCount: 2,
    });
  });

  it("still diffs an insertion whose old side is empty", () => {
    // `content` marks a whole-file write; an empty old_string is an insertion
    // into a file that already exists, so the diff gutter still carries meaning.
    const detail = traceEventDetail({
      type: "tool_use",
      input: { file_path: "/f", old_string: "", new_string: "added" },
    });
    expect(detail.kind).toBe("diff");
    if (detail.kind !== "diff") return;
    expect(detail.lines).toEqual([{ kind: "add", text: "added" }]);
  });

  it("keeps a deletion visible when new_string is empty", () => {
    const detail = traceEventDetail({
      type: "tool_use",
      input: { file_path: "/f", old_string: "gone", new_string: "" },
    });
    expect(detail.kind).toBe("diff");
    if (detail.kind !== "diff") return;
    expect(detail.lines).toEqual([{ kind: "remove", text: "gone" }]);
  });

  it("falls back to pretty JSON for a tool call that is not an edit", () => {
    const detail = traceEventDetail({
      type: "tool_use",
      tool: "Bash",
      input: { command: "ls -la" },
    });
    expect(detail.kind).toBe("text");
    if (detail.kind !== "text") return;
    expect(detail.text).toBe('{\n  "command": "ls -la"\n}');
  });

  it("unwraps a tool result so it reads as the terminal output it was", () => {
    const detail = traceEventDetail({
      type: "tool_result",
      tool: "Bash",
      output: '"total 0\\ndrwxr-xr-x  2 user  staff"',
    });
    expect(detail).toEqual({ kind: "text", text: "total 0\ndrwxr-xr-x  2 user  staff" });
  });

  it("uses content for prose events and never throws on an empty event", () => {
    expect(traceEventDetail({ type: "text", content: "hello" })).toEqual({
      kind: "text",
      text: "hello",
    });
    expect(traceEventDetail({ type: "tool_use" })).toEqual({ kind: "text", text: "" });
  });
});

describe("collapseDiffContext", () => {
  const ctx = (n: number) => Array.from({ length: n }, (_, i) => ctxLine(`c${i}`));
  function ctxLine(text: string) {
    return { kind: "context" as const, text };
  }

  it("collapses a long unchanged stretch between two changes", () => {
    const lines = [
      { kind: "remove" as const, text: "old" },
      ...ctx(10),
      { kind: "add" as const, text: "new" },
    ];
    const out = collapseDiffContext(lines, 2);
    expect(out.map((l) => l.kind)).toEqual([
      "remove",
      "context",
      "context",
      "gap",
      "context",
      "context",
      "add",
    ]);
    expect(out[3]).toEqual({ kind: "gap", text: "", hidden: 6 });
  });

  it("drops leading and trailing context entirely — nothing faces a change there", () => {
    const out = collapseDiffContext([...ctx(8), { kind: "add", text: "x" }, ...ctx(8)], 2);
    expect(out.map((l) => l.kind)).toEqual(["gap", "context", "context", "add", "context", "context", "gap"]);
    expect(out[0]?.hidden).toBe(6);
  });

  it("leaves a run alone when collapsing would not save a line", () => {
    const lines = [{ kind: "remove" as const, text: "a" }, ...ctx(4), { kind: "add" as const, text: "b" }];
    expect(collapseDiffContext(lines, 2)).toEqual(lines);
  });

  it("is a no-op for a diff with no context at all", () => {
    const lines = [
      { kind: "remove" as const, text: "a" },
      { kind: "add" as const, text: "b" },
    ];
    expect(collapseDiffContext(lines)).toEqual(lines);
  });
});

// --- Codex multi-file patch payload (GH #6157) -----------------------------
//
// The Codex adapter records a file edit as
// `{ changes: [{ path, kind, diff?, content?, move_path? }], truncated? }`.
// Before this the presenter only understood a top-level file_path plus
// old_string/new_string, so a Codex edit fell through to pretty JSON.

describe("parseUnifiedDiff", () => {
  it("maps a ready-made unified diff onto diff rows", () => {
    // Codex hands over a finished diff, so it is parsed rather than recomputed.
    expect(parseUnifiedDiff("@@ -1,3 +1,3 @@\n ctx\n-old\n+new\n")).toEqual([
      { kind: "gap", text: "@@ -1,3 +1,3 @@" },
      { kind: "context", text: "ctx" },
      { kind: "remove", text: "old" },
      { kind: "add", text: "new" },
    ]);
  });

  it("drops file headers ahead of the first hunk", () => {
    const lines = parseUnifiedDiff(
      "diff --git a/x b/x\nindex 111..222 100644\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n-a\n+b\n\\ No newline at end of file\n",
    );
    expect(lines).toEqual([
      { kind: "gap", text: "@@ -1 +1 @@" },
      { kind: "remove", text: "a" },
      { kind: "add", text: "b" },
    ]);
  });

  // Inside a hunk, "---"/"+++" are ordinary changed lines whose content starts
  // with a dash or plus — a Markdown rule, a nested patch, a comment banner.
  // Treating them as file headers anywhere silently deleted real content.
  it("keeps header-like content lines once inside a hunk", () => {
    expect(parseUnifiedDiff("@@ -1 +1 @@\n--- old markdown\n+++ new markdown\n")).toEqual([
      { kind: "gap", text: "@@ -1 +1 @@" },
      { kind: "remove", text: "-- old markdown" },
      { kind: "add", text: "++ new markdown" },
    ]);
  });

  it("keeps a removed line that is exactly a Markdown rule", () => {
    expect(parseUnifiedDiff("@@ -1,2 +1,1 @@\n---\n ok\n")).toEqual([
      { kind: "gap", text: "@@ -1,2 +1,1 @@" },
      { kind: "remove", text: "--" },
      { kind: "context", text: "ok" },
    ]);
  });

  it("keeps an empty context line and does not invent a trailing one", () => {
    // " " is a blank unchanged line; the phantom element left by the trailing
    // newline is not.
    expect(parseUnifiedDiff("@@ -1,2 +1,2 @@\n \n+x\n")).toEqual([
      { kind: "gap", text: "@@ -1,2 +1,2 @@" },
      { kind: "context", text: "" },
      { kind: "add", text: "x" },
    ]);
  });
});

describe("traceEventDetail — Codex changes[]", () => {
  it("renders a multi-file patch with one section per file", () => {
    const detail = traceEventDetail({
      type: "tool_use",
      tool: "patch_apply",
      input: {
        changes: [
          { path: "src/a.go", kind: "update", diff: "@@ -1 +1 @@\n-a\n+b\n" },
          { path: "src/new.go", kind: "add", content: "package main\n" },
        ],
      },
    });
    expect(detail.kind).toBe("patch");
    if (detail.kind !== "patch") return;
    expect(detail.truncated).toBe(false);
    expect(detail.files).toHaveLength(2);

    const [update, added] = detail.files;
    expect(update?.path).toBe("src/a.go");
    expect(update?.changeKind).toBe("update");
    expect(update?.body).toEqual({
      kind: "diff",
      lines: [
        { kind: "gap", text: "@@ -1 +1 @@" },
        { kind: "remove", text: "a" },
        { kind: "add", text: "b" },
      ],
    });

    // An added file has no before side, matching the whole-file write surface.
    expect(added?.path).toBe("src/new.go");
    expect(added?.body).toEqual({ kind: "file", text: "package main\n", lineCount: 2 });
  });

  it("renders a deletion as all-removals rather than a green whole-file write", () => {
    // The legacy protocol reports a delete as the outgoing file's content; a
    // "+N" gutter would state the opposite of what happened.
    const detail = traceEventDetail({
      type: "tool_use",
      tool: "patch_apply",
      input: { changes: [{ path: "gone.txt", kind: "delete", content: "one\ntwo" }] },
    });
    expect(detail.kind).toBe("patch");
    if (detail.kind !== "patch") return;
    expect(detail.files[0]?.body).toEqual({
      kind: "diff",
      lines: [
        { kind: "remove", text: "one" },
        { kind: "remove", text: "two" },
      ],
    });
  });

  it("surfaces a rename destination", () => {
    const detail = traceEventDetail({
      type: "tool_use",
      tool: "patch_apply",
      input: {
        changes: [
          { path: "old.go", kind: "update", move_path: "new.go", diff: "@@ -1 +1 @@\n-x\n+y\n" },
        ],
      },
    });
    if (detail.kind !== "patch") return expect(detail.kind).toBe("patch");
    expect(detail.files[0]?.movePath).toBe("new.go");
  });

  it("keeps the path when the body was dropped by the size budget", () => {
    const detail = traceEventDetail({
      type: "tool_use",
      tool: "patch_apply",
      input: {
        changes: [{ path: "big.txt", kind: "add", truncated: true }],
        truncated: true,
      },
    });
    expect(detail.kind).toBe("patch");
    if (detail.kind !== "patch") return;
    expect(detail.truncated).toBe(true);
    expect(detail.files[0]).toEqual({
      path: "big.txt",
      changeKind: "add",
      truncated: true,
      body: { kind: "none" },
    });
  });

  it("renders an empty added file as an empty body, not as a missing one", () => {
    const detail = traceEventDetail({
      type: "tool_use",
      tool: "patch_apply",
      input: { changes: [{ path: "empty.txt", kind: "add", content: "" }] },
    });
    if (detail.kind !== "patch") return expect(detail.kind).toBe("patch");
    expect(detail.files[0]?.body).toEqual({ kind: "file", text: "", lineCount: 0 });
  });

  it("falls back to pretty JSON for an unrecognised shape", () => {
    // Forward compatibility: a payload this presenter does not understand must
    // still be readable rather than rendering as an empty patch.
    const detail = traceEventDetail({
      type: "tool_use",
      tool: "patch_apply",
      input: { changes: "not-an-array" },
    });
    expect(detail.kind).toBe("text");
    if (detail.kind !== "text") return;
    expect(detail.text).toContain("not-an-array");
  });

  it("falls back to pretty JSON when no change carries a path", () => {
    const detail = traceEventDetail({
      type: "tool_use",
      tool: "patch_apply",
      input: { changes: [42, null, { kind: "add" }] },
    });
    expect(detail.kind).toBe("text");
  });
});

describe("traceToolArgSummary / traceEventHasDetail — Codex changes[]", () => {
  it("summarises a single-file patch as its path", () => {
    expect(
      traceToolArgSummary({ changes: [{ path: "src/a.go", kind: "update", diff: "@@\n+x" }] }),
    ).toBe("src/a.go");
  });

  it("summarises a multi-file patch as the first path plus a count", () => {
    expect(
      traceToolArgSummary({
        changes: [
          { path: "src/a.go", kind: "update", diff: "@@\n+x" },
          { path: "src/b.go", kind: "add", content: "y" },
          { path: "src/c.go", kind: "add", content: "z" },
        ],
      }),
    ).toBe("src/a.go +2 more");
  });

  it("shortens a deep path in the summary", () => {
    expect(
      traceToolArgSummary({ changes: [{ path: "a/b/c/d/e.go", kind: "add", content: "x" }] }),
    ).toBe(".../d/e.go");
  });

  // The presenter stays i18n-free, so the caller injects the phrasing; the
  // English form is only the fallback.
  it("uses injected phrasing for the multi-file count", () => {
    expect(
      traceToolArgSummary(
        {
          changes: [
            { path: "src/a.go", kind: "update", diff: "@@\n+x" },
            { path: "src/b.go", kind: "add", content: "y" },
          ],
        },
        { morePaths: (path, extraCount) => `${path}，另有 ${extraCount} 个文件` },
      ),
    ).toBe("src/a.go，另有 1 个文件");
  });

  // The injected number counts files *beyond* the named one. Phrasing it as a
  // total under-reports by one, which is easy to miss in English ("+2 more")
  // and obvious in a language that states the total outright.
  it("passes the count of additional files, not the total", () => {
    const seen: number[] = [];
    traceToolArgSummary(
      {
        changes: [
          { path: "a.go", kind: "add", content: "x" },
          { path: "b.go", kind: "add", content: "y" },
          { path: "c.go", kind: "add", content: "z" },
        ],
      },
      {
        morePaths: (path, extraCount) => {
          seen.push(extraCount);
          return path;
        },
      },
    );
    expect(seen).toEqual([2]);
  });

  it("does not consult the injected phrasing for a single file", () => {
    expect(
      traceToolArgSummary(
        { changes: [{ path: "only.go", kind: "add", content: "x" }] },
        {
          morePaths: () => {
            throw new Error("must not be called for a single-file patch");
          },
        },
      ),
    ).toBe("only.go");
  });

  it("makes a patch row expandable — the bug was two blank unexpandable rows", () => {
    expect(
      traceEventHasDetail({
        type: "tool_use",
        tool: "patch_apply",
        input: { changes: [{ path: "a.go", kind: "add", content: "x" }] },
      }),
    ).toBe(true);
    expect(
      traceEventHasDetail({
        type: "tool_result",
        tool: "patch_apply",
        output: "completed (1 file)",
      }),
    ).toBe(true);
    // The regression itself: no payload means no expandable detail.
    expect(traceEventHasDetail({ type: "tool_use", tool: "patch_apply", input: {} })).toBe(false);
    expect(traceEventHasDetail({ type: "tool_result", tool: "patch_apply", output: "" })).toBe(
      false,
    );
  });
});
