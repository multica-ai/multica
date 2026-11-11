// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { Agent } from "../types";
import { resolveChatAgent, workspaceDefaultChatAgentId } from "./resolve-agent";

const first = { id: "first", runtime_id: "runtime" } as Agent;
const personal = { id: "personal", runtime_id: "runtime" } as Agent;
const workspace = { id: "workspace", runtime_id: "runtime" } as Agent;
const archived = { id: "archived", archived_at: "now" } as Agent;
const input = { agents: [first, personal, workspace, archived], availableAgents: [first, personal, workspace], agentsSettled: true, sessionId: null, sessionAgentId: null, selectedAgentId: null, defaultAgentId: workspace.id };

describe("chat agent resolution", () => {
  it("keeps the real session agent, including archived or no longer callable agents", () => {
    expect(resolveChatAgent({ ...input, sessionId: "session", sessionAgentId: archived.id, selectedAgentId: personal.id, explicitAgentId: workspace.id })).toBe(archived);
  });
  it("does not resolve a fallback while a session or its agent is pending or missing", () => {
    expect(resolveChatAgent({ ...input, sessionId: "pending" })).toBeNull();
    expect(resolveChatAgent({ ...input, sessionId: "session", sessionAgentId: "missing" })).toBeNull();
  });
  it("respects URL then personal then workspace then first/null", () => {
    expect(resolveChatAgent({ ...input, explicitAgentId: first.id, selectedAgentId: personal.id })).toBe(first);
    expect(resolveChatAgent({ ...input, selectedAgentId: personal.id })).toBe(personal);
    expect(resolveChatAgent({ ...input, selectedAgentId: "revoked" })).toBe(workspace);
    expect(resolveChatAgent({ ...input, defaultAgentId: "deleted" })).toBe(first);
    expect(resolveChatAgent({ ...input, availableAgents: [] })).toBeNull();
  });
  it("holds pending explicit URL intent until it is resolved or consumed", () => {
    expect(resolveChatAgent({ ...input, explicitAgentId: "pending" })).toBeNull();
    expect(resolveChatAgent({ ...input, explicitAgentId: personal.id, agentsSettled: false })).toBeNull();
  });
  it("uses the Ask preference then workspace default then first, ignoring the last chat selection", () => {
    const ask = { ...input, isAskChat: true, selectedAgentId: personal.id };
    expect(resolveChatAgent({ ...ask, quickChatAgentId: first.id })).toBe(first);
    expect(resolveChatAgent(ask)).toBe(workspace);
    expect(resolveChatAgent({ ...ask, quickChatAgentId: "revoked" })).toBe(workspace);
    expect(resolveChatAgent({ ...ask, defaultAgentId: "revoked" })).toBe(first);
    expect(resolveChatAgent({ ...ask, availableAgents: [] })).toBeNull();
  });
  it("waits for workspace fallback while retaining a valid personal choice", () => {
    const ask = { ...input, isAskChat: true, workspaceSettled: false };
    expect(resolveChatAgent(ask)).toBeNull();
    expect(resolveChatAgent({ ...ask, quickChatAgentId: personal.id })).toBe(personal);
    expect(resolveChatAgent({ ...ask, isAskChat: false, selectedAgentId: personal.id })).toBe(personal);
  });
  it("does not infer authorization from loading or failed agents/member queries", () => {
    expect(resolveChatAgent({ ...input, selectedAgentId: personal.id, agentsSettled: false })).toBeNull();
  });
  it("ignores unbound defaults without requiring offline agents to be online", () => {
    expect(resolveChatAgent({ ...input, availableAgents: [first, { ...workspace, runtime_bound: false }] })).toBe(first);
    expect(resolveChatAgent({ ...input, availableAgents: [first, { ...workspace, runtime_availability: "offline" }] })?.id).toBe(workspace.id);
  });
  it.each([null, undefined, [], "bad", 42, { default_chat_agent_id: [] }, { default_chat_agent_id: "" }])("tolerates bad settings %j", (settings) => {
    expect(workspaceDefaultChatAgentId(settings)).toBeNull();
  });
});
