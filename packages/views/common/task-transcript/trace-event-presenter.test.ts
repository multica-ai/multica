import { describe, expect, it } from "vitest";
import {
  collapseDiffContext,
  parseUnifiedDiff,
  stripShellWrapper,
  traceEventCopyText,
  traceEventDefaultExpanded,
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

describe("traceEventDefaultExpanded", () => {
  const agent = { type: "text", content: "hello" };
  const error = { type: "error", content: "boom" };
  const thinking = { type: "thinking", content: "hmm" };
  const tool = { type: "tool_use", tool: "Bash", input: { command: "ls" } };

  it("smart: agent and error read without a click, process noise stays folded", () => {
    expect(traceEventDefaultExpanded(agent, "smart")).toBe(true);
    expect(traceEventDefaultExpanded(error, "smart")).toBe(true);
    expect(traceEventDefaultExpanded(thinking, "smart")).toBe(false);
    expect(traceEventDefaultExpanded(tool, "smart")).toBe(false);
  });

  it("expanded/collapsed override the hierarchy wholesale", () => {
    expect(traceEventDefaultExpanded(thinking, "expanded")).toBe(true);
    expect(traceEventDefaultExpanded(agent, "collapsed")).toBe(false);
  });

  it("a row without detail never expands", () => {
    expect(traceEventDefaultExpanded({ type: "text" }, "expanded")).toBe(false);
    expect(traceEventHasDetail({ type: "tool_use", input: {} })).toBe(false);
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
    expect(detail.files).toHaveLength(1);
    expect(detail.files[0]?.path).toBe("/repo/src/counter.rs");
    expect(detail.files[0]?.lines).toEqual([
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
    expect(detail.files[0]?.lines).toEqual([{ kind: "add", text: "added" }]);
  });

  it("keeps a deletion visible when new_string is empty", () => {
    const detail = traceEventDetail({
      type: "tool_use",
      input: { file_path: "/f", old_string: "gone", new_string: "" },
    });
    expect(detail.kind).toBe("diff");
    if (detail.kind !== "diff") return;
    expect(detail.files[0]?.lines).toEqual([{ kind: "remove", text: "gone" }]);
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

describe("parseUnifiedDiff", () => {
  // Fixture captured from codex-cli 0.145.0: a `fileChange` item hands over a
  // ready-made unified diff rather than before/after text.
  const codexDiff =
    '@@ -1,2 +1,2 @@\n def greet(name):\n-    return f"Hello, {name}!"\n+    return f"Hey, {name}!"\n';

  it("reads a hunk into rows without re-diffing anything", () => {
    expect(parseUnifiedDiff(codexDiff)).toEqual([
      { kind: "context", text: "def greet(name):" },
      { kind: "remove", text: '    return f"Hello, {name}!"' },
      { kind: "add", text: '    return f"Hey, {name}!"' },
    ]);
  });

  it("marks a hunk boundary as a gap, but not before the first hunk", () => {
    const kinds = parseUnifiedDiff("@@ -1 +1 @@\n-a\n+b\n@@ -9 +9 @@\n-y\n+z\n").map(
      (l) => l.kind,
    );
    expect(kinds).toEqual(["remove", "add", "gap", "remove", "add"]);
  });

  it("drops file headers and the no-newline marker", () => {
    const out = parseUnifiedDiff("--- a/f\n+++ b/f\n@@ -1 +1 @@\n-a\n+b\n\\ No newline at end of file\n");
    expect(out).toEqual([
      { kind: "remove", text: "a" },
      { kind: "add", text: "b" },
    ]);
  });

  it("keeps an unmarked body line as context rather than dropping evidence", () => {
    expect(parseUnifiedDiff("@@ -1 +1 @@\nunmarked\n")).toEqual([
      { kind: "context", text: "unmarked" },
    ]);
  });

  it("survives an empty or marker-only diff", () => {
    expect(parseUnifiedDiff("")).toEqual([]);
    expect(parseUnifiedDiff("@@ -0,0 +0,0 @@\n")).toEqual([]);
  });
});

describe("traceEventDetail — patch-applying providers", () => {
  const change = (path: string, diff: string, movePath?: string) => ({
    path,
    diff,
    ...(movePath ? { move_path: movePath } : {}),
  });

  it("renders a patch_apply payload as one diff per touched file", () => {
    const detail = traceEventDetail({
      type: "tool_use",
      tool: "patch_apply",
      input: {
        changes: [
          change("/repo/a.py", "@@ -1 +1 @@\n-old\n+new\n"),
          change("/repo/b.py", "@@ -1 +1 @@\n-x\n+y\n"),
        ],
      },
    });
    expect(detail.kind).toBe("diff");
    if (detail.kind !== "diff") return;
    expect(detail.files.map((f) => f.path)).toEqual(["/repo/a.py", "/repo/b.py"]);
    expect(detail.files[0]?.lines).toEqual([
      { kind: "remove", text: "old" },
      { kind: "add", text: "new" },
    ]);
  });

  it("carries a rename target through", () => {
    const detail = traceEventDetail({
      type: "tool_use",
      input: { changes: [change("/repo/old.py", "@@ -1 +1 @@\n-a\n+b\n", "/repo/new.py")] },
    });
    expect(detail.kind).toBe("diff");
    if (detail.kind !== "diff") return;
    expect(detail.files[0]?.movePath).toBe("/repo/new.py");
  });

  it("skips entries with no usable path or diff, and falls back when none are usable", () => {
    const partial = traceEventDetail({
      type: "tool_use",
      input: { changes: [{ path: "/only-path" }, change("/repo/ok.py", "@@ -1 +1 @@\n+ok\n")] },
    });
    expect(partial.kind).toBe("diff");
    if (partial.kind !== "diff") return;
    expect(partial.files.map((f) => f.path)).toEqual(["/repo/ok.py"]);

    // Nothing usable: the JSON view is more honest than an empty diff.
    const none = traceEventDetail({ type: "tool_use", input: { changes: [{ path: "/p" }] } });
    expect(none.kind).toBe("text");
  });
});

describe("traceToolArgSummary — patch payloads", () => {
  it("names the patched file, so the collapsed row is not blank", () => {
    expect(
      traceToolArgSummary({ changes: [{ path: "/repo/src/greet.py", diff: "@@ -1 +1 @@\n-a\n+b\n" }] }),
    ).toBe(".../src/greet.py");
  });

  it("counts the rest when a patch touches several files", () => {
    expect(
      traceToolArgSummary({
        changes: [{ path: "/repo/a.py" }, { path: "/repo/b.py" }, { path: "/repo/c.py" }],
      }),
    ).toBe("/repo/a.py +2 more");
  });

  it("falls through to the string ladder when changes carries no path", () => {
    expect(traceToolArgSummary({ changes: [{ diff: "@@" }], command: "ls" })).toBe("ls");
    expect(traceToolArgSummary({ changes: "not-an-array", command: "ls" })).toBe("ls");
  });
});
