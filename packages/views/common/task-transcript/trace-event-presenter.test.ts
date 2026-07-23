import { describe, expect, it } from "vitest";
import {
  decodeToolResultOutput,
  traceEventDefaultCollapsed,
  traceEventEmphasis,
  traceEventFilterKey,
  traceEventHasDetail,
  traceEventIsMonospace,
  traceEventKind,
  traceEventLabel,
  traceEventSummary,
  traceToolArgSummary,
  shortenTracePath,
  type TraceEvent,
} from "./trace-event-presenter";

const ev = (e: Partial<TraceEvent> & { type: string }): TraceEvent => e;

describe("traceEventKind / label — faithful, unknown-safe", () => {
  it("maps known types", () => {
    expect(traceEventKind(ev({ type: "text" }))).toBe("agent");
    expect(traceEventKind(ev({ type: "thinking" }))).toBe("thinking");
    expect(traceEventKind(ev({ type: "tool_use" }))).toBe("tool_use");
    expect(traceEventKind(ev({ type: "tool_result" }))).toBe("tool_result");
    expect(traceEventKind(ev({ type: "error" }))).toBe("error");
  });

  it("treats an unknown type as generic and surfaces the raw type as its label", () => {
    expect(traceEventKind(ev({ type: "exec_command" }))).toBe("generic");
    expect(traceEventLabel(ev({ type: "exec_command" }))).toBe("exec_command");
    expect(traceEventLabel(ev({ type: "" }))).toBe("Event");
  });

  it("shows the provider-native tool name verbatim, never renamed", () => {
    expect(traceEventLabel(ev({ type: "tool_use", tool: "exec_command" }))).toBe("exec_command");
    expect(traceEventLabel(ev({ type: "tool_use", tool: "patch_apply" }))).toBe("patch_apply");
    expect(traceEventLabel(ev({ type: "tool_result", tool: "exec_command" }))).toBe("exec_command");
  });

  it("falls back to a neutral word when the tool name is missing (no invention)", () => {
    expect(traceEventLabel(ev({ type: "tool_use" }))).toBe("Tool");
    expect(traceEventLabel(ev({ type: "tool_result" }))).toBe("Result");
  });
});

describe("traceToolArgSummary — most informative one-liner", () => {
  it("prefers command, then path/query, then first short string", () => {
    expect(traceToolArgSummary({ command: "pnpm test" })).toBe("pnpm test");
    expect(traceToolArgSummary({ query: "how to X", command: "ls" })).toBe("how to X");
    expect(traceToolArgSummary({ file_path: "/a/b/c/d/e.ts" })).toBe(".../d/e.ts");
    expect(traceToolArgSummary({ misc: "only-value" })).toBe("only-value");
    expect(traceToolArgSummary(undefined)).toBe("");
  });

  it("clips a very long command", () => {
    const long = "x".repeat(300);
    expect(traceToolArgSummary({ command: long }).endsWith("...")).toBe(true);
    expect(traceToolArgSummary({ command: long }).length).toBeLessThan(130);
  });
});

describe("traceEventSummary", () => {
  it("text → first non-empty line; error → content; result → decoded first line", () => {
    expect(traceEventSummary(ev({ type: "text", content: "\n\nhello\nworld" }))).toBe("hello");
    expect(traceEventSummary(ev({ type: "error", content: "boom" }))).toBe("boom");
    expect(traceEventSummary(ev({ type: "tool_result", output: "line1\nline2" }))).toBe("line1");
    expect(traceEventSummary(ev({ type: "tool_use", tool: "exec_command", input: { command: "go build" } }))).toBe(
      "go build",
    );
  });

  it("tool_result summary decodes a double-encoded output to its real first line", () => {
    // A JSON-encoded string literal: outer quotes + escaped newline in storage.
    expect(traceEventSummary(ev({ type: "tool_result", output: '"line1\\nline2"' }))).toBe("line1");
  });

  it("a generic event still yields a summary from content/output/input", () => {
    expect(traceEventSummary(ev({ type: "weird", content: "c" }))).toBe("c");
    expect(traceEventSummary(ev({ type: "weird", output: "o" }))).toBe("o");
    expect(traceEventSummary(ev({ type: "weird", input: { a: 1 } }))).toContain("a");
  });
});

