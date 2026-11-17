import { afterEach, describe, expect, it } from "vitest";
import { Editor } from "@tiptap/core";
import type { Node } from "@tiptap/pm/model";
import type { Agent, AgentSkillSummary, AgentTask, MemberWithUser, Squad, TimelineEntry } from "@multica/core/types";
import { createEditorExtensions } from "../../editor/extensions";
import { resolveIssueSlashSkills, type IssueSlashSkillContext } from "./use-quick-action-menu";

const editors: Editor[] = [];

afterEach(() => {
  editors.splice(0).forEach((editor) => editor.destroy());
});

function skill(id: string, overrides: Partial<AgentSkillSummary> = {}): AgentSkillSummary {
  return { id, name: id, description: `${id} description`, ...overrides };
}

function agent(
  id: string,
  skills: AgentSkillSummary[],
  overrides: Partial<Agent> = {},
): Agent {
  return {
    id,
    skills,
    runtime_id: "runtime-1",
    archived_at: null,
    owner_id: null,
    permission_mode: "public_to",
    invocation_targets: [{ target_type: "workspace", target_id: null }],
    ...overrides,
  } as Agent;
}

function squad(id: string, leaderId: string, archivedAt: string | null = null): Squad {
  return { id, leader_id: leaderId, archived_at: archivedAt } as Squad;
}

function context(overrides: Partial<IssueSlashSkillContext> = {}): IssueSlashSkillContext {
  return {
    agents: [],
    squads: [],
    members: [{ user_id: "user-1", role: "member" } as MemberWithUser],
    userId: "user-1",
    ...overrides,
  };
}

function makeEditor(): Editor {
  const editor = new Editor({ extensions: createEditorExtensions({}) });
  editors.push(editor);
  return editor;
}

interface TestMentionContent {
  type: "mention";
  attrs: { type: string; id: string; label: string; mentionSuggestionChar: string };
}

function mention(type: string, id: string): TestMentionContent {
  return {
    type: "mention",
    attrs: { type, id, label: id, mentionSuggestionChar: "@" },
  };
}

function docWithMentions(...mentions: TestMentionContent[]): Node {
  const editor = makeEditor();
  if (mentions.length) editor.commands.insertContent(mentions);
  return editor.state.doc;
}

