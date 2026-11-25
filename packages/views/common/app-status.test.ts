// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { Agent, AgentApp } from "@multica/core/types";
import { agentAppsForViewer } from "./app-status";

function agent(id: string, patch: Partial<Agent> = {}): Agent {
  return {
    id,
    owner_id: "someone-else",
    permission_mode: "private",
    invocation_targets: [],
    archived_at: null,
    ...patch,
  } as Agent;
}

const app = (agent_id: string, account_scope: AgentApp["account_scope"]): AgentApp => ({
  agent_id, toolkit_slug: "gmail", account_scope, level: "write",
});

const viewer = { userId: "me", role: "member" as const };
const ids = (apps: AgentApp[]) => apps.map((a) => `${a.agent_id}:${a.account_scope}`);

describe("agentAppsForViewer", () => {
  it("counts only agents the viewer can run against the viewer's personal accounts", () => {
    const agents = [
      agent("mine", { owner_id: "me" }),
      agent("shared", { permission_mode: "public_to", invocation_targets: [{ target_type: "workspace", target_id: "ws-1" }] }),
      agent("granted", { permission_mode: "public_to", invocation_targets: [{ target_type: "member", target_id: "me" }] }),
      agent("others-private"),
      agent("others-restricted", { permission_mode: "public_to", invocation_targets: [{ target_type: "member", target_id: "u2" }] }),
    ];
    const apps = agents.map((a) => app(a.id, "personal"));
    expect(ids(agentAppsForViewer(apps, agents, viewer))).toEqual(["mine:personal", "shared:personal", "granted:personal"]);
  });

  it("keeps every live agent on a team account, whoever owns it", () => {
    const agents = [agent("others-private")];
    expect(ids(agentAppsForViewer([app("others-private", "team")], agents, viewer))).toEqual(["others-private:team"]);
  });

  it("leaves out archived agents and agents the list does not have", () => {
    const agents = [agent("archived", { owner_id: "me", archived_at: "2026-11-01T00:00:00Z" })];
    const apps = [app("archived", "personal"), app("archived", "team"), app("gone", "team")];
    expect(agentAppsForViewer(apps, agents, viewer)).toEqual([]);
  });
});