describe("hierarchy / collapse / monospace", () => {
  it("agent + error are primary; tool/thinking secondary; generic tertiary", () => {
    expect(traceEventEmphasis(ev({ type: "text" }))).toBe("primary");
    expect(traceEventEmphasis(ev({ type: "error" }))).toBe("primary");
    expect(traceEventEmphasis(ev({ type: "tool_use" }))).toBe("secondary");
    expect(traceEventEmphasis(ev({ type: "thinking" }))).toBe("secondary");
    expect(traceEventEmphasis(ev({ type: "mystery" }))).toBe("tertiary");
  });

  it("only thinking is collapsed by default", () => {
    expect(traceEventDefaultCollapsed(ev({ type: "thinking" }))).toBe(true);
    expect(traceEventDefaultCollapsed(ev({ type: "text" }))).toBe(false);
    expect(traceEventDefaultCollapsed(ev({ type: "tool_use" }))).toBe(false);
  });

  it("tool input/output render monospace; prose does not", () => {
    expect(traceEventIsMonospace(ev({ type: "tool_use" }))).toBe(true);
    expect(traceEventIsMonospace(ev({ type: "tool_result" }))).toBe(true);
    expect(traceEventIsMonospace(ev({ type: "text" }))).toBe(false);
  });
});

describe("traceEventHasDetail", () => {
  it("requires the relevant body to be present", () => {
    expect(traceEventHasDetail(ev({ type: "tool_use", input: { a: 1 } }))).toBe(true);
    expect(traceEventHasDetail(ev({ type: "tool_use", input: {} }))).toBe(false);
    expect(traceEventHasDetail(ev({ type: "tool_result", output: "x" }))).toBe(true);
    expect(traceEventHasDetail(ev({ type: "tool_result" }))).toBe(false);
    expect(traceEventHasDetail(ev({ type: "text", content: "hi" }))).toBe(true);
    expect(traceEventHasDetail(ev({ type: "text" }))).toBe(false);
  });
});

describe("traceEventFilterKey — one tool chip covers use + result", () => {
  it("keys tool events by tool and others by type", () => {
    expect(traceEventFilterKey(ev({ type: "tool_use", tool: "exec_command" }))).toBe("tool:exec_command");
    expect(traceEventFilterKey(ev({ type: "tool_result", tool: "exec_command" }))).toBe("tool:exec_command");
    expect(traceEventFilterKey(ev({ type: "error" }))).toBe("error");
    expect(traceEventFilterKey(ev({ type: "tool_use" }))).toBe("tool_use");
  });
});

describe("shortenTracePath", () => {
  it("keeps short paths and abbreviates deep ones", () => {
    expect(shortenTracePath("a/b")).toBe("a/b");
    expect(shortenTracePath("/w/x/y/z.ts")).toBe(".../y/z.ts");
  });
});

describe("decodeToolResultOutput — conservative one-level unwrap (MUL-5122)", () => {
  it("unwraps a double-encoded JSON string to real newlines and quotes", () => {
    // Storage held a JSON-encoded string: outer quotes, escaped \n and \".
    const stored = '"line1\\nline2 \\"q\\""';
    expect(decodeToolResultOutput(stored)).toEqual({
      text: 'line1\nline2 "q"',
      json: false,
    });
  });

  it("pretty-prints a JSON object string", () => {
    const result = decodeToolResultOutput('{"status":"ok","n":2}');
    expect(result.json).toBe(true);
    expect(result.text).toBe('{\n  "status": "ok",\n  "n": 2\n}');
    expect(result.text.split("\n").length).toBeGreaterThan(1);
  });

  it("pretty-prints a JSON array string", () => {
    const result = decodeToolResultOutput("[1,2,3]");
    expect(result.json).toBe(true);
    expect(result.text).toBe("[\n  1,\n  2,\n  3\n]");
  });

  it("leaves plain multi-line logs untouched (real newline, not JSON)", () => {
    expect(decodeToolResultOutput("PASS\nok")).toEqual({ text: "PASS\nok", json: false });
  });

  it("leaves a real backslash path verbatim — never a global \\n-replace", () => {
    // Real backslashes; the string does not start with " { or [, so it is not parsed.
    expect(decodeToolResultOutput("C:\\Users\\x")).toEqual({ text: "C:\\Users\\x", json: false });
  });

  it("returns malformed JSON-looking text unchanged", () => {
    expect(decodeToolResultOutput("{oops")).toEqual({ text: "{oops", json: false });
  });

  it("returns an empty string unchanged", () => {
    expect(decodeToolResultOutput("")).toEqual({ text: "", json: false });
  });
});