function timelineComment(
  id: string,
  overrides: Partial<TimelineEntry> = {},
): TimelineEntry {
  return {
    type: "comment",
    id,
    actor_type: "member",
    actor_id: "user-2",
    content: "",
    parent_id: null,
    created_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("resolveIssueSlashSkills", () => {
  it.each([undefined, ""])("excludes an agent with runtime_id %s without falling back", (runtimeId) => {
    const unavailable = agent("unavailable", [skill("unavailable-skill")], { runtime_id: runtimeId });
    const assignee = agent("assignee", [skill("assignee-skill")]);
    const data = context({
      issue: { assignee_type: "agent", assignee_id: assignee.id },
      agents: [unavailable, assignee],
    });

    expect(resolveIssueSlashSkills(docWithMentions(mention("agent", unavailable.id)), data)).toEqual([]);
    expect(resolveIssueSlashSkills(docWithMentions(), {
      ...data,
      issue: { assignee_type: "agent", assignee_id: unavailable.id },
    })).toEqual([]);
  });

  it("reads a newly inserted explicit agent synchronously and locks out the assignee", () => {
    const editor = makeEditor();
    const target = agent("explicit", [skill("explicit-skill")]);
    const assignee = agent("assignee", [skill("assignee-skill")]);
    const data = context({
      issue: { assignee_type: "agent", assignee_id: assignee.id },
      agents: [target, assignee],
    });

    expect(resolveIssueSlashSkills(editor.state.doc, data).map((item) => item.id))
      .toEqual(["assignee-skill"]);

    editor.commands.insertContent([
      mention("agent", target.id),
      { type: "text", text: " " },
    ]);

    expect(resolveIssueSlashSkills(editor.state.doc, data).map((item) => item.id))
      .toEqual(["explicit-skill"]);
  });

  it("maps an explicit squad to its leader ahead of the Issue assignee", () => {
    const leader = agent("leader", [skill("leader-skill")]);
    const assignee = agent("assignee", [skill("assignee-skill")]);
    const doc = docWithMentions(mention("squad", "squad-1"));

    expect(resolveIssueSlashSkills(doc, context({
      issue: { assignee_type: "agent", assignee_id: assignee.id },
      agents: [leader, assignee],
      squads: [squad("squad-1", leader.id)],
    })).map((item) => item.id)).toEqual(["leader-skill"]);
  });

  it("unions multiple targets in document order and deduplicates agents and skills", () => {
    const first = agent("first", [skill("one"), skill("two")]);
    const second = agent("second", [skill("two"), skill("three")]);
    const leader = agent("leader", [skill("four"), skill("one")]);
    const doc = docWithMentions(
      mention("agent", first.id),
      mention("agent", second.id),
      mention("squad", "squad-1"),
      mention("agent", first.id),
    );

    expect(resolveIssueSlashSkills(doc, context({
      agents: [first, second, leader],
      squads: [squad("squad-1", leader.id)],
    })).map((item) => item.id)).toEqual(["one", "two", "three", "four"]);
  });

  it("filters archived and unauthorized targets and disabled skills without falling back", () => {
    const archived = agent("archived", [skill("archived-skill")], { archived_at: "2026-01-01" });
    const privateAgent = agent("private", [skill("private-skill")], {
      owner_id: "other-user",
      permission_mode: "private",
      invocation_targets: [],
    });
    const allowed = agent("allowed", [
      skill("enabled"),
      skill("disabled", { enabled: false }),
    ]);
    const archivedSquadLeader = agent("squad-leader", [skill("archived-squad-skill")]);
    const assignee = agent("assignee", [skill("assignee-skill")]);
    const doc = docWithMentions(
      mention("agent", archived.id),
      mention("agent", privateAgent.id),
      mention("squad", "archived-squad"),
      mention("agent", allowed.id),
    );

    expect(resolveIssueSlashSkills(doc, context({
      issue: { assignee_type: "agent", assignee_id: assignee.id },
      agents: [archived, privateAgent, allowed, archivedSquadLeader, assignee],
      squads: [squad("archived-squad", archivedSquadLeader.id, "2026-01-01")],
    })).map((item) => item.id)).toEqual(["enabled"]);
  });

  it.each([
    { assigneeType: "agent" as const, squadId: undefined },
    { assigneeType: "squad" as const, squadId: "squad-1" },
  ])("falls back to an Issue $assigneeType assignee", ({ assigneeType, squadId }) => {
    const assignee = agent("assignee", [skill("assignee-skill")]);
    const assigneeId = assigneeType === "squad" ? squadId! : assignee.id;

    expect(resolveIssueSlashSkills(docWithMentions(), context({
      issue: { assignee_type: assigneeType, assignee_id: assigneeId },
      agents: [assignee],
      squads: squadId ? [squad(squadId, assignee.id)] : [],
    })).map((item) => item.id)).toEqual(["assignee-skill"]);
  });

  it.each([null, "member" as const])(
    "returns no skills for an Issue assigned to %s without explicit targets",
    (assigneeType) => {
      const available = agent("available", [skill("unrelated-skill")]);
      expect(resolveIssueSlashSkills(docWithMentions(), context({
        issue: { assignee_type: assigneeType, assignee_id: assigneeType ? "user-2" : null },
        agents: [available],
      }))).toEqual([]);
    },
  );

  it.each(["member", "all"])("suppresses the Issue assignee fallback for @%s", (type) => {
    const assignee = agent("assignee", [skill("assignee-skill")]);
    const doc = docWithMentions(mention(type, type === "all" ? "all" : "user-2"));

    expect(resolveIssueSlashSkills(doc, context({
      issue: { assignee_type: "agent", assignee_id: assignee.id },
      agents: [assignee],
    }))).toEqual([]);
  });

  it("keeps an explicit agent target when member and all mentions are present", () => {
    const explicit = agent("explicit", [skill("explicit-skill")]);
    const assignee = agent("assignee", [skill("assignee-skill")]);
    const doc = docWithMentions(
      mention("member", "user-2"),
      mention("all", "all"),
      mention("agent", explicit.id),
    );

    expect(resolveIssueSlashSkills(doc, context({
      issue: { assignee_type: "agent", assignee_id: assignee.id },
      agents: [explicit, assignee],
    })).map((item) => item.id)).toEqual(["explicit-skill"]);
  });

  it("does not let issue or project mentions suppress the assignee fallback", () => {
    const assignee = agent("assignee", [skill("assignee-skill")]);
    const doc = docWithMentions(
      mention("issue", "issue-2"),
      mention("project", "project-2"),
    );

    expect(resolveIssueSlashSkills(doc, context({
      issue: { assignee_type: "agent", assignee_id: assignee.id },
      agents: [assignee],
    })).map((item) => item.id)).toEqual(["assignee-skill"]);
  });

  it("returns no skills for an archived Issue squad assignee", () => {
    const leader = agent("leader", [skill("leader-skill")]);

    expect(resolveIssueSlashSkills(docWithMentions(), context({
      issue: { assignee_type: "squad", assignee_id: "squad-1" },
      agents: [leader],
      squads: [squad("squad-1", leader.id, "2026-01-01")],
    }))).toEqual([]);
  });

  it("routes a reply to its agent parent author instead of the Issue assignee", () => {
    const parentAuthor = agent("parent-author", [skill("parent-skill")]);
    const assignee = agent("assignee", [skill("assignee-skill")]);
    const parent = timelineComment("parent", {
      actor_type: "agent",
      actor_id: parentAuthor.id,
      parent_id: "root",
    });

    expect(resolveIssueSlashSkills(docWithMentions(), context({
      issue: { assignee_type: "agent", assignee_id: assignee.id },
      agents: [parentAuthor, assignee],
      parentId: parent.id,
      timeline: [parent],
    })).map((item) => item.id)).toEqual(["parent-skill"]);
  });

  it("uses the first explicit agent in a member root comment", () => {
    const first = agent("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", [skill("first-skill")]);
    const second = agent("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", [skill("second-skill")]);
    const root = timelineComment("root", {
      content: `[@First](mention://agent/${first.id}) [@Second](mention://agent/${second.id})`,
    });
    const reply = timelineComment("reply", { parent_id: root.id });

    expect(resolveIssueSlashSkills(docWithMentions(), context({
      agents: [first, second],
      parentId: reply.id,
      timeline: [root, reply],
    })).map((item) => item.id)).toEqual(["first-skill"]);
  });

  it("maps an explicit squad in a member root comment to its leader", () => {
    const leader = agent("leader", [skill("leader-skill")]);
    const squadId = "cccccccc-cccc-cccc-cccc-cccccccccccc";
    const root = timelineComment("root", {
      content: `[@Team](mention://squad/${squadId})`,
    });
    const reply = timelineComment("reply", { parent_id: root.id });

    expect(resolveIssueSlashSkills(docWithMentions(), context({
      agents: [leader],
      squads: [squad(squadId, leader.id)],
      parentId: reply.id,
      timeline: [root, reply],
    })).map((item) => item.id)).toEqual(["leader-skill"]);
  });

  it("does not skip an unavailable first root mention for a later target or assignee", () => {
    const archived = agent("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", [skill("archived-skill")], { archived_at: "2026-01-01" });
    const second = agent("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", [skill("second-skill")]);
    const assignee = agent("assignee", [skill("assignee-skill")]);
    const root = timelineComment("root", {
      content: `[@Archived](mention://agent/${archived.id}) [@Second](mention://agent/${second.id})`,
    });
    const reply = timelineComment("reply", { parent_id: root.id });

    expect(resolveIssueSlashSkills(docWithMentions(), context({
      issue: { assignee_type: "agent", assignee_id: assignee.id },
      agents: [archived, second, assignee],
      parentId: reply.id,
      timeline: [root, reply],
    }))).toEqual([]);
  });

  it("uses the conversation owner when replying directly to a member root", () => {
    const owner = agent("owner", [skill("owner-skill")]);
    const root = timelineComment("root");
    const tasks = [{ agent_id: owner.id, trigger_comment_id: root.id } as AgentTask];

    expect(resolveIssueSlashSkills(docWithMentions(), context({
      agents: [owner],
      parentId: root.id,
      timeline: [root],
      tasks,
    })).map((item) => item.id)).toEqual(["owner-skill"]);
  });

  it("uses the conversation owner from a coalesced root comment", () => {
    const owner = agent("owner", [skill("owner-skill")]);
    const root = timelineComment("root");
    const tasks = [{
      agent_id: owner.id,
      trigger_comment_id: "latest-comment",
      coalesced_comment_ids: [root.id],
    } as AgentTask];

    expect(resolveIssueSlashSkills(docWithMentions(), context({
      agents: [owner],
      parentId: root.id,
      timeline: [root],
      tasks,
    })).map((item) => item.id)).toEqual(["owner-skill"]);
  });

  it("uses task and same-thread agent owners for a nested member reply", () => {
    const taskOwner = agent("task-owner", [skill("task-skill")]);
    const replyOwner = agent("reply-owner", [skill("reply-skill")]);
    const root = timelineComment("root");
    const agentReply = timelineComment("agent-reply", {
      actor_type: "agent",
      actor_id: replyOwner.id,
      parent_id: root.id,
    });
    const memberReply = timelineComment("member-reply", { parent_id: agentReply.id });
    const tasks = [{ agent_id: taskOwner.id, trigger_comment_id: root.id } as AgentTask];

    expect(resolveIssueSlashSkills(docWithMentions(), context({
      agents: [taskOwner, replyOwner],
      parentId: memberReply.id,
      timeline: [root, agentReply, memberReply],
      tasks,
    })).map((item) => item.id)).toEqual(["task-skill", "reply-skill"]);
  });

  it("keeps plain member-to-member replies silent", () => {
    const assignee = agent("assignee", [skill("assignee-skill")]);
    const root = timelineComment("root");
    const reply = timelineComment("reply", { parent_id: root.id });

    expect(resolveIssueSlashSkills(docWithMentions(), context({
      issue: { assignee_type: "agent", assignee_id: assignee.id },
      agents: [assignee],
      parentId: reply.id,
      timeline: [root, reply],
    }))).toEqual([]);
  });

  it("returns no skills when the reply target is missing", () => {
    const explicit = agent("explicit", [skill("explicit-skill")]);

    expect(resolveIssueSlashSkills(docWithMentions(mention("agent", explicit.id)), context({
      agents: [explicit],
      targetMissing: true,
    }))).toEqual([]);
  });

  it("returns no skills when a loaded timeline lacks the parent or contains only its tombstone", () => {
    const assignee = agent("assignee", [skill("assignee-skill")]);
    const base = context({
      issue: { assignee_type: "agent", assignee_id: assignee.id },
      agents: [assignee],
      parentId: "parent",
    });
    const deleted = timelineComment("parent", {
      actor_type: "agent",
      actor_id: assignee.id,
      deleted_at: "2026-01-01",
    });

    expect(resolveIssueSlashSkills(docWithMentions(), { ...base, timeline: [] })).toEqual([]);
    expect(resolveIssueSlashSkills(docWithMentions(), { ...base, timeline: [deleted] })).toEqual([]);
  });

  it("retains the assignee fallback while the timeline is not cached", () => {
    const assignee = agent("assignee", [skill("assignee-skill")]);

    expect(resolveIssueSlashSkills(docWithMentions(), context({
      issue: { assignee_type: "agent", assignee_id: assignee.id },
      agents: [assignee],
      parentId: "parent",
    })).map((item) => item.id)).toEqual(["assignee-skill"]);
  });

  it("retains the assignee fallback for a non-member parent without thread owners", () => {
    const assignee = agent("assignee", [skill("assignee-skill")]);
    const parent = timelineComment("parent", { actor_type: "system", actor_id: "system" });

    expect(resolveIssueSlashSkills(docWithMentions(), context({
      issue: { assignee_type: "agent", assignee_id: assignee.id },
      agents: [assignee],
      parentId: parent.id,
      timeline: [parent],
    })).map((item) => item.id)).toEqual(["assignee-skill"]);
  });
});
