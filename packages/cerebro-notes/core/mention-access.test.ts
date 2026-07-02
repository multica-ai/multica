import { describe, it, expect } from "vitest";
import { extractMemberMentions } from "./comments";

// FIR-2595 Stage 2 — the composer relies on this to know who was tagged before
// asking the server whether they can open the note.
describe("extractMemberMentions", () => {
  const a = "11111111-1111-4111-8111-111111111111";
  const b = "22222222-2222-4222-8222-222222222222";

  it("pulls member UUIDs out of mention markdown", () => {
    const body = `hey [@Ann](mention://member/${a}) and [@Bo](mention://member/${b})`;
    expect(extractMemberMentions(body).sort()).toEqual([a, b].sort());
  });

  it("de-duplicates a member mentioned twice", () => {
    const body = `[@Ann](mention://member/${a}) ... [@Ann again](mention://member/${a})`;
    expect(extractMemberMentions(body)).toEqual([a]);
  });

  it("ignores non-member mentions (agents, issues) and plain text", () => {
    const body = `[@Bot](mention://agent/${a}) see [MUL-1](mention://issue/${b}) no tags here`;
    expect(extractMemberMentions(body)).toEqual([]);
  });

  it("returns empty for an empty body", () => {
    expect(extractMemberMentions("")).toEqual([]);
  });
});
