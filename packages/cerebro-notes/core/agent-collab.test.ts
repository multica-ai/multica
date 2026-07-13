import { describe, expect, it } from "vitest";
import {
  isUnsentToAgent,
  hasAgentMention,
  parseIssueMentions,
  safeParseSendResult,
  safeParseNoteComments,
  type NoteComment,
} from "./comments";

const AGENT_TAG =
  "please look [@Bot](mention://agent/00000000-0000-0000-0000-0000000000aa)";

// FIR-1621 — agent-collaboration: unsent-state detection + defensive parsing of
// the send-comments response. The API Response Compatibility rule requires
// malformed responses to fail soft, never throw.

function comment(over: Partial<NoteComment>): NoteComment {
  return {
    id: "c1",
    note_id: "n1",
    thread_root_id: null,
    kind: "comment",
    body: AGENT_TAG,
    anchor_quote: null,
    anchor_prefix: null,
    anchor_suffix: null,
    anchor_start: null,
    anchor_end: null,
    suggestion_text: null,
    suggestion_state: "pending",
    resolved: false,
    resolved_by: null,
    resolved_at: "",
    author_type: "member",
    author_id: "u1",
    created_at: "",
    updated_at: "",
    sent_to_agent_at: null,
    issue_id: null,
    ...over,
  };
}

describe("hasAgentMention", () => {
  it("detects an agent mention link", () => {
    expect(hasAgentMention(AGENT_TAG)).toBe(true);
  });
  it("ignores plain text and member mentions", () => {
    expect(hasAgentMention("just a note")).toBe(false);
    expect(
      hasAgentMention("[@Jo](mention://member/00000000-0000-0000-0000-0000000000aa)"),
    ).toBe(false);
  });
});

describe("parseIssueMentions", () => {
  const A = "00000000-0000-0000-0000-0000000000a1";
  const B = "00000000-0000-0000-0000-0000000000b2";
  it("extracts distinct issue mentions with their label, first-seen order", () => {
    const body =
      `see [FIR-1](mention://issue/${A}) and [FIR-2](mention://issue/${B}) ` +
      `and again [FIR-1](mention://issue/${A})`;
    const got = parseIssueMentions(body);
    expect(got).toEqual([
      { id: A, label: "FIR-1" },
      { id: B, label: "FIR-2" },
    ]);
  });
  it("ignores agent and member mentions, returns [] when none", () => {
    expect(parseIssueMentions(AGENT_TAG)).toEqual([]);
    expect(parseIssueMentions("just a plain note")).toEqual([]);
  });
});

describe("isUnsentToAgent", () => {
  it("a member comment that tags an agent and is not sent is unsent", () => {
    expect(isUnsentToAgent(comment({}))).toBe(true);
  });
  it("a member comment with NO agent tag is never unsent (local discussion)", () => {
    expect(isUnsentToAgent(comment({ body: "just a thought" }))).toBe(false);
  });
  it("a sent member comment is not unsent", () => {
    expect(
      isUnsentToAgent(comment({ sent_to_agent_at: "2026-06-19T10:00:00Z" })),
    ).toBe(false);
  });
  it("an agent-authored comment is never an unsent draft", () => {
    expect(isUnsentToAgent(comment({ author_type: "agent" }))).toBe(false);
  });
  it("a suggestion is never an unsent draft", () => {
    expect(isUnsentToAgent(comment({ kind: "suggestion" }))).toBe(false);
  });
});

describe("sent_to_agent_at parsing", () => {
  it("defaults a missing sent_to_agent_at to null", () => {
    const out = safeParseNoteComments([{ id: "x", body: "b" }]);
    expect(out[0]!.sent_to_agent_at).toBeNull();
  });
});

describe("safeParseSendResult", () => {
  it("parses a full response", () => {
    const out = safeParseSendResult({
      sent: [{ id: "c1", body: "hi", sent_to_agent_at: "2026-06-19T10:00:00Z" }],
      unsent_remaining: 2,
      destination_kind: "issue",
      destination_ref_id: "MUL-1",
      agents_triggered: 1,
    });
    expect(out.sent).toHaveLength(1);
    expect(out.unsent_remaining).toBe(2);
    expect(out.destination_kind).toBe("issue");
    expect(out.agents_triggered).toBe(1);
  });
  it("fails soft on a malformed response", () => {
    const out = safeParseSendResult("garbage");
    expect(out.sent).toEqual([]);
    expect(out.unsent_remaining).toBe(0);
    expect(out.destination_kind).toBe("");
  });
  it("tolerates missing fields", () => {
    const out = safeParseSendResult({ destination_kind: "chat_session" });
    expect(out.sent).toEqual([]);
    expect(out.destination_kind).toBe("chat_session");
    expect(out.agents_triggered).toBe(0);
  });
});
