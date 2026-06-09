import { describe, it, expect } from "vitest";
import { parseLogLine } from "./parse-daemon-log";

// All sample lines below are taken verbatim from real daemon output (Go
// `slog` + `lmittmann/tint` v1.1.3 with NoColor=true). The parser must
// stay aligned with what tint actually writes — not what we assume.

describe("parseLogLine", () => {
  it("parses tint's 3-letter INF level", () => {
    const line =
      "17:52:35.587 INF task completed component=daemon task=c45266e5 status=completed";
    const r = parseLogLine(line, 1);
    expect(r.timestamp).toBe("17:52:35.587");
    expect(r.level).toBe("INFO");
    expect(r.message).toBe("task completed");
    expect(r.fields).toEqual({
      component: "daemon",
      task: "c45266e5",
      status: "completed",
    });
  });

  it("parses 3-letter DBG / WRN / ERR levels", () => {
    expect(parseLogLine("17:53:06.644 DBG agent component=daemon", 1).level).toBe("DEBUG");
    expect(parseLogLine("07:48:09.391 WRN claim task failed component=daemon", 1).level).toBe("WARN");
    expect(parseLogLine("12:00:00.000 ERR something bad component=daemon", 1).level).toBe("ERROR");
  });

  it("still accepts 4-letter level names (defensive against config changes)", () => {
    const r = parseLogLine("12:00:00.000 INFO regular component=daemon", 1);
    expect(r.level).toBe("INFO");
    expect(r.message).toBe("regular");
  });

  it("tolerates the +N / -N delta tint appends for non-standard slog levels", () => {
    // tint emits e.g. "INF+1" when slog.Log is called with LevelInfo+1.
    // We treat the base level as canonical and drop the delta from the UI.
    const r = parseLogLine("12:00:00.000 INF+1 unusual delta component=daemon", 1);
    expect(r.level).toBe("INFO");
    expect(r.message).toBe("unusual delta");
  });

  it("preserves message text containing colons and special chars", () => {
    // Real sample: "tool #1: Skill component=daemon task=..."
    const r = parseLogLine(
      "17:52:54.578 INF tool #1: Skill component=daemon task=8791b717",
      1,
    );
    expect(r.message).toBe("tool #1: Skill");
    expect(r.fields).toEqual({ component: "daemon", task: "8791b717" });
  });

  it("unquotes a double-quoted value containing escaped quotes", () => {
    // Real sample with escaped quotes inside the agent's emitted text.
    const line =
      '17:53:06.644 DBG agent component=daemon task=8791b717 text="The issue is just \\"ping\\" with no description."';
    const r = parseLogLine(line, 1);
    expect(r.message).toBe("agent");
    expect(r.fields.text).toBe('The issue is just "ping" with no description.');
    expect(r.fields.task).toBe("8791b717");
  });

  it("handles a quoted value containing a URL with embedded escaped quotes and a colon", () => {
    // Real sample: error="Post \"http://...\": dial tcp ..."
    const line =
      '07:48:09.391 WRN claim task failed component=daemon runtime_id=03f8ff17-276d error="Post \\"http://localhost:8080/api/daemon/runtimes/abc/tasks/claim\\": dial tcp [::1]:8080: connect: connection refused"';
    const r = parseLogLine(line, 1);
    expect(r.level).toBe("WARN");
    expect(r.message).toBe("claim task failed");
    expect(r.fields.runtime_id).toBe("03f8ff17-276d");
    expect(r.fields.error).toBe(
      'Post "http://localhost:8080/api/daemon/runtimes/abc/tasks/claim": dial tcp [::1]:8080: connect: connection refused',
    );
  });

  it("handles a quoted value with internal whitespace (e.g. args array)", () => {
    const line =
      '17:52:48.757 INF agent command component=daemon exec=claude args="[-p --output-format stream-json --verbose]"';
    const r = parseLogLine(line, 1);
    expect(r.message).toBe("agent command");
    expect(r.fields.exec).toBe("claude");
    expect(r.fields.args).toBe("[-p --output-format stream-json --verbose]");
  });

  it("handles message words ending with characters before the field block", () => {
    // 'execenv:' is part of the message — the colon shouldn't confuse parsing.
    const r = parseLogLine(
      "17:52:48.757 INF execenv: prepared env component=daemon repos_available=0",
      1,
    );
    expect(r.message).toBe("execenv: prepared env");
    expect(r.fields).toEqual({ component: "daemon", repos_available: "0" });
  });

  it("falls back to raw rendering for non-matching lines (panic stack frame)", () => {
    const r = parseLogLine("\tat github.com/multica/foo (line 42)", 1);
    expect(r.timestamp).toBeNull();
    expect(r.level).toBeNull();
    expect(r.message).toBe("\tat github.com/multica/foo (line 42)");
    expect(r.fields).toEqual({});
    expect(r.raw).toBe("\tat github.com/multica/foo (line 42)");
  });

  it("falls back to raw rendering for unrecognised level tokens", () => {
    // If tint ever emits something we don't know, never crash; show raw.
    const r = parseLogLine("12:00:00.000 TRACE something exotic", 1);
    expect(r.timestamp).toBeNull();
    expect(r.level).toBeNull();
    expect(r.raw).toBe("12:00:00.000 TRACE something exotic");
  });

  it("attaches an id to every parsed line for stable React keys", () => {
    const a = parseLogLine("17:52:35.587 INF first component=daemon", 7);
    const b = parseLogLine("17:52:35.588 INF second component=daemon", 8);
    expect(a.id).toBe(7);
    expect(b.id).toBe(8);
  });

  it("returns empty fields object when there are no key=value pairs", () => {
    const r = parseLogLine("17:52:35.587 INF a bare message with no fields", 1);
    expect(r.message).toBe("a bare message with no fields");
    expect(r.fields).toEqual({});
  });
});
