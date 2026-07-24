import { describe, expect, it } from "vitest";
import {
  stripShellWrapper,
  traceEventDefaultExpanded,
  traceEventHasDetail,
  traceEventKind,
  traceEventLabel,
  traceEventSummary,
  traceToolArgSummary,
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
  it("takes the first non-empty line for agent text and tool output", () => {
    expect(traceEventSummary({ type: "text", content: "\n\nFirst line\nrest" })).toBe(
      "First line",
    );
    expect(traceEventSummary({ type: "tool_result", output: "line one\nline two" })).toBe(
      "line one",
    );
  });

  it("retains unknown events instead of dropping them", () => {
    expect(traceEventSummary({ type: "custom", content: "payload" })).toBe("payload");
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
