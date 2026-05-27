import { describe, expect, it } from "vitest";
import type { Agent, MemberWithUser } from "@multica/core/types";
import {
  extractMentionedAgentIds,
  getReplyTargetAgents,
  memberMentionMarkdown,
} from "./reply-targets";

const makeAgent = (id: string, name: string): Agent =>
  ({
    id,
    name,
  }) as Agent;

const makeMember = (name: string, userId: string): MemberWithUser =>
  ({
    name,
    user_id: userId,
  }) as MemberWithUser;

describe("ReplyInput target resolution", () => {
  it("uses the replied-to agent when the draft has no agent tags", () => {
    const fallback = makeAgent("agent-fallback", "Fallback Agent");
    const tagged = makeAgent("agent-tagged", "Tagged Agent");

    expect(
      getReplyTargetAgents({
        markdown: "plain reply",
        replyTargetAgentId: fallback.id,
        agents: [fallback, tagged],
      }).map((agent) => agent.id),
    ).toEqual([fallback.id]);
  });

  it("does not show a target when there is no replied-to agent or agent tag", () => {
    const agent = makeAgent("agent-1", "Agent One");

    expect(
      getReplyTargetAgents({
        markdown: "plain issue comment",
        agents: [agent],
      }),
    ).toEqual([]);
  });

  it("switches to explicitly tagged agents", () => {
    const fallback = makeAgent("agent-fallback", "Fallback Agent");
    const tagged = makeAgent("agent-tagged", "Tagged Agent");

    expect(
      getReplyTargetAgents({
        markdown: "ping [@Tagged Agent](mention://agent/agent-tagged)",
        replyTargetAgentId: fallback.id,
        agents: [fallback, tagged],
      }).map((agent) => agent.id),
    ).toEqual([tagged.id]);
  });

  it("deduplicates agent tags in draft order", () => {
    expect(
      extractMentionedAgentIds(
        "[@A](mention://agent/a) [@A](mention://agent/a) [@B](mention://agent/b)",
      ),
    ).toEqual(["a", "b"]);
  });

  it("escapes owner names when inserting a member mention", () => {
    expect(memberMentionMarkdown(makeMember("Sara [CTO]", "user-1"))).toBe(
      "[@Sara \\[CTO\\]](mention://member/user-1)",
    );
  });
});
