import { describe, expect, it } from "vitest";
import { collectTraceIssueIds, replaceTraceIssueIds } from "./trace-issue-labels";
import { traceEventCopyText, traceEventDetail, traceToolArgSummary } from "./trace-event-presenter";

const issue = "01a07eca-8e82-775e-be06-e4a97ccfa299";
const other = "01a07cd3-f171-706d-9e76-b3428d0de333";
const thread = "01a07cd9-610f-7b89-98e2-a194339b1372";
const labels = new Map([[issue, "DEV-17"], [other, "DEV-14"]]);

describe("trace issue labels", () => {
  it("resolves only issue targets, deduplicating commands and preserving comment IDs", () => {
    expect(collectTraceIssueIds(issue, [
      { type: "tool_use", input: { command: `multica issue comment list '${issue}' --thread ${thread}` } },
      { type: "tool_use", input: { cmd: `multica issue get ${other.toUpperCase()} --output json` } },
      { type: "tool_use", input: { issue_id: other } },
      { type: "tool_use", input: { command: `multica issue comment update ${thread}` } },
      { type: "tool_result", output: `multica issue get ${thread}` },
      { type: "text", content: `multica issue get ${thread}` },
    ])).toEqual([issue, other]);
  });

  it("replaces exact known UUIDs case-insensitively and leaves unknown IDs intact", () => {
    expect(replaceTraceIssueIds(`${issue.toUpperCase()} (${other}) --thread ${thread}`, labels))
      .toBe(`DEV-17 (DEV-14) --thread ${thread}`);
    expect(replaceTraceIssueIds(`prefix${issue} ${issue}-suffix ${issue}f`, labels))
      .toBe(`prefix${issue} ${issue}-suffix ${issue}f`);
    expect(replaceTraceIssueIds(issue, new Map())).toBe(issue);
  });

  it("formats before truncation without changing raw input, detail or copy text", () => {
    const command = `multica issue update ${issue} --description '${other} ${issue} visible-tail'`;
    const input = { command };
    const event = { type: "tool_use", tool: "exec_command", input };
    expect(traceToolArgSummary(input, { formatText: (text) => replaceTraceIssueIds(text, labels) }))
      .toBe("multica issue update DEV-17 --description 'DEV-14 DEV-17 visible-tail'");
    expect(traceToolArgSummary({ cmd: command }, { formatText: (text) => replaceTraceIssueIds(text, labels) }))
      .toBe("multica issue update DEV-17 --description 'DEV-14 DEV-17 visible-tail'");
    expect(input.command).toBe(command);
    expect(traceEventDetail(event)).toEqual({ kind: "text", text: JSON.stringify(input, null, 2) });
    expect(traceEventCopyText(event)).toContain(issue);
    expect(traceEventCopyText(event)).not.toContain("DEV-17");
  });
});
